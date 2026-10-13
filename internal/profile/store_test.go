package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePreservesOldOnValidationFailureAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func(b []byte) error {
		if string(b) == "bad" {
			return errors.New("invalid")
		}
		return nil
	}, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "default", []byte("bad")); err == nil {
		t.Fatal("expected validation error")
	}
	if got, err := s.Show("default"); err != nil || string(got) != "first" {
		t.Fatalf("profile after failure = %q, %v", got, err)
	}
	if err := s.Write(ctx, "default", []byte("second")); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "profiles", "default.yaml.bak"))
	if err != nil || string(backup) != "first" {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	if err := s.Use(ctx, "../escape"); err == nil {
		t.Fatal("accepted traversal name")
	}
}

func TestReloadFailureRollsBackFiles(t *testing.T) {
	dir := t.TempDir()
	validate := func([]byte) error { return nil }
	s := NewStore(dir, validate, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "other", []byte("other")); err != nil {
		t.Fatal(err)
	}
	if err := s.Use(ctx, "default"); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.Write(ctx, "default", []byte("new")); err == nil {
		t.Fatal("expected reload error")
	}
	if got, err := s.Show("default"); err != nil || string(got) != "old" {
		t.Fatalf("profile after reload error = %q, %v", got, err)
	}
	if err := s.Use(ctx, "other"); err == nil {
		t.Fatal("expected reload error")
	}
	if selected, err := s.Current(); err != nil || selected != "default" {
		t.Fatalf("selected after reload error = %q, %v", selected, err)
	}
}

func TestApplySelectsAndReloadsOnce(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("old")); err != nil {
		t.Fatal(err)
	}
	reloads := 0
	s.Reload = func(context.Context) error {
		reloads++
		selected, err := s.Current()
		if err != nil || selected != "new" {
			t.Fatalf("selection during reload = %q, %v", selected, err)
		}
		data, err := s.Show("new")
		if err != nil || string(data) != "new config" {
			t.Fatalf("profile during reload = %q, %v", data, err)
		}
		return nil
	}
	if err := s.Apply(ctx, "new", []byte("new config")); err != nil {
		t.Fatal(err)
	}
	if reloads != 1 {
		t.Fatalf("reload count = %d, want 1", reloads)
	}
}

func TestApplyReloadFailureRestoresProfileAndSelection(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new profile", true: "existing profile"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			s := NewStore(dir, func([]byte) error { return nil }, nil)
			ctx := context.Background()
			if err := s.Write(ctx, "default", []byte("active")); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := s.Write(ctx, "target", []byte("previous")); err != nil {
					t.Fatal(err)
				}
			}
			s.Reload = func(context.Context) error { return errors.New("reload failed") }
			if err := s.Apply(ctx, "target", []byte("replacement")); err == nil {
				t.Fatal("expected reload failure")
			}
			if selected, err := s.Current(); err != nil || selected != "default" {
				t.Fatalf("selection after failure = %q, %v", selected, err)
			}
			data, err := s.Show("target")
			if existing {
				if err != nil || string(data) != "previous" {
					t.Fatalf("profile after failure = %q, %v", data, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("new profile remains after failure: %q, %v", data, err)
			}
		})
	}
}

func TestApplyCurrentProfileReloadFailureRestoresContent(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("previous")); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.Apply(ctx, "default", []byte("replacement")); err == nil {
		t.Fatal("expected reload failure")
	}
	if data, err := s.Show("default"); err != nil || string(data) != "previous" {
		t.Fatalf("active profile after failure = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("implicit default selection changed: %v", err)
	}
}

func TestWriteRepairsInvalidProfileWithoutReloadingUnselectedProfile(t *testing.T) {
	dir := t.TempDir()
	validate := func(data []byte) error {
		if string(data) == "invalid" {
			return errors.New("invalid YAML")
		}
		return nil
	}
	s := NewStore(dir, validate, nil)
	if err := s.Write(context.Background(), "other", []byte("first")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "profiles", "other.yaml")
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { t.Fatal("unselected profile was reloaded"); return nil }
	if err := s.Write(context.Background(), "other", []byte("repaired")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Show("other"); err != nil || string(got) != "repaired" {
		t.Fatalf("profile = %q, %v", got, err)
	}
	if got, err := os.ReadFile(path + ".bak"); err != nil || string(got) != "invalid" {
		t.Fatalf("backup = %q, %v", got, err)
	}
}

func TestWriteFailedReloadRestoresInvalidOldProfile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func(data []byte) error {
		if string(data) == "invalid" {
			return errors.New("invalid YAML")
		}
		return nil
	}, nil)
	path := filepath.Join(dir, "profiles", "default.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.Write(context.Background(), "default", []byte("repaired")); err == nil {
		t.Fatal("expected reload failure")
	}
	if got, err := s.Show("default"); err != nil || string(got) != "invalid" {
		t.Fatalf("profile after failure = %q, %v", got, err)
	}
}

func TestUseRepairsInvalidSelectionAndRestoresItOnReloadFailure(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "other", []byte("valid")); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte("profile: ../broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.Use(ctx, "other"); err == nil {
		t.Fatal("expected reload failure")
	}
	if got, err := os.ReadFile(settings); err != nil || string(got) != "profile: ../broken\n" {
		t.Fatalf("selection after failure = %q, %v", got, err)
	}
	s.Reload = nil
	if err := s.Use(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Current(); err != nil || got != "other" {
		t.Fatalf("selection = %q, %v", got, err)
	}
}

