package menu

import (
	"bytes"
	"context"
	"os"
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

func TestRuleImportMenuJourneys(t *testing.T) {
	for _, source := range []string{"./personal rules.yaml", "https://example.com/rules.yaml"} {
		t.Run(source, func(t *testing.T) {
			runner := &fakeRunner{}
			var out bytes.Buffer
			input := "4\npersonal\n" + source + "\n0\n"
			if code := RunPage(context.Background(), NewSession(runner, strings.NewReader(input), &out), RulesPage()); code != 0 {
				t.Fatalf("exit=%d out=%s", code, out.String())
			}
			var imports [][]string
			for _, args := range runner.calls {
				if len(args) > 1 && args[1] == "import" {
					imports = append(imports, args)
				}
			}
			if !reflect.DeepEqual(imports, [][]string{{"rules", "import", "personal", source}}) {
				t.Fatalf("calls=%v out=%s", runner.calls, out.String())
			}
			for _, unwanted := range []string{"Rule file format", "Classical rule set", "Target (DIRECT"} {
				if strings.Contains(out.String(), unwanted) {
					t.Fatalf("unexpected prompt %q: %s", unwanted, out.String())
				}
			}
			for _, want := range []string{"4. Import rules", "Rules imported."} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
		})
	}
}

func TestRuleImportRetainsNameAndSourceAfterFailure(t *testing.T) {
	runner := &sequenceRunner{results: map[string][]Result{"rules import personal ./rules.yaml": {
		{Code: 1, Error: &CLIError{Message: "validation failed"}}, {},
	}}}
	var out bytes.Buffer
	if err := ruleImport(context.Background(), NewSession(runner, strings.NewReader("personal\n./rules.yaml\n1\n\n2\n\n3\n"), &out)); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[0], runner.calls[1]) || !strings.Contains(out.String(), "Values are retained") || !strings.Contains(out.String(), "blank keeps current value") {
		t.Fatalf("calls=%v out=%s", runner.calls, out.String())
	}
}

func TestRuleImportCancellationAndEOFDoNotSubmit(t *testing.T) {
	for _, input := range []string{"", "personal\n"} {
		runner := &fakeRunner{}
		err := ruleImport(context.Background(), NewSession(runner, strings.NewReader(input), &bytes.Buffer{}))
		if err != ErrQuit || len(runner.calls) != 0 {
			t.Fatalf("input=%q err=%v calls=%v", input, err, runner.calls)
		}
	}
	canceled := &fakeRunner{}
	s := NewSession(canceled, strings.NewReader("personal\n./rules.yaml\n"), &bytes.Buffer{})
	s.interrupts = make(chan os.Signal, 1)
	s.interrupts <- os.Interrupt
	if err := ruleImport(context.Background(), s); err != ErrBack || len(canceled.calls) != 0 {
		t.Fatalf("interrupt: err=%v calls=%v", err, canceled.calls)
	}
	// Cancel or EOF after failure never retries the already submitted mutation.
	for _, ending := range []string{"0\n", ""} {
		runner := &fakeRunner{results: map[string]Result{"rules import personal ./rules.yaml": {Code: 1}}}
		err := ruleImport(context.Background(), NewSession(runner, strings.NewReader("personal\n./rules.yaml\n"+ending), &bytes.Buffer{}))
		if (err != ErrBack && err != ErrQuit) || len(runner.calls) != 1 {
			t.Fatalf("err=%v calls=%v", err, runner.calls)
		}
	}
}

func TestRuleManagementOffersWholeNamedObjects(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"rules custom": {Data: map[string]any{"profile": "work", "entries": []map[string]any{
		{"name": "single", "kind": "rule"}, {"name": "bundle", "kind": "rules"}, {"name": "set", "kind": "set"},
	}}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("3\n2\n4\n0\n0\n"), &out), RulesPage())
	var removes [][]string
	for _, args := range runner.calls {
		if len(args) > 1 && args[1] == "remove" {
			removes = append(removes, args)
		}
	}
	if !reflect.DeepEqual(removes, [][]string{{"rules", "remove", "bundle"}}) || !strings.Contains(out.String(), "All rules:") {
		t.Fatalf("calls=%v out=%s", runner.calls, out.String())
	}
}

func TestRulesPageShowsProfileOwnership(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"rules custom": {Data: map[string]any{"profile": "work", "entries": []rules.Entry{}}}}}
	var out bytes.Buffer
	if code := RunPage(context.Background(), NewSession(runner, strings.NewReader("0\n"), &out), RulesPage()); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "Selected profile: work") || !strings.Contains(out.String(), "Custom rules and sets: 0") {
		t.Fatalf("ownership missing: %s", out.String())
	}
}
