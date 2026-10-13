package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
)

func TestStoredRuleCLIProfileIsolation(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n"))
	profiles := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
	for _, name := range []string{"default", "jimiao"} {
		data := []byte("rules: ['MATCH,DIRECT']\n")
		if name == "jimiao" {
			data = []byte("proxy-groups: [{name: Subscription, type: select, proxies: [DIRECT]}]\nrules: ['MATCH,Subscription']\n")
		}
		if err := profiles.Write(context.Background(), name, data); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) []byte {
		t.Helper()
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "test"); code != 0 || errs.Len() != 0 {
			t.Fatalf("%q: exit=%d out=%s err=%s", args, code, out.String(), errs.String())
		}
		return out.Bytes()
	}
	entries := func() []rules.Entry {
		t.Helper()
		var response struct {
			Data struct {
				Entries []rules.Entry `json:"entries"`
			} `json:"data"`
		}
		if err := json.Unmarshal(run("rules", "custom"), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data.Entries
	}
	run("profile", "use", "jimiao")
	run("rules", "add", "personal", "DOMAIN", "jimiao.example", "Subscription")
	run("profile", "use", "default")
	if got := entries(); len(got) != 0 {
		t.Fatalf("default inherited jimiao rules: %+v", got)
	}
	run("rules", "add", "personal", "DOMAIN", "default.example", "DIRECT")
	run("rules", "disable", "personal")
	run("profile", "use", "jimiao")
	if got := entries(); len(got) != 1 || !got[0].Enabled || got[0].Payload != "jimiao.example" {
		t.Fatalf("jimiao entries: %+v", got)
	}
	effective, err := profiles.Effective("default")
	if err != nil || strings.Contains(string(effective), "Subscription") || strings.Contains(string(effective), "jimiao.example") {
		t.Fatalf("default effective profile contaminated: %s (%v)", effective, err)
	}
	run("rules", "remove", "personal")
	run("profile", "use", "default")
	if got := entries(); len(got) != 1 || got[0].Enabled || got[0].Payload != "default.example" {
		t.Fatalf("default entries changed by jimiao removal: %+v", got)
	}
}

func TestSubscriptionRuleTargetsUseTargetProfile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	source := []byte("rules: ['MATCH,DIRECT']\n")
	for _, name := range []string{"default", "jimiao"} {
		if err := profiles.Write(ctx, name, source); err != nil {
			t.Fatal(err)
		}
	}
	// default remains selected; candidate validation must use the requested profile.
	imported := rules.Entry{Name: "personal", Enabled: true, Kind: "rules", Items: []string{"DOMAIN,jimiao.example,Subscription"}}
	if err := rules.New(dir, "jimiao", nil).Add(ctx, imported); err != nil {
		t.Fatal(err)
	}
	if err := validateRuleTargets(dir, "default", source); err != nil {
		t.Fatalf("another profile's rule blocked default: %v", err)
	}
	if err := validateRuleTargets(dir, "jimiao", source); !errors.Is(err, errRuleConflict) {
		t.Fatalf("missing target in jimiao accepted: %v", err)
	}
	withGroup := []byte("proxy-groups: [{name: Subscription, type: select, proxies: [DIRECT]}]\nrules: ['MATCH,DIRECT']\n")
	if err := validateRuleTargets(dir, "jimiao", withGroup); err != nil {
		t.Fatalf("valid target profile rejected: %v", err)
	}
	if err := rules.New(dir, "jimiao", nil).SetEnabled(ctx, "personal", false); err != nil {
		t.Fatal(err)
	}
	if err := validateRuleTargets(dir, "jimiao", source); err != nil {
		t.Fatalf("disabled profile rule blocked apply: %v", err)
	}
}

