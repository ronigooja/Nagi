package profile

import (
	"context"
	"github.com/ronigooja/Nagi/internal/requestrewrite"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestRewritePreservedAcrossSubscriptionsRemovedWithProfile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	profiles := NewStore(dir, func([]byte) error { return nil }, nil)
	if e := profiles.Apply(ctx, "work", []byte("mode: rule\nrules: [MATCH,DIRECT]\n")); e != nil {
		t.Fatal(e)
	}
	rewrites := requestrewrite.Store{ConfigDir: dir, Profile: "work"}
	candidate, e := requestrewrite.Compile(requestrewrite.Document{Version: 2, Scripts: []requestrewrite.Script{}, Rules: []requestrewrite.Rule{}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = rewrites.Save(ctx, candidate); e != nil {
		t.Fatal(e)
	}
	ca := requestrewrite.CAStore{ConfigDir: dir}
	if _, e = ca.Generate(time.Now()); e != nil {
		t.Fatal(e)
	}
	for _, source := range []string{"mode: direct\nrules: [MATCH,DIRECT]\nrequest-rewrite: {enabled: true}\n", "mode: global\nrules: [MATCH,DIRECT]\n"} {
		if e = profiles.ApplySubscription(ctx, "work", []byte(source)); e != nil {
			t.Fatal(e)
		}
		got, e := rewrites.Load()
		if e != nil || got.Version() != candidate.Version() || got.Document().Enabled {
			t.Fatal("subscription changed local rules/intent", e)
		}
	}
	if e = profiles.Apply(ctx, "other", []byte("mode: direct\n")); e != nil {
		t.Fatal(e)
	}
	if e = profiles.Remove("work"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(requestrewrite.Path(dir, "work")); !os.IsNotExist(e) {
		t.Fatal("document not deleted", e)
	}
	if _, e = ca.Status(time.Now()); e != nil {
		t.Fatal("CA removed", e)
	}
	if e = profiles.Write(ctx, "work", []byte("mode: rule\n")); e != nil {
		t.Fatal(e)
	}
	got, e := rewrites.Load()
	if e != nil || got.Document().Enabled || len(got.Document().Rules) != 0 {
		t.Fatal("profile recreated with stale rules", e)
	}
	if _, e = os.Stat(filepath.Join(dir, "request-rewrite", "ca")); e != nil {
		t.Fatal(e)
	}
}
