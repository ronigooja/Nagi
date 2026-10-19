package rewritescript

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestPinnedGuestBoundary(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256(wasm)); got != "5932a32f2d0b0dea7e02a6861852d437ba6e11addf9d6636e52122e1285bf0ba" {
		t.Fatalf("guest artifact changed: %s", got)
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, runtimeConfig())
	defer r.Close(ctx)
	compiled, err := r.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"clock_time_get": true, "fd_close": true, "fd_seek": true, "fd_write": true, "poll_oneoff": true}
	for _, f := range compiled.ImportedFunctions() {
		module, name, ok := f.Import()
		if !ok || module != "wasi_snapshot_preview1" || !allowed[name] {
			t.Fatalf("unexpected guest import %s/%s", module, name)
		}
	}
	if len(compiled.ExportedMemories()) != 1 || len(compiled.ImportedMemories()) != 0 {
		t.Fatal("unexpected memory boundary")
	}
	exports := compiled.ExportedFunctions()
	if len(exports) != 4 {
		t.Fatalf("unexpected exports: %v", exports)
	}
	for _, name := range []string{"_initialize", "malloc", "transform_json", "result_size"} {
		if exports[name] == nil {
			t.Fatalf("missing export %s", name)
		}
	}
	for _, memory := range compiled.ExportedMemories() {
		max, ok := memory.Max()
		if !ok || max != MemoryPages {
			t.Fatalf("memory maximum %d %v", max, ok)
		}
	}
}
