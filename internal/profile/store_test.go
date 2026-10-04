package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
