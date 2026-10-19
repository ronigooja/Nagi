package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type CLIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Data  any
	Code  int
	Error *CLIError
}

type Runner interface {
	Run(context.Context, []string) Result
}

// InputRunner submits command input separately from arguments and results.
type InputRunner interface {
	RunInput(context.Context, []string, io.Reader) Result
}

// ReadRunner is optional. It runs independent read-only commands and returns
// results in argument order. Run itself need not support concurrent calls.
// Session rendering, feedback, and input must remain outside the batch.
type ReadRunner interface {
	Read(context.Context, [][]string) []Result
}

// StreamRunner is optional. Streaming commands use a child process so that
// interrupting the stream does not interrupt the menu itself.
type StreamRunner interface {
	Stream(context.Context, []string, io.Writer) Result
}

// CommandFunc must support concurrent independent reads. Each invocation receives
// private stdout/stderr buffers; business operations are dispatched sequentially.
type CommandFunc func([]string, io.Writer, io.Writer) int
type InputCommandFunc func([]string, io.Reader, io.Writer, io.Writer) int

type commandRunner struct {
	run      CommandFunc
	runInput InputCommandFunc
}

func NewCommandRunner(run CommandFunc) Runner { return &commandRunner{run: run} }

func NewInputCommandRunner(run InputCommandFunc) Runner {
	return &commandRunner{runInput: run, run: func(args []string, out, errOut io.Writer) int {
		return run(args, strings.NewReader(""), out, errOut)
	}}
}

func (r *commandRunner) Run(ctx context.Context, args []string) Result {
	return r.RunInput(ctx, args, nil)
}

func (r *commandRunner) RunInput(ctx context.Context, args []string, input io.Reader) Result {
	if err := ctx.Err(); err != nil {
		return Result{Code: 1, Error: &CLIError{Code: "cancelled", Message: err.Error()}}
	}
	var stdout, stderr bytes.Buffer
	argv := append([]string{"--json"}, args...)
	var code int
	if input != nil {
		if r.runInput == nil {
			return Result{Code: 1, Error: &CLIError{Code: "input_unavailable", Message: "Command runner does not support input"}}
		}
		code = r.runInput(argv, input, &stdout, &stderr)
	} else {
		code = r.run(argv, &stdout, &stderr)
	}
	var envelope struct {
		OK    bool      `json:"ok"`
		Data  any       `json:"data"`
		Error *CLIError `json:"error"`
	}
	data := stdout.Bytes()
	if code != 0 {
		data = stderr.Bytes()
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Result{Code: maxCode(code), Error: &CLIError{Code: "output_error", Message: "Could not decode command response"}}
	}
	if !envelope.OK || code != 0 {
		if envelope.Error == nil {
			envelope.Error = &CLIError{Code: "command_error", Message: "Command failed"}
		}
		return Result{Code: maxCode(code), Error: envelope.Error}
	}
	return Result{Data: envelope.Data}
}

// Read is used only for independent page status reads. Mutations continue to
// use Run sequentially, including multi-command operations.
func (r *commandRunner) Read(ctx context.Context, commands [][]string) []Result {
	results := make([]Result, len(commands))
	var wg sync.WaitGroup
	for i, args := range commands {
		i, args := i, args
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.Run(ctx, args)
		}()
	}
	wg.Wait()
	return results
}

func maxCode(code int) int {
	if code == 0 {
		return 1
	}
	return code
}

func (r *commandRunner) Stream(ctx context.Context, args []string, out io.Writer) Result {
	exe, err := os.Executable()
	if err != nil {
		return Result{Code: 1, Error: &CLIError{Code: "output_error", Message: err.Error()}}
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}
		}
		code := 1
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = fmt.Sprintf("command exited with status %d", code)
		}
		return Result{Code: code, Error: &CLIError{Code: "stream_error", Message: message}}
	}
	return Result{}
}

// RawRunner is restricted to explicit request-rewrite document/public CA exports.
// Raw data is for the selected sensitive workflow, never ordinary presentation.
type RawRunner interface {
	RunRaw(context.Context, []string) Result
}

func (r *commandRunner) RunRaw(ctx context.Context, args []string) Result {
	if ctx.Err() != nil {
		return Result{Code: 1, Error: &CLIError{Message: "Export cancelled"}}
	}
	valid := len(args) == 3 && args[0] == "request-rewrite" && args[1] == "export"
	if len(args) == 5 && args[0] == "request-rewrite" && args[1] == "export" && args[3] == "--profile" && args[4] != "" {
		valid = true
	}
	if len(args) == 4 && args[0] == "request-rewrite" && args[1] == "ca" && args[2] == "export" {
		valid = true
	}
	if valid {
		destination := args[2]
		if args[1] == "ca" {
			destination = args[3]
		}
		if destination == "" || (strings.HasPrefix(destination, "-") && destination != "-") {
			valid = false
		}
	}
	for _, arg := range args {
		if arg == "--json" {
			valid = false
		}
	}
	if !valid {
		return Result{Code: 1, Error: &CLIError{Message: "Unsupported raw command"}}
	}
	var stdout, stderr bytes.Buffer
	code := r.run(args, &stdout, &stderr)
	if code != 0 {
		return Result{Code: code, Error: &CLIError{Message: "Export failed; check destination permissions, existence, and request-rewrite status"}}
	}
	return Result{Data: stdout.String()}
}
