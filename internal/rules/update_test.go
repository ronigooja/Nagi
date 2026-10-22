package rules

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUpdatePreservesOrderStateAndPrivateSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	body := "rules: ['DOMAIN,first.example,DIRECT']"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "PRIVATE" {
			t.Error("saved URL lost query")
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	s := New(dir, "work", nil)
	if err := s.Import(ctx, "bundle", server.URL+"/rules?token=PRIVATE"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, Entry{Name: "last", Kind: "rule", Enabled: true, Type: "MATCH", Target: "DIRECT"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(ctx, "bundle", false); err != nil {
		t.Fatal(err)
	}
	body = "rules: ['DOMAIN,second.example,REJECT', 'IP-CIDR,192.0.2.0/24,DIRECT,no-resolve']"
	// A fresh store proves the source survives a process boundary.
	s = New(dir, "work", nil)
	if err := s.Update(ctx, "bundle", ""); err != nil {
		t.Fatal(err)
	}
	doc, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Entries[0].Name != "bundle" || doc.Entries[0].Enabled || !doc.Entries[0].CanUpdate || doc.Entries[1].Name != "last" || !reflect.DeepEqual(doc.Entries[0].Items, []string{"DOMAIN,second.example,REJECT", "IP-CIDR,192.0.2.0/24,DIRECT,no-resolve"}) {
		t.Fatalf("unexpected update: %+v", doc)
	}
	public, err := json.Marshal(doc)
	if err != nil || strings.Contains(string(public), "PRIVATE") || strings.Contains(string(public), "refresh-source") {
		t.Fatalf("private source exposed: %s (%v)", public, err)
	}
	info, err := os.Stat(Path(dir, "work"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
	other, err := New(dir, "other", nil).List()
	if err != nil || len(other.Entries) != 0 {
		t.Fatal("profile isolation lost")
	}
}

func TestUpdateLocalLegacySetAndRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "source.yaml")
	write := func(data string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, "work", nil)
	legacy := Entry{Name: "legacy", Kind: "set", Enabled: true, Behavior: "domain", Target: "REJECT", Items: []string{"old.example"}, Source: "local:old.yaml"}
	if err := s.Add(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, "legacy", ""); err == nil {
		t.Fatal("legacy summary treated as real source")
	}
	write("payload: ['first.example', 'second.example']")
	if err := s.Update(ctx, "legacy", file); err != nil {
		t.Fatal(err)
	}
	doc, _ := s.List()
	e := doc.Entries[0]
	if e.Kind != "set" || e.Behavior != "domain" || e.Target != "REJECT" || !e.Enabled || e.RefreshSource != file || !reflect.DeepEqual(e.Items, []string{"first.example", "second.example"}) {
		t.Fatalf("legacy update: %+v", e)
	}
	previous, _ := os.ReadFile(Path(dir, "work"))
	write("payload: ['changed.example']")
	s.Check = func(context.Context) error { return errors.New("reload rejected") }
	if err := s.Update(ctx, "legacy", ""); err == nil {
		t.Fatal("expected reload failure")
	}
	after, _ := os.ReadFile(Path(dir, "work"))
	if string(after) != string(previous) {
		t.Fatal("rollback changed stored data")
	}
	s.Check = nil
	write("payload: []")
	if err := s.Update(ctx, "legacy", ""); err == nil {
		t.Fatal("empty set accepted")
	}
	after, _ = os.ReadFile(Path(dir, "work"))
	if string(after) != string(previous) {
		t.Fatal("parse failure changed data")
	}
	if err := s.Edit(ctx, Entry{Name: "legacy", Type: "MATCH", Target: "DIRECT"}); err == nil {
		t.Fatal("edit converted a set")
	}
}

func TestEditSingleRulePreservesPositionAndRollback(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir(), "work", nil)
	for _, name := range []string{"first", "last"} {
		if err := s.Add(ctx, Entry{Name: name, Kind: "rule", Type: "DOMAIN", Payload: "old.example", Target: "DIRECT"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Edit(ctx, Entry{Name: "first", Type: "MATCH", Target: "REJECT"}); err != nil {
		t.Fatal(err)
	}
	doc, _ := s.List()
	if doc.Entries[0].Enabled || doc.Entries[0].Type != "MATCH" || doc.Entries[1].Name != "last" {
		t.Fatalf("edit changed state/order: %+v", doc)
	}
	if err := s.Update(ctx, "first", ""); err == nil {
		t.Fatal("update accepted a single rule")
	}
	s.Check = func(context.Context) error { return errors.New("validation rejected") }
	if err := s.Edit(ctx, Entry{Name: "first", Type: "DOMAIN", Payload: "new.example", Target: "DIRECT"}); err == nil {
		t.Fatal("expected validation failure")
	}
	after, _ := s.List()
	if !reflect.DeepEqual(doc, after) {
		t.Fatal("edit rollback failed")
	}
}
