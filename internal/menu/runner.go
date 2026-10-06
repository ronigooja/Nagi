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

// StreamRunner is optional. Streaming commands use a child process so that
// interrupting the stream does not interrupt the menu itself.
type StreamRunner interface {
	Stream(context.Context, []string, io.Writer) Result
}

type CommandFunc func([]string, io.Writer, io.Writer) int

type commandRunner struct{ run CommandFunc }

func NewCommandRunner(run CommandFunc) Runner { return &commandRunner{run: run} }

func (r *commandRunner) Run(ctx context.Context, args []string) Result {
	if err := ctx.Err(); err != nil {
		return Result{Code: 1, Error: &CLIError{Code: "cancelled", Message: err.Error()}}
	}
	var stdout, stderr bytes.Buffer
	argv := append([]string{"--json"}, args...)
	code := r.run(argv, &stdout, &stderr)
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
