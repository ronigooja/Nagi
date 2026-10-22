package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/rules"
)

func managedRunner(entries ...rules.Entry) *fakeRunner {
	return &fakeRunner{results: map[string]Result{"rules custom": {Data: managedRules{Profile: "work", Entries: entries}}}}
}

func TestManagedRulesViewOrderAndReadOnly(t *testing.T) {
	lines := []string{}
	for i := 0; i < 12; i++ {
		lines = append(lines, fmt.Sprintf("DOMAIN,n%d.example,DIRECT", i))
	}
	r := managedRunner(rules.Entry{Name: "bundle", Kind: "rules", Enabled: true, Items: lines})
	var out bytes.Buffer
	// Open object, View, next page, first rule, Back through all pages.
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n1\n11\n1\n0\n0\n0\n0\n"), &out), localizedRuleManagementPage(i18n.New(i18n.English)))
	for _, call := range r.calls {
		if !reflect.DeepEqual(call, []string{"rules", "custom"}) {
			t.Fatalf("view mutated: %v", r.calls)
		}
	}
	for _, want := range []string{"bundle [Enabled · Imported rules]", "Rules: 12", "1. DOMAIN,n0.example,DIRECT", "1. DOMAIN,n10.example,DIRECT", "2. DOMAIN,n11.example,DIRECT", "DOMAIN,n10.example,DIRECT"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if strings.Index(out.String(), "n10.example") < strings.Index(out.String(), "n9.example") {
		t.Fatal("view changed source order")
	}
}

func TestManagedDetailActionsFollowStateAndKind(t *testing.T) {
	for _, kind := range []string{"rule", "rules", "set"} {
		for _, enabled := range []bool{true, false} {
			e := rules.Entry{Name: "test", Kind: kind, Enabled: enabled, CanUpdate: true}
			r := managedRunner(e)
			s := NewSession(r, strings.NewReader(""), &bytes.Buffer{})
			view, err := localizedRuleDetailPage(i18n.New(i18n.English), "work", "test").Load(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			labels := []string{}
			for _, item := range view.Actions {
				labels = append(labels, item.Label)
			}
			text := strings.Join(labels, "|")
			if !strings.Contains(text, "View rules") {
				t.Fatal(text)
			}
			if (kind == "rule") != strings.Contains(text, "Edit rule") || (kind != "rule") != strings.Contains(text, "Update from source") {
				t.Fatal(text)
			}
			if enabled && (strings.Contains(text, "Enable") || !strings.Contains(text, "Disable")) {
				t.Fatal(text)
			}
			if !enabled && (strings.Contains(text, "Disable") || !strings.Contains(text, "Enable")) {
				t.Fatal(text)
			}
		}
	}
}

func TestManagedRulesMissingOrChangedProfileHasNoActions(t *testing.T) {
	for _, data := range []managedRules{{Profile: "other", Entries: []rules.Entry{{Name: "test"}}}, {Profile: "work", Entries: []rules.Entry{}}} {
		r := &fakeRunner{results: map[string]Result{"rules custom": {Data: data}}}
		view, err := localizedRuleDetailPage(i18n.New(i18n.English), "work", "test").Load(context.Background(), NewSession(r, strings.NewReader(""), &bytes.Buffer{}))
		if err == nil || len(view.Actions) > 0 {
			t.Fatalf("stale actions: %+v %v", view, err)
		}
	}
}

func TestManagedUpdateRetainsSourceAfterFailure(t *testing.T) {
	r := &sequenceRunner{results: map[string][]Result{
		"rules custom":                     {{Data: managedRules{Profile: "work", Entries: []rules.Entry{{Name: "bundle", Kind: "rules"}}}}},
		"rules update bundle ./rules.yaml": {{Code: 1, Error: &CLIError{Message: "validation rejected"}}, {}},
	}}
	r.results["rules custom"] = append(r.results["rules custom"], r.results["rules custom"][0], r.results["rules custom"][0])
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader("./rules.yaml\n2\n"), &out)
	v, err := localizedRuleDetailPage(i18n.New(i18n.English), "work", "bundle").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range v.Actions {
		if item.ID == "rules.detail.source" {
			if err := item.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := 0
	for _, call := range r.calls {
		if len(call) > 1 && call[1] == "update" {
			count++
			if !reflect.DeepEqual(call, []string{"rules", "update", "bundle", "./rules.yaml"}) {
				t.Fatal(call)
			}
		}
	}
	if count != 2 || !strings.Contains(out.String(), "Values are retained") {
		t.Fatalf("calls=%v out=%s", r.calls, out.String())
	}
}

func TestManagedEditPrefillsAndKeepsDisabledState(t *testing.T) {
	r := managedRunner(rules.Entry{Name: "single", Kind: "rule", Type: "DOMAIN", Payload: "old.example", Target: "DIRECT"})
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader("\n\nREJECT\n"), &out)
	v, err := localizedRuleDetailPage(i18n.New(i18n.English), "work", "single").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range v.Actions {
		if item.ID == "rules.detail.edit" {
			if err := item.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(r.calls[len(r.calls)-1], []string{"rules", "edit", "single", "DOMAIN", "old.example", "REJECT"}) {
		t.Fatal(r.calls)
	}
	for _, want := range []string{"[DOMAIN]", "[old.example]", "[DIRECT]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
}

func TestManagedRulesChineseLabels(t *testing.T) {
	r := managedRunner(rules.Entry{Name: "bundle", Kind: "rules", CanUpdate: true})
	s := NewSessionWithLanguage(r, strings.NewReader(""), &bytes.Buffer{}, i18n.New(i18n.SimplifiedChinese))
	v, err := localizedRuleDetailPage(i18n.New(i18n.SimplifiedChinese), "work", "bundle").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{}
	for _, item := range v.Actions {
		labels = append(labels, item.Label)
	}
	if !reflect.DeepEqual(labels, []string{"查看规则", "从来源更新", "更换来源…", "启用", "删除"}) {
		t.Fatal(labels)
	}
}
