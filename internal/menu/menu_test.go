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

type sequenceRunner struct {
	calls   [][]string
	results map[string][]Result
}

func (f *sequenceRunner) Run(_ context.Context, args []string) Result {
	f.calls = append(f.calls, append([]string(nil), args...))
	key := strings.Join(args, " ")
	queue := f.results[key]
	if len(queue) == 0 {
		return Result{}
	}
	f.results[key] = queue[1:]
	return queue[0]
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
	input := strings.NewReader("wrong\n\n1\n2\n0\n0\n")
	code := Run(context.Background(), NewSession(fake, input, &out))
	if code != 0 {
		t.Fatalf("exit status = %d", code)
	}
	if strings.Count(out.String(), "Choose a listed number") != 1 {
		t.Fatalf("invalid choice feedback missing: %s", out.String())
	}
	if strings.Contains(out.String(), "confirm") {
		t.Fatal("confirmation should not be shown")
	}
	for _, args := range fake.calls {
		if reflect.DeepEqual(args, []string{"stop"}) {
			return
		}
	}
	t.Fatalf("stop not dispatched: %#v", fake.calls)
}

func TestRuntimeModeMapping(t *testing.T) {
	fake := &fakeRunner{results: map[string]Result{"status": {Data: map[string]any{"running": true, "profile": "work"}}, "mode saved": {Data: map[string]any{"mode": "rule"}}}}
	var out bytes.Buffer
	Run(context.Background(), NewSession(fake, strings.NewReader("4\n1\n2\n2\n"), &out))
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
	s := NewSession(&fakeRunner{}, strings.NewReader("11\n1\n"), &out)
	choice, err := s.Select("Names", choices)
	if err != nil || choice != "eleven" {
		t.Fatalf("choice %q, err %v", choice, err)
	}
}

func TestRootShowsEightSectionsAndSettings(t *testing.T) {
	fake := &fakeRunner{}
	var out bytes.Buffer
	Run(context.Background(), NewSession(fake, strings.NewReader("8\n0\n0\n"), &out))
	for _, entry := range rootEntries {
		if !strings.Contains(out.String(), entry.label) {
			t.Fatalf("root section %q absent: %s", entry.label, out.String())
		}
	}
	if !strings.Contains(out.String(), "Maintenance\n") || !strings.Contains(out.String(), "\nMaintenance\n") {
		t.Fatalf("advanced section was not selectable: %s", out.String())
	}
}

func TestFailedRefreshDropsStaleChoices(t *testing.T) {
	fake := &sequenceRunner{results: map[string][]Result{
		"profile list": {
			{Data: map[string]any{"profiles": []string{"old"}}},
			{Code: 1, Error: &CLIError{Code: "profile_error", Message: "fetch failed: https://secret.example/token"}},
		},
	}}
	var out bytes.Buffer
	Run(context.Background(), NewSession(fake, strings.NewReader("5\n1\n0\n1\n3\n0\n0\n0\n"), &out))
	if !strings.Contains(out.String(), "Data unavailable. Return to the parent page") || !strings.Contains(out.String(), "Choose a listed number") {
		t.Fatalf("failed refresh did not clear stale choices: %s", out.String())
	}
	if !strings.Contains(out.String(), "https://secret.example/token") {
		t.Fatalf("refresh error lost URL context: %s", out.String())
	}
}

func TestReturningToParentRefreshesList(t *testing.T) {
	fake := &sequenceRunner{results: map[string][]Result{
		"profile list": {
			{Data: map[string]any{"profiles": []string{"old"}}},
			{Data: map[string]any{"profiles": []string{"old"}}},
			{Data: map[string]any{"profiles": []string{"new"}}},
		},
	}}
	var out bytes.Buffer
	Run(context.Background(), NewSession(fake, strings.NewReader("5\n1\n1\n0\n0\n0\n0\n"), &out))
	if !strings.Contains(out.String(), "1. new") {
		t.Fatalf("parent list not refreshed after back: %s", out.String())
	}
}

func TestCommandErrorPreservesURLContext(t *testing.T) {
	fake := &fakeRunner{results: map[string]Result{
		"status": {Data: map[string]any{"running": false}},
		"start":  {Code: 1, Error: &CLIError{Message: "failed https://secret.example/token"}},
	}}
	var out bytes.Buffer
	Run(context.Background(), NewSession(fake, strings.NewReader("1\n1\n0\n0\n"), &out))
	if !strings.Contains(out.String(), "Error: failed https://secret.example/token") {
		t.Fatalf("command error lost URL context: %s", out.String())
	}
}
