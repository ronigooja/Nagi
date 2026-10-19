// Package rewritescript executes isolated synchronous JSON transforms. It has no
// request-rewrite domain dependencies and gives scripts no host capabilities.
package rewritescript

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const (
	MaxSourceBytes = 64 << 10
	MaxInputBytes  = 12 << 20
	MaxOutputBytes = 12 << 20
	MemoryPages    = 2048 // 128 MiB guest linear memory, not a process RSS limit.
	Timeout        = time.Second
)

var (
	ErrBusy      = i18n.Errorf("system.rewritescript.busy", "script_busy")
	ErrLimit     = i18n.Errorf("system.rewritescript.limit", "script_limit")
	ErrExecution = i18n.Errorf("system.rewritescript.execution.failed", "script_execution_failed")
	ErrOutput    = i18n.Errorf("system.rewritescript.invalid.output", "script_invalid_output")
	ErrTimeout   = i18n.Errorf("system.rewritescript.timeout", "script_timeout")
	ErrCanceled  = i18n.Errorf("system.rewritescript.canceled", "script_canceled")
	//go:embed guest/quickjs.wasm
	wasm  []byte
	slots = make(chan struct{}, 2)
	cache = wazero.NewCompilationCache()
)

// Prepare compiles the immutable guest before admitting requests. Execution's
// one-second deadline includes instantiation, copying, evaluation, and output.
func Prepare(ctx context.Context) error {
	r := wazero.NewRuntimeWithConfig(ctx, runtimeConfig())
	defer r.Close(context.Background())
	if _, err := r.CompileModule(ctx, wasm); err != nil {
		return executionError(ctx)
	}
	return nil
}

func runtimeConfig() wazero.RuntimeConfig {
	return wazero.NewRuntimeConfig().WithMemoryLimitPages(MemoryPages).
		WithCloseOnContextDone(true).WithCompilationCache(cache)
}

// Execute invokes transform(input) exactly once in a fresh guest. Admission is
// process-wide and fail-fast; there is no queue or shared JavaScript state.
// Errors are stable codes and never include script text, input, or guest stacks.
// Returned JSON has an action of unchanged, replace, or block. Callers must
// validate message fields and enforce their own domain and byte constraints.
func Execute(parent context.Context, source string, input json.RawMessage) (json.RawMessage, error) {
	if err := parent.Err(); err != nil {
		return nil, contextError(parent)
	}
	if len(source) == 0 || len(source) > MaxSourceBytes || len(input) > MaxInputBytes || !json.Valid(input) {
		return nil, ErrLimit
	}
	select {
	case slots <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	defer func() { <-slots }()
	ctx, cancel := context.WithTimeout(parent, Timeout)
	defer cancel()
	r := wazero.NewRuntimeWithConfig(ctx, runtimeConfig())
	defer r.Close(context.Background())
	compiled, err := r.CompileModule(ctx, wasm)
	if err != nil {
		return nil, executionError(ctx)
	}
	// QuickJS links libc clock/stdio hooks. Deny every imported WASI operation;
	// there is no clock, entropy, filesystem, stdout, polling, or environment host.
	if err := instantiateDeniedWASI(ctx, r, compiled); err != nil {
		return nil, executionError(ctx)
	}
	// No arguments, environment, files, sockets, clocks, or random reader are
	// configured. Only the capability-denying WASI stubs above are exported.
	m, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize"))
	if err != nil {
		return nil, executionError(ctx)
	}
	alloc := func(b []byte) (uint64, bool) {
		result, err := m.ExportedFunction("malloc").Call(ctx, uint64(len(b)+1))
		if err != nil || len(result) != 1 || result[0] == 0 {
			return 0, false
		}
		p := uint32(result[0])
		return result[0], m.Memory().Write(p, b) && m.Memory().WriteByte(p+uint32(len(b)), 0)
	}
	src, ok := alloc([]byte(source))
	if !ok {
		return nil, executionError(ctx)
	}
	in, ok := alloc(input)
	if !ok {
		return nil, executionError(ctx)
	}
	result, err := m.ExportedFunction("transform_json").Call(ctx, src, uint64(len(source)), in, uint64(len(input)))
	if err != nil || len(result) != 1 || result[0] == 0 {
		return nil, executionError(ctx)
	}
	size, err := m.ExportedFunction("result_size").Call(ctx)
	if err != nil || len(size) != 1 || size[0] == 0 || size[0] > MaxOutputBytes {
		return nil, executionError(ctx)
	}
	b, ok := m.Memory().Read(uint32(result[0]), uint32(size[0]))
	if !ok {
		return nil, ErrExecution
	}
	// Validate without converting message/body into Go object trees.
	var envelope struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(b, &envelope) != nil || (envelope.Action != "unchanged" && envelope.Action != "replace" && envelope.Action != "block") {
		return nil, ErrOutput
	}
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	return append(json.RawMessage(nil), b...), nil
}

func executionError(ctx context.Context) error {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	return ErrExecution
}
func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	return ErrCanceled
}

func instantiateDeniedWASI(ctx context.Context, r wazero.Runtime, compiled wazero.CompiledModule) error {
	builder := r.NewHostModuleBuilder("wasi_snapshot_preview1")
	for _, f := range compiled.ImportedFunctions() {
		module, name, ok := f.Import()
		if !ok || module != "wasi_snapshot_preview1" {
			return ErrExecution
		}
		fn := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) { stack[0] = 76 /* ENOTCAPABLE */ })
		builder.NewFunctionBuilder().WithGoModuleFunction(fn, f.ParamTypes(), f.ResultTypes()).Export(name)
	}
	_, err := builder.Instantiate(ctx)
	return err
}
