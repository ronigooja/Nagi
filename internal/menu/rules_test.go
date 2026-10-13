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

func TestRuleImportMenuJourneys(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
		withTarget  bool
	}{
		{"rules", "4\n1\npersonal\n./personal rules.yaml\n0\n", []string{"rules", "import", "personal", "rules", "./personal rules.yaml"}, false},
		{"classical", "4\n2\nset\nhttps://example.com/rules.yaml\nProxy Group\n0\n", []string{"rules", "import", "set", "classical", "https://example.com/rules.yaml", "Proxy Group"}, true},
		{"domain", "4\n3\nset\n./domains.txt\nREJECT\n0\n", []string{"rules", "import", "set", "domain", "./domains.txt", "REJECT"}, true},
		{"ipcidr", "4\n4\nset\n./networks.txt\nDIRECT\n0\n", []string{"rules", "import", "set", "ipcidr", "./networks.txt", "DIRECT"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			var out bytes.Buffer
			if code := RunPage(context.Background(), NewSession(runner, strings.NewReader(tc.input), &out), RulesPage()); code != 0 {
				t.Fatalf("exit=%d out=%s", code, out.String())
			}
			var imports [][]string
			for _, args := range runner.calls {
				if len(args) > 1 && args[1] == "import" {
					imports = append(imports, args)
				}
			}
			if !reflect.DeepEqual(imports, [][]string{tc.want}) {
				t.Fatalf("calls=%v out=%s", runner.calls, out.String())
			}
			if strings.Contains(out.String(), "Target (DIRECT") != tc.withTarget {
				t.Fatalf("target prompt for %s: %s", tc.name, out.String())
			}
			for _, want := range []string{"4. Import rule file", "Rule file format", "Rule file imported."} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
		})
	}
}

func TestRuleImportRetainsNameSourceAndTargetAfterFailure(t *testing.T) {
	for _, tc := range []struct{ input, command string }{
		{"1\nbundle\n./rules.yaml\n1\n\n2\n\n3\n", "rules import bundle rules ./rules.yaml"},
		{"3\nset\n./domains.txt\nREJECT\n1\n\n2\n\n3\n\n4\n", "rules import set domain ./domains.txt REJECT"},
	} {
		runner := &sequenceRunner{results: map[string][]Result{tc.command: {
			{Code: 1, Error: &CLIError{Message: "validation failed"}}, {},
		}}}
		var out bytes.Buffer
		if err := ruleImport(context.Background(), NewSession(runner, strings.NewReader(tc.input), &out)); err != nil {
			t.Fatal(err)
		}
		if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[0], runner.calls[1]) || !strings.Contains(out.String(), "Values are retained") || !strings.Contains(out.String(), "blank keeps current value") {
			t.Fatalf("calls=%v out=%s", runner.calls, out.String())
		}
	}
}

func TestRuleImportCancellationAndEOFDoNotSubmit(t *testing.T) {
	for _, input := range []string{"0\n", "", "1\n", "1\nbundle\n", "3\nset\n./domains.txt\n"} {
		runner := &fakeRunner{}
		var out bytes.Buffer
		err := ruleImport(context.Background(), NewSession(runner, strings.NewReader(input), &out))
		if (err != ErrBack && err != ErrQuit) || len(runner.calls) != 0 {
			t.Fatalf("input=%q err=%v calls=%v", input, err, runner.calls)
		}
	}
	// Cancel after failure never retries the already submitted mutation.
	runner := &fakeRunner{results: map[string]Result{"rules import bundle rules ./rules.yaml": {Code: 1}}}
	if err := ruleImport(context.Background(), NewSession(runner, strings.NewReader("1\nbundle\n./rules.yaml\n0\n"), &bytes.Buffer{})); err != ErrBack || len(runner.calls) != 1 {
		t.Fatalf("err=%v calls=%v", err, runner.calls)
	}
}

func TestRuleManagementOffersWholeNamedObjects(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"rules custom": {Data: map[string]any{"entries": []map[string]any{
		{"name": "single", "kind": "rule"}, {"name": "bundle", "kind": "rules"}, {"name": "set", "kind": "set"},
	}}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("3\n4\n2\n0\n0\n"), &out), RulesPage())
	var removes [][]string
	for _, args := range runner.calls {
		if len(args) > 1 && args[1] == "remove" {
			removes = append(removes, args)
		}
	}
	if !reflect.DeepEqual(removes, [][]string{{"rules", "remove", "bundle"}}) || !strings.Contains(out.String(), "Removes the whole named object") {
		t.Fatalf("calls=%v out=%s", runner.calls, out.String())
	}
}