func TestApplyRepairsInvalidProfileAndSelection(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func(data []byte) error {
		if string(data) == "invalid" {
			return errors.New("invalid YAML")
		}
		return nil
	}, nil)
	ctx := context.Background()
	path := filepath.Join(dir, "profiles", "other.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte("profile: ../broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.Apply(ctx, "other", []byte("valid")); err == nil {
		t.Fatal("expected reload failure")
	}
	if got, err := s.Show("other"); err != nil || string(got) != "invalid" {
		t.Fatalf("profile after failure = %q, %v", got, err)
	}
	if got, err := os.ReadFile(settings); err != nil || string(got) != "profile: ../broken\n" {
		t.Fatalf("selection after failure = %q, %v", got, err)
	}
	s.Reload = nil
	if err := s.Apply(ctx, "other", []byte("valid")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Current(); err != nil || got != "other" {
		t.Fatalf("selection = %q, %v", got, err)
	}
}

func TestRemoveProfileCleansRelatedFilesAndPreservesOtherState(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("active")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "other", []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "other", []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	override, _ := s.overridePath("other")
	state, _ := s.defaultOverrideStatePath("other")
	if err := os.MkdirAll(filepath.Dir(override), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{override, override + ".bak", state, filepath.Join(dir, "profiles", ".effective-other.yaml")} {
		if err := os.WriteFile(path, []byte("old state"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Remove("other"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Show("other"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed profile remains: %v", err)
	}
	backup := filepath.Join(dir, "profiles", "other.yaml.bak")
	for _, path := range []string{backup, override, override + ".bak", state, filepath.Join(dir, "profiles", ".effective-other.yaml")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cleanup target remains: %s, %v", path, err)
		}
	}
	if err := s.Write(ctx, "other", []byte("new profile")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Override("other"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old override on recreation: %v", err)
	}
	if got, err := s.Show("default"); err != nil || string(got) != "active" {
		t.Fatalf("active profile = %q, %v", got, err)
	}
	if got, err := s.Current(); err != nil || got != "default" {
		t.Fatalf("selection = %q, %v", got, err)
	}
}

func TestRemoveRefusesActiveInvalidSelectionAndNonregularTargets(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("active")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "other", []byte("other")); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("default"); err == nil {
		t.Fatal("removed selected profile")
	}
	if got, err := s.Show("default"); err != nil || string(got) != "active" {
		t.Fatalf("selected profile changed: %q, %v", got, err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte("profile: ../broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("other"); !errors.Is(err, ErrInvalidSelectedProfile) {
		t.Fatalf("invalid selection error = %v", err)
	}
	if got, err := s.Show("other"); err != nil || string(got) != "other" {
		t.Fatalf("profile changed: %q, %v", got, err)
	}
	if err := s.Remove("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing profile error = %v", err)
	}
	if err := s.Remove("../bad"); err == nil {
		t.Fatal("accepted invalid name")
	}
	path := filepath.Join(dir, "profiles", "link.yaml")
	if err := os.Symlink(filepath.Join(dir, "profiles", "other.yaml"), path); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("link"); err == nil {
		t.Fatal("removed symlink profile")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("symlink removed: %v", err)
	}
}

func TestRemoveRefusesNonregularCleanupTargetBeforeDeleting(t *testing.T) {
	for _, suffix := range []string{".bak", "override", "state", "rules"} {
		t.Run(suffix, func(t *testing.T) {
			s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
			if err := s.Write(context.Background(), "other", []byte("config")); err != nil {
				t.Fatal(err)
			}
			path, _ := s.path("other")
			target := path + ".bak"
			if suffix != ".bak" {
				target, _ = s.overridePath("other")
				if suffix == "state" {
					target, _ = s.defaultOverrideStatePath("other")
				}
			}
			if suffix == "rules" {
				target = filepath.Join(s.Dir, "rules", "other.yaml")
			}
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := s.Remove("other"); err == nil {
				t.Fatal("removed nonregular cleanup target")
			}
			if got, err := s.Show("other"); err != nil || string(got) != "config" {
				t.Fatalf("profile changed: %q, %v", got, err)
			}
		})
	}
}

func TestRemoveCleansOnlyNamedProfileRules(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	for _, name := range []string{"default", "other"} {
		if err := s.Write(ctx, name, []byte("rules: ['MATCH,DIRECT']")); err != nil {
			t.Fatal(err)
		}
	}
	ruleDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(ruleDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default", "other"} {
		if err := os.WriteFile(filepath.Join(ruleDir, name+".yaml"), []byte("entries: []"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Remove("other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(ruleDir, "other.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed rules remain: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(ruleDir, "default.yaml")); err != nil {
		t.Fatalf("other rules removed: %v", err)
	}
	if err := s.Write(ctx, "other", []byte("rules: ['MATCH,DIRECT']")); err != nil {
		t.Fatal(err)
	}
	effective, err := s.Effective("other")
	if err != nil || string(effective) != "rules: ['MATCH,DIRECT']" {
		t.Fatalf("recreated profile: %q %v", effective, err)
	}
}

func TestEffectiveUsesOnlyNamedProfileRules(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func([]byte) error { return nil }, nil)
	ctx := context.Background()
	for _, name := range []string{"default", "work", "home"} {
		if err := s.Write(ctx, name, []byte("rules: ['MATCH,DIRECT']")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte("[invalid global YAML"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "work.yaml"), []byte("entries:\n  - name: work\n    enabled: true\n    kind: rule\n    type: DOMAIN\n    payload: work.example\n    target: DIRECT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default", "work", "home"} {
		data, err := s.Effective(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(data), "work.example") != (name == "work") {
			t.Fatalf("%s rules: %s", name, data)
		}
	}
}
