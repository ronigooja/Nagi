package menu

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls   [][]string
	results map[string]Result
}

func (f *fakeRunner) Run(_ context.Context, args []string) Result {
	f.calls = append(f.calls, append([]string(nil), args...))
	if result, ok := f.results[strings.Join(args, " ")]; ok {
		return result
	}
	return Result{Data: map[string]any{"ok": true}}
}

func TestMenuRuntimeNavigation(t *testing.T) {
	fake := &fakeRunner{results: map[string]Result{"status": {Data: map[string]any{"running": false, "profile": "default"}}}}
	var out bytes.Buffer
	input := strings.NewReader("wrong\n\n1\n3\nno\n3\nyes\nb\nq\n")
	code := Run(context.Background(), NewSession(fake, input, &out))
	if code != 0 {
		t.Fatalf("exit status = %d", code)
	}
	if strings.Count(out.String(), "Choose a listed number") != 1 {
		t.Fatalf("invalid choice feedback missing: %s", out.String())
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatal("confirmation cancellation missing")
	}
	for _, args := range fake.calls {
		if reflect.DeepEqual(args, []string{"stop"}) {
			return
		}
	}
	t.Fatalf("confirmed stop not dispatched: %#v", fake.calls)
}

func TestRuntimeModeMapping(t *testing.T) {
	fake := &fakeRunner{results: map[string]Result{"status": {Data: map[string]any{"running": true, "profile": "work"}}, "mode saved": {Data: map[string]any{"mode": "rule"}}}}
	var out bytes.Buffer
	Run(context.Background(), NewSession(fake, strings.NewReader("1\n9\n2\nq\n"), &out))
	want := []string{"mode", "save", "global"}
	for _, args := range fake.calls {
		if reflect.DeepEqual(args, want) {
			return
		}
	}
	t.Fatalf("persistent mode command not dispatched: %#v", fake.calls)
}

func TestCommandRunnerJSON(t *testing.T) {
	runner := NewCommandRunner(func(args []string, out, errOut io.Writer) int {
		if !reflect.DeepEqual(args, []string{"--json", "version"}) {
			t.Fatalf("args = %#v", args)
		}
		io.WriteString(out, `{"ok":true,"data":{"version":"dev"}}`)
		return 0
	})
	result := runner.Run(context.Background(), []string{"version"})
	if result.Code != 0 || result.Error != nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Data.(map[string]any)["version"] != "dev" {
		t.Fatalf("data = %#v", result.Data)
	}
}

func TestSelectPaginates(t *testing.T) {
	choices := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven"}
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader("n\n1\n"), &out)
	choice, err := s.Select("Names", choices)
	if err != nil || choice != "eleven" {
		t.Fatalf("choice %q, err %v", choice, err)
	}
}