func TestProfileRemovalDeletesOnlyItsRules(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	for _, name := range []string{"default", "jimiao"} {
		if err := profiles.Write(ctx, name, []byte("rules: ['MATCH,DIRECT']\n")); err != nil {
			t.Fatal(err)
		}
		if err := rules.New(dir, name, nil).Add(ctx, rules.Entry{Name: "personal", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: name + ".example", Target: "DIRECT"}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := rules.New(dir, "default", nil).List()
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.Remove("jimiao"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rules.Path(dir, "jimiao")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed profile's rules remain: %v", err)
	}
	after, err := rules.New(dir, "default", nil).List()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("remaining profile rules changed: %+v (%v)", after, err)
	}
	if err := profiles.Write(ctx, "jimiao", []byte("rules: ['MATCH,DIRECT']\n")); err != nil {
		t.Fatal(err)
	}
	recreated, err := rules.New(dir, "jimiao", nil).List()
	if err != nil || len(recreated.Entries) != 0 {
		t.Fatalf("recreated profile resurrected rules: %+v (%v)", recreated, err)
	}
}

func TestGlobalRulesFileIsIgnoredAndUnchanged(t *testing.T) {
	dir := t.TempDir()
	// Even an unreadable YAML document at the former global path cannot affect
	// listing, effective generation, or subscription target validation.
	old := []byte("entries: [invalid YAML\n")
	oldPath := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(oldPath, old, 0600); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	source := []byte("rules: ['MATCH,DIRECT']\n")
	if err := profiles.Write(context.Background(), "default", source); err != nil {
		t.Fatal(err)
	}
	doc, err := rules.New(dir, "default", nil).List()
	if err != nil || len(doc.Entries) != 0 {
		t.Fatalf("global rules read: %+v (%v)", doc, err)
	}
	if _, err := profiles.Effective("default"); err != nil {
		t.Fatalf("global file affected effective generation: %v", err)
	}
	if err := validateRuleTargets(dir, "default", source); err != nil {
		t.Fatalf("global file affected subscription validation: %v", err)
	}
	current, err := os.ReadFile(oldPath)
	if err != nil || !bytes.Equal(current, old) {
		t.Fatalf("global file migrated or modified: %q (%v)", current, err)
	}
	if _, err := os.Stat(rules.Path(dir, "default")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("global file migrated to profile rules: %v", err)
	}
}

func TestSubscriptionApplyIgnoresOtherProfileRulesAcrossEntryPoints(t *testing.T) {
	for _, entryPoint := range []string{"manual", "preserving", "scheduled"} {
		t.Run(entryPoint, func(t *testing.T) {
			ctx := context.Background()
			next := []byte("proxies: [{name: New, type: direct}]\nrules: ['MATCH,DIRECT']\n")
			paths, cache, _ := schedulerFixture(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(next) })
			if _, err := cache.Refresh(ctx, "work"); err != nil {
				t.Fatal(err)
			}
			profiles := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
			old := []byte("proxies: [{name: Old, type: direct}]\nrules: ['MATCH,DIRECT']\n")
			if err := profiles.Apply(ctx, "work", old); err != nil {
				t.Fatal(err)
			}
			foreign := rules.Entry{Name: "personal", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "default.example", Target: "MissingInWork"}
			if err := rules.New(paths.ConfigDir, "default", nil).Add(ctx, foreign); err != nil {
				t.Fatal(err)
			}
			local := rules.Entry{Name: "personal", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "work.example", Target: "DIRECT"}
			if err := rules.New(paths.ConfigDir, "work", nil).Add(ctx, local); err != nil {
				t.Fatal(err)
			}
			if err := cache.RecordApplySuccess("work", "work", "old-cache", old); err != nil {
				t.Fatal(err)
			}
			var err error
			switch entryPoint {
			case "manual":
				_, err = subscriptionCommand(ctx, []string{"subscription", "apply", "work"}, cache, profiles, nil)
			case "preserving":
				err = ApplyCachedSubscriptionPreservingSource(ctx, paths, cache, profiles, "work")
			case "scheduled":
				entry, findErr := cache.Find("work")
				if findErr != nil {
					t.Fatal(findErr)
				}
				err = applyScheduledCache(ctx, paths, cache, profiles, entry)
			}
			if err != nil {
				t.Fatalf("foreign rule blocked %s subscription application: %v", entryPoint, err)
			}
			effective, err := profiles.Effective("work")
			if err != nil || !strings.Contains(string(effective), "work.example") || strings.Contains(string(effective), "default.example") || !strings.Contains(string(effective), "name: New") {
				t.Fatalf("applied profile rules: %s (%v)", effective, err)
			}
			foreignDoc, err := rules.New(paths.ConfigDir, "default", nil).List()
			if err != nil || !reflect.DeepEqual(foreignDoc.Entries, []rules.Entry{foreign}) {
				t.Fatalf("foreign rules altered: %+v (%v)", foreignDoc, err)
			}
		})
	}
}
