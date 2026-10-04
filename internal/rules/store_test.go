package rules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlayOrderAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil)
	ctx := context.Background()
	if err := s.Add(ctx, Entry{Name: "ads", Enabled: true, Kind: "rule", Type: "DOMAIN-SUFFIX", Payload: "ads.example", Target: "REJECT"}); err != nil {
		t.Fatal(err)
	}
	base := []byte("rules:\n  - MATCH,DIRECT\n")
	effective, err := Apply(base, dir)
	if err != nil {
		t.Fatal(err)
	}
	report, err := AnalyzeEffective(effective)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Order) != 2 || report.Order[0] != "DOMAIN-SUFFIX,ads.example,REJECT" || len(report.Conflicts) != 0 {
		t.Fatalf("report %+v", report)
	}
	if err := s.SetEnabled(ctx, "ads", false); err != nil {
		t.Fatal(err)
	}
	effective, err = Apply(base, dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(effective), "ads.example") {
		t.Fatal(string(effective))
	}
	if err := s.SetEnabled(ctx, "ads", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, "ads"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatal(err)
	}
}
func TestRollbackOnValidationFailure(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, func(context.Context) error { return errors.New("reject") })
	err := s.Add(context.Background(), Entry{Name: "bad", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "DIRECT"})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("journal persisted: %v", err)
	}
}
func TestSetImportAndConflicts(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "set.yaml")
	if err := os.WriteFile(file, []byte("payload:\n  - example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil)
	if err := s.ImportLocal(context.Background(), "domains", "domain", file, "DIRECT"); err != nil {
		t.Fatal(err)
	}
	result, err := Apply([]byte("rules:\n  - RULE-SET,nagi-domains,REJECT\n  - MATCH,DIRECT\n  - DOMAIN,late.example,REJECT\n"), dir)
	if err != nil {
		t.Fatal(err)
	}
	report, err := AnalyzeEffective(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Conflicts) < 2 {
		t.Fatalf("conflicts %+v", report)
	}
	if !strings.Contains(string(result), "rule-providers:") {
		t.Fatal(string(result))
	}
}
