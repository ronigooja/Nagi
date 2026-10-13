package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/rules"
)

func TestRuleTestMenuJourney(t *testing.T) {
	result := Result{Data: rules.TestResult{Target: "154.19.187.117", Port: 443, Network: "tcp", Mode: "rule", Matched: true, Rule: "RuleSet", RulePayload: "nagi-personal", Providers: []string{"nagi-personal"}, Outbound: "DIRECT", Warnings: []string{"Synthetic destination test"}}}
	runner := &fakeRunner{results: map[string]Result{"rules test 154.19.187.117": result}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("1\nhttps://invalid.example\n154.19.187.117\n0\n"), &out)
	if code := RunPage(context.Background(), s, RulesPage()); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	var tests [][]string
	for _, args := range runner.calls {
		if len(args) > 1 && args[1] == "test" {
			tests = append(tests, args)
		}
	}
	if !reflect.DeepEqual(tests, [][]string{{"rules", "test", "154.19.187.117"}}) {
		t.Fatalf("dispatch: %v", runner.calls)
	}
	for _, want := range []string{"1. Test Rule", "Invalid value:", "Matched: RuleSet nagi-personal -> DIRECT", "Synthetic destination test"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out.String())
		}
	}
}

func TestRuleTestMenuRetainsInputAfterFailure(t *testing.T) {
	runner := &sequenceRunner{results: map[string][]Result{"rules test example.com": {
		{Code: 1, Error: &CLIError{Code: "rule_error", Message: "DNS failed"}},
		{Data: rules.TestResult{Target: "example.com", Outbound: "DIRECT"}},
	}}}
	var out bytes.Buffer
	if err := ruleTest(context.Background(), NewSession(runner, strings.NewReader("example.com\n2\n"), &out)); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[0], runner.calls[1]) || !strings.Contains(out.String(), "Values are retained") {
		t.Fatalf("calls=%v out=%s", runner.calls, out.String())
	}
	// EOF before submission sends no request.
	empty := &fakeRunner{}
	if err := ruleTest(context.Background(), NewSession(empty, strings.NewReader(""), &out)); err != ErrQuit || len(empty.calls) != 0 {
		t.Fatalf("EOF: %v calls=%v", err, empty.calls)
	}
}
