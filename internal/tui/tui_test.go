package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type fakeBackend struct {
	snapshot Snapshot
	calls    []string
	err      error
}

func (f *fakeBackend) Load() (Snapshot, error) { return f.snapshot, f.err }
func (f *fakeBackend) Do(command string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(append([]string{command}, args...), "|"))
	return "completed", nil
}
func TestJourney(t *testing.T) {
	b := &fakeBackend{snapshot: Snapshot{Running: true, Profile: "work", Mode: "rule", DNS: "enabled", Groups: []Group{{Name: "group", Now: "Alpha", Nodes: []string{"Alpha", "Beta"}}}, Subscriptions: []string{"sub"}}}
	var out bytes.Buffer
	app := &App{In: strings.NewReader("/bet\rsduamq"), Out: &out, Backend: b}
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.calls, ","); got != "proxy|select|group|Beta,proxy|delay|Beta,subscription|update|sub,subscription|apply|sub,mode|global" {
		t.Fatalf("calls = %q", got)
	}
	if !strings.Contains(out.String(), "Search nodes") {
		t.Fatalf("missing user feedback: %q", out.String())
	}
}
func TestEmptyAndErrorStates(t *testing.T) {
	b := &fakeBackend{snapshot: Snapshot{}}
	var out bytes.Buffer
	app := &App{In: strings.NewReader("sq"), Out: &out, Backend: b}
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No proxy groups available", "No saved subscriptions", "Choose a node first"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	b.err = errors.New("control failed")
	app = &App{In: strings.NewReader("q"), Out: &out, Backend: b}
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "control failed") {
		t.Fatal("load error missing")
	}
}
func TestFilterAndEscape(t *testing.T) {
	nodes := FilterNodes([]string{"Alpha", "BETA", "Gamma"}, "bet")
	if len(nodes) != 1 || nodes[0] != "BETA" {
		t.Fatalf("nodes=%v", nodes)
	}
	b := &fakeBackend{snapshot: Snapshot{Groups: []Group{{Name: "bad\x1b[31m", Nodes: []string{"x\x1b[0m"}}}}}
	var out bytes.Buffer
	app := &App{In: strings.NewReader("q"), Out: &out, Backend: b}
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "bad\x1b") {
		t.Fatal("untrusted escape rendered")
	}
}
