package requestrewrite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type activation struct {
	applied, rollback     int
	applyErr, rollbackErr error
	version               string
}

func (a *activation) Apply(ctx context.Context, old, next *Snapshot) error {
	a.applied++
	a.version = next.Version()
	return a.applyErr
}
func (a *activation) Rollback(ctx context.Context, old *Snapshot) error {
	a.rollback++
	a.version = old.Version()
	return a.rollbackErr
}
func TestStoreIsolationMutationsAndRedaction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := Store{ConfigDir: dir, Profile: "work"}
	d := baseDoc()
	d.Rules = []Rule{rule("one", Operation{Op: "header_set", Name: "Authorization", Value: str("fictitious-secret")})}
	candidate := compiled(t, d)
	result, e := s.Save(ctx, candidate)
	if e != nil || !result.Saved || result.RuntimeConfirmed {
		t.Fatal(result, e)
	}
	other, e := (Store{ConfigDir: dir, Profile: "other"}).Load()
	if e != nil || len(other.doc.Rules) != 0 {
		t.Fatal("profile leak")
	}
	if _, e = s.AddRule(ctx, rule("two", Operation{Op: "header_delete", Name: "Cookie"})); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReorderRules(ctx, []string{"two", "one"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReorderRules(ctx, []string{"two", "two"}); e == nil {
		t.Fatal("bad reorder")
	}
	if _, e = s.SetRuleEnabled(ctx, "one", false); e != nil {
		t.Fatal(e)
	}
	snapshot, e := s.Load()
	if e != nil || snapshot.doc.Rules[0].ID != "two" || snapshot.doc.Rules[1].Enabled {
		t.Fatal(e)
	}
	summaries := snapshot.RuleSummaries()
	if strings.Contains(summaries[1].Operations[0], "secret") {
		t.Fatal("redaction")
	}
	copy := snapshot.Document()
	copy.Enabled = true
	if _, e = s.ImportSensitive(ctx, mustJSON(t, copy)); e == nil {
		t.Fatal("enabled import")
	}
	if _, e = s.Save(ctx, compiled(t, copy)); e == nil {
		t.Fatal("enabled save without coordinator")
	}
	for _, path := range []string{filepath.Dir(Path(dir, "work")), Path(dir, "work")} {
		info, e := os.Stat(path)
		if e != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("permissions", e)
		}
	}
}
func mustJSON(t *testing.T, d Document) []byte { t.Helper(); return compiled(t, d).canonical }
func TestStoreActivationRollbackAndEnabledImport(t *testing.T) {
	ctx := context.Background()
	s := Store{ConfigDir: t.TempDir(), Profile: "work"}
	old := compiled(t, baseDoc())
	if _, e := s.Save(ctx, old); e != nil {
		t.Fatal(e)
	}
	a := &activation{applyErr: errors.New("private upstream details")}
	s.Activation = a
	d := baseDoc()
	d.Enabled = true
	candidate := compiled(t, d)
	if _, e := s.Save(ctx, candidate); e == nil || e.Error() != "rewrite_activation_failed" || a.rollback != 1 {
		t.Fatal("activation failure", e)
	}
	got, _ := s.Load()
	if got.Version() != old.Version() || a.version != old.Version() {
		t.Fatal("rollback")
	}
	a.rollbackErr = errors.New("private error")
	if _, e := s.Save(ctx, candidate); e == nil || e.Error() != "rewrite_state_uncertain" {
		t.Fatal("uncertainty", e)
	}
	a.applyErr = nil
	a.rollbackErr = nil
	if _, e := s.Save(ctx, candidate); e != nil {
		t.Fatal(e)
	}
	portable := baseDoc()
	portable.Rules = append(portable.Rules, rule("more", Operation{Op: "header_delete", Name: "X-Example"}))
	if _, e := s.ImportSensitive(ctx, mustJSON(t, portable)); e != nil {
		t.Fatal(e)
	}
	got, _ = s.Load()
	if !got.doc.Enabled || len(got.doc.Rules) != 1 {
		t.Fatal("intent not retained independently")
	}
	if _, e := s.save(ctx, old, old.Version()); e == nil || e.Error() != "rewrite_conflict" {
		t.Fatal("stale edit")
	}
}
func TestPrivateStorageRejectsLinksAndPermissions(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"directory-symlink", "file-symlink", "hardlink", "public-file", "public-dir", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			s := Store{ConfigDir: dir, Profile: "work"}
			if _, e := s.Save(ctx, Empty()); e != nil {
				t.Fatal(e)
			}
			path := Path(dir, "work")
			switch kind {
			case "directory-symlink":
				if e := os.Rename(filepath.Dir(path), filepath.Join(dir, "elsewhere")); e != nil {
					t.Fatal(e)
				}
				os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Dir(path))
			case "file-symlink":
				os.Remove(path)
				os.Symlink(filepath.Join(dir, "target"), path)
			case "hardlink":
				os.Link(path, filepath.Join(dir, "other"))
			case "public-file":
				os.Chmod(path, 0644)
			case "public-dir":
				os.Chmod(filepath.Dir(path), 0755)
			case "fifo":
				os.Remove(path)
				makeFIFO(t, path)
			}
			if _, e := s.Load(); e == nil {
				t.Fatal("read unsafe path")
			}
			if _, e := s.Save(ctx, Empty()); e == nil {
				t.Fatal("save unsafe path")
			}
			if e := RemoveProfile(dir, "work"); e == nil {
				t.Fatal("remove unsafe path")
			}
		})
	}
}
func TestCAExplicitGenerationPublicExportAndValidation(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	c := CAStore{ConfigDir: t.TempDir()}
	status, e := c.Status(now)
	if e != nil || status.State != "missing" || status.Trust != "unknown" {
		t.Fatal(status, e)
	}
	status, e = c.Generate(now)
	if e != nil || status.State != "generated" || status.Trust != "unknown" || status.Fingerprint == "" {
		t.Fatal(status, e)
	}
	if _, e = c.Generate(now); e == nil {
		t.Fatal("replaced existing CA")
	}
	cert, key, e := c.LoadForChild(now)
	if e != nil || key == nil || !cert.IsCA {
		t.Fatal(e)
	}
	export, e := c.ExportPublic(now)
	if e != nil || strings.Contains(string(export), "PRIVATE KEY") || !strings.Contains(string(export), "CERTIFICATE") {
		t.Fatal("private export", e)
	}
	if _, e = c.Status(now.AddDate(6, 0, 0)); e == nil {
		t.Fatal("expired CA")
	}
	path := filepath.Join(c.ConfigDir, "request-rewrite", "ca", caBundle)
	os.Chmod(path, 0644)
	if status, e = c.Status(now); e == nil || status.State != "invalid" {
		t.Fatal("unsafe key permissions", status, e)
	}
}
