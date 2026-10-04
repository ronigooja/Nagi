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
