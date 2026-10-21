package rules

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOverlayOrderAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "work", nil)
	ctx := context.Background()
	if err := s.Add(ctx, Entry{Name: "ads", Enabled: true, Kind: "rule", Type: "DOMAIN-SUFFIX", Payload: "ads.example", Target: "REJECT"}); err != nil {
		t.Fatal(err)
	}
	base := []byte("rules:\n  - MATCH,DIRECT\n")
	effective, err := Apply(base, dir, "work")
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
	effective, err = Apply(base, dir, "work")
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
	if _, err := os.Stat(Path(dir, "work")); err != nil {
		t.Fatal(err)
	}
}
func TestRollbackOnValidationFailure(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "work", func(context.Context) error { return errors.New("reject") })
	err := s.Add(context.Background(), Entry{Name: "bad", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "DIRECT"})
	if err == nil {
		t.Fatal("expected error")
	}
	var rejected *CheckError
	if !errors.As(err, &rejected) || rejected.RollbackErr != nil {
		t.Fatalf("missing confirmed restoration: %v", err)
	}
	if _, err := os.Stat(Path(dir, "work")); !os.IsNotExist(err) {
		t.Fatalf("journal persisted: %v", err)
	}
}

func TestCheckErrorDoesNotClaimSuccessfulRestoration(t *testing.T) {
	dir := t.TempDir()
	cause := errors.New("validation rejected")
	s := New(dir, "work", nil)
	s.Check = func(context.Context) error {
		path := Path(dir, "work")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "block-removal"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		return cause
	}
	err := s.Add(context.Background(), Entry{Name: "bad", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "DIRECT"})
	var rejected *CheckError
	if !errors.As(err, &rejected) || rejected.RollbackErr == nil || !errors.Is(err, cause) {
		t.Fatalf("lost restoration failure: %v", err)
	}
}
func TestPersistedSetAndConflicts(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "work", nil)
	if err := s.Add(context.Background(), Entry{Name: "domains", Kind: "set", Enabled: true, Behavior: "domain", Items: []string{"example.com"}, Target: "DIRECT", Source: "local:set.yaml"}); err != nil {
		t.Fatal(err)
	}

	result, err := Apply([]byte("rules:\n  - RULE-SET,nagi-domains,REJECT\n  - MATCH,DIRECT\n  - DOMAIN,late.example,REJECT\n"), dir, "work")
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

func TestRulesBundlePersistenceAndLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "bundle.yaml")
	lines := []string{"DOMAIN,work.example,Work", "AND,((DOMAIN,a.example),(DST-PORT,443)),DIRECT", "DOMAIN-REGEX,^a,b$,REJECT", "IP-CIDR,192.0.2.0/24,DIRECT,no-resolve", "MATCH,REJECT"}
	data := "proxies:\n  - name: ignored\n    type: direct\nrule-providers:\n  ignored:\n    type: inline\n    behavior: domain\n    payload: [ignored.example]\nrules:\n"
	for _, line := range lines {
		data += "  - '" + line + "'\n"
	}
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	store := New(dir, "work", nil)
	if err := store.Import(ctx, "bundle", file); err != nil {
		t.Fatal(err)
	}
	saved, err := New(dir, "work", nil).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Entries) != 1 || saved.Entries[0].Kind != "rules" || saved.Entries[0].Target != "" || !reflect.DeepEqual(saved.Entries[0].Items, lines) {
		t.Fatalf("saved: %+v", saved)
	}
	base := []byte("rules: ['MATCH,DIRECT']\n")
	checkOrder := func(want []string) {
		t.Helper()
		effective, err := Apply(base, dir, "work")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(effective), "ignored") {
			t.Fatal("imported non-rules source fields")
		}
		report, err := AnalyzeEffective(effective)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(report.Order, want) {
			t.Fatalf("order: %#v, want %#v", report.Order, want)
		}
	}
	checkOrder(append(append([]string{}, lines...), "MATCH,DIRECT"))
	if err := store.SetEnabled(ctx, "bundle", false); err != nil {
		t.Fatal(err)
	}
	checkOrder([]string{"MATCH,DIRECT"})
	if err := store.SetEnabled(ctx, "bundle", true); err != nil {
		t.Fatal(err)
	}
	checkOrder(append(append([]string{}, lines...), "MATCH,DIRECT"))
	if err := store.Remove(ctx, "bundle"); err != nil {
		t.Fatal(err)
	}
	checkOrder([]string{"MATCH,DIRECT"})
}

func TestRulesImportRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := New(dir, "work", nil)
	if err := store.Add(ctx, Entry{Name: "old", Kind: "rule", Type: "MATCH", Target: "DIRECT", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(Path(dir, "work"))
	file := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(file, []byte("rules: ['UNKNOWN,native,target']"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	store.Check = func(context.Context) error { calls++; return errors.New("mihomo rejected unknown rule") }
	if err := store.Import(ctx, "bad", file); err == nil {
		t.Fatal("expected engine validation error")
	}
	after, _ := os.ReadFile(Path(dir, "work"))
	if calls != 1 || string(before) != string(after) {
		t.Fatalf("rollback failed, checks=%d", calls)
	}
}

func TestRulesImportRemoteSnapshotAndBound(t *testing.T) {
	ctx := context.Background()
	body := "rules: ['DOMAIN,a.example,DIRECT']"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	dir := t.TempDir()
	store := New(dir, "work", nil)
	if err := store.Import(ctx, "remote", server.URL); err != nil {
		t.Fatal(err)
	}
	body = "rules: ['MATCH,REJECT']"
	effective, err := Apply([]byte("rules: []"), dir, "work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(effective), "DOMAIN,a.example,DIRECT") || strings.Contains(string(effective), "MATCH,REJECT") {
		t.Fatal(string(effective))
	}
	before, _ := os.ReadFile(Path(dir, "work"))
	body = strings.Repeat("x", maxImportBytes+1)
	if err := store.Import(ctx, "large", server.URL); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("size error: %v", err)
	}
	after, _ := os.ReadFile(Path(dir, "work"))
	if string(before) != string(after) {
		t.Fatal("failed download changed persisted rules")
	}
}

func TestRulesImportInvalidSourcesAndData(t *testing.T) {
	for _, data := range []string{"", "rules: []", "payload: ['MATCH,DIRECT']", "rules: ['MATCH,DIRECT']\n---\nrules: ['MATCH,REJECT']", "rules: [true]", "rules: [123]", "rules: ['']", "rules: [\"a\\nb\"]", "rules: ['MATCH,DIRECT']\nrules: ['MATCH,REJECT']", "["} {
		t.Run(data, func(t *testing.T) {
			if _, err := ParseRules([]byte(data)); err == nil {
				t.Fatalf("accepted %q", data)
			}
		})
	}
	for _, source := range []string{"", "http:/broken", "ftp://example.invalid/rules", "https://user:secret@example.invalid/rules", filepath.Join(t.TempDir(), "missing")} {
		if err := New(t.TempDir(), "work", nil).Import(context.Background(), "bad", source); err == nil {
			t.Fatalf("accepted source %q", source)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "large")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", maxImportBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := New(dir, "work", nil).Import(context.Background(), "large", file); err == nil {
		t.Fatal("accepted oversized file")
	}

	items, err := ParseRules([]byte("['MATCH,DIRECT']"))
	if err != nil || !reflect.DeepEqual(items, []string{"MATCH,DIRECT"}) {
		t.Fatalf("list: %v %v", items, err)
	}
}

func TestBundleTargetsFollowMihomoFields(t *testing.T) {
	entry := Entry{Kind: "rules", Items: []string{"IP-CIDR,192.0.2.0/24,Proxy,no-resolve", "OR,((DOMAIN,a.example),(DOMAIN,b.example)),Other", "DOMAIN-REGEX,^a,b$,Regex", "MATCH,REJECT", "SUB-RULE,(NETWORK,TCP),sub", "DST-PORT,80,Port"}}
	want := []string{"Proxy", "Other", "Regex", "REJECT", "Port"}
	if got := Targets(entry); !reflect.DeepEqual(got, want) {
		t.Fatalf("targets %v", got)
	}
}

func TestLegacySetsRemainManageable(t *testing.T) {
	ctx := context.Background()
	for _, behavior := range []string{"classical", "domain", "ipcidr"} {
		t.Run(behavior, func(t *testing.T) {
			dir := t.TempDir()
			store := New(dir, "work", nil)
			item := map[string]string{"classical": "DOMAIN,example.com", "domain": "+.example.com", "ipcidr": "192.0.2.0/24"}[behavior]
			original := Entry{Name: "legacy", Kind: "set", Enabled: true, Behavior: behavior, Items: []string{item}, Target: "DIRECT", Source: "remote:example.invalid"}
			if err := store.Save(ctx, Document{Entries: []Entry{original}}); err != nil {
				t.Fatal(err)
			}
			doc, err := New(dir, "work", nil).List()
			if err != nil || !reflect.DeepEqual(doc.Entries, []Entry{original}) {
				t.Fatalf("legacy read: %+v %v", doc, err)
			}
			check := func(enabled bool) {
				t.Helper()
				data, err := Apply([]byte("rules: ['MATCH,DIRECT']"), dir, "work")
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "RULE-SET,nagi-legacy,DIRECT") != enabled {
					t.Fatalf("legacy activation=%v: %s", enabled, data)
				}
				if enabled && (!strings.Contains(string(data), "behavior: "+behavior) || !strings.Contains(string(data), item)) {
					t.Fatalf("legacy provider changed: %s", data)
				}
			}
			check(true)
			if err := store.SetEnabled(ctx, "legacy", false); err != nil {
				t.Fatal(err)
			}
			check(false)
			if err := store.SetEnabled(ctx, "legacy", true); err != nil {
				t.Fatal(err)
			}
			check(true)
			if err := store.Remove(ctx, "legacy"); err != nil {
				t.Fatal(err)
			}
			check(false)
			doc, err = store.List()
			if err != nil || len(doc.Entries) != 0 {
				t.Fatalf("legacy removal: %+v %v", doc, err)
			}
		})
	}
}

func TestProfileRulesIsolationAndRollback(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	entry := Entry{Name: "shared", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "work.example", Target: "DIRECT"}
	work := New(dir, "work", nil)
	home := New(dir, "home", nil)
	if err := work.Add(ctx, entry); err != nil {
		t.Fatal(err)
	}
	entry.Payload = "home.example"
	if err := home.Add(ctx, entry); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir, "work"))
	if err != nil {
		t.Fatal(err)
	}
	work.Check = func(context.Context) error { return errors.New("reload failed") }
	if err := work.SetEnabled(ctx, "shared", false); err == nil {
		t.Fatal("expected failed reload")
	}
	after, _ := os.ReadFile(Path(dir, "work"))
	if string(before) != string(after) {
		t.Fatal("work rules not restored")
	}
	if err := home.Remove(ctx, "shared"); err != nil {
		t.Fatal(err)
	}
	workDoc, err := work.List()
	if err != nil || len(workDoc.Entries) != 1 || workDoc.Entries[0].Payload != "work.example" {
		t.Fatalf("work rules changed: %+v %v", workDoc, err)
	}
	homeDoc, err := home.List()
	if err != nil || len(homeDoc.Entries) != 0 {
		t.Fatalf("home rules: %+v %v", homeDoc, err)
	}
	info, err := os.Stat(filepath.Join(dir, "rules"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("rules directory: %v %v", info, err)
	}
	info, err = os.Stat(Path(dir, "work"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("rules file: %v %v", info, err)
	}
}

func TestGlobalRulesIgnoredAndInvalidProfileRejected(t *testing.T) {
	dir := t.TempDir()
	old := []byte("[malformed legacy YAML")
	legacy := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(legacy, old, 0600); err != nil {
		t.Fatal(err)
	}
	base := []byte("rules: ['MATCH,DIRECT']")
	data, err := Apply(base, dir, "default")
	if err != nil || string(data) != string(base) {
		t.Fatalf("global rules read: %q %v", data, err)
	}
	if Exists(dir, "default") {
		t.Fatal("legacy rules counted as profile rules")
	}
	doc, err := New(dir, "default", nil).List()
	if err != nil || doc.Entries == nil || len(doc.Entries) != 0 {
		t.Fatalf("missing rules: %+v %v", doc, err)
	}
	for _, name := range []string{"", "../escape", "/absolute", "a/b", ".", "a b"} {
		store := New(dir, name, nil)
		if _, err := store.List(); err == nil {
			t.Fatalf("List accepted %q", name)
		}
		if err := store.Save(context.Background(), Document{}); err == nil {
			t.Fatalf("Save accepted %q", name)
		}
		if Exists(dir, name) {
			t.Fatalf("Exists accepted %q", name)
		}
	}
	after, _ := os.ReadFile(legacy)
	if string(after) != string(old) {
		t.Fatal("legacy rules modified")
	}
}
