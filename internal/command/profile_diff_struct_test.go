package command

import (
	"strings"
	"testing"
)

func TestStructuralDiffUsesNamedEntriesAndWarnings(t *testing.T) {
	before := []byte("proxies:\n  - name: A\n    server: old.example\n  - name: B\n    server: keep.example\nrules:\n  - MATCH,DIRECT\ndns:\n  nameserver:\n    - https://old.example/dns-query\n")
	after := []byte("proxies:\n  - name: B\n    server: keep.example\n  - name: A\n    server: new.example\nrules:\n  - DOMAIN,example.com,REJECT\n  - MATCH,DIRECT\ndns:\n  nameserver:\n    - https://new.example/dns-query\n")
	changes, warnings, err := structuralDiff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	orderFound := false
	for _, c := range changes {
		if c.Path == "/proxies/A/server" && c.Kind == "modified" {
			found = true
		}
		if c.Path == "/proxies" && c.Kind == "order_changed" {
			orderFound = true
		}
		if strings.HasPrefix(c.Path, "/proxies/0") {
			t.Fatalf("index based proxy diff: %+v", changes)
		}
	}
	if !found || !orderFound || len(warnings) < 2 {
		t.Fatalf("changes=%+v warnings=%+v", changes, warnings)
	}
}
func TestStructuralDiffOnlyNamedOrderChanged(t *testing.T) {
	before := []byte("proxies:\n  - name: A\n  - name: B\n")
	after := []byte("proxies:\n  - name: B\n  - name: A\n")
	changes, _, err := structuralDiff(before, after)
	if err != nil || len(changes) != 1 || changes[0].Path != "/proxies" || changes[0].Kind != "order_changed" {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
}
func TestStructuralDiffReportsTypeChange(t *testing.T) {
	changes, warnings, err := structuralDiff([]byte("mixed-port: 7890\n"), []byte("mixed-port: text\n"))
	if err != nil || len(changes) != 1 || changes[0].Kind != "type_change" || len(warnings) != 1 {
		t.Fatalf("%+v %+v %v", changes, warnings, err)
	}
}
func TestStructuralDiffRejectsValuesJSONCannotRepresent(t *testing.T) {
	_, _, err := structuralDiff([]byte("a: 1\n"), []byte("a:\n  1: value\n"))
	if err == nil || !strings.Contains(err.Error(), "cannot be represented in JSON") {
		t.Fatal(err)
	}
}
