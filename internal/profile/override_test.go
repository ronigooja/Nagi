package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverrideStaysSeparateAndSurvivesProfileReplacement(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func(data []byte) error {
		if len(data) == 0 {
			return errors.New("empty")
		}
		return nil
	}, nil)
	if err := s.Write(context.Background(), "work", []byte("mixed-port: 17890\ndns:\n  enable: false\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOverride(context.Background(), "work", []byte("mixed-port: 17891\ndns:\n  enable: true\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(context.Background(), "work", []byte("mixed-port: 17900\ndns:\n  enable: false\n")); err != nil {
		t.Fatal(err)
	}
	base, err := s.Show("work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(base), "17891") {
		t.Fatalf("source modified by override: %s", base)
	}
	effective, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(effective), "17891") || !strings.Contains(string(effective), "enable: true") {
		t.Fatalf("override not applied: %s", effective)
	}
	if err := s.ClearOverride(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	effective, err = s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	if string(effective) != string(base) {
		t.Fatalf("clear did not restore source: %s", effective)
	}
}

func TestRestoreBackupValidationAndReloadRollback(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, func(data []byte) error {
		if string(data) == "bad" {
			return errors.New("bad")
		}
		return nil
	}, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "default", []byte("second")); err != nil {
		t.Fatal(err)
	}
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.RestoreBackup(ctx, "default"); err == nil {
		t.Fatal("expected reload failure")
	}
	current, _ := s.Show("default")
	backup, _ := s.Backup("default")
	if string(current) != "second" || string(backup) != "first" {
		t.Fatalf("rollback current=%q backup=%q", current, backup)
	}
	s.Reload = nil
	if err := s.RestoreBackup(ctx, "default"); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Show("default")
	backup, _ = s.Backup("default")
	if string(current) != "first" || string(backup) != "second" {
		t.Fatalf("swap current=%q backup=%q", current, backup)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "default.yaml.bak"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreBackup(ctx, "default"); err == nil {
		t.Fatal("invalid backup accepted")
	}
}
