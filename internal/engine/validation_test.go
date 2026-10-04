package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func TestValidateSanitizesFailureAndSuggestsNextStep(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "mihomo")
	cfg := filepath.Join(d, "config.yaml")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho SECRET >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Binary: bin, ConfigPath: cfg, Paths: nagiruntime.Paths{RuntimeDir: d}})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Validate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nagi config show") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unexpected validation error: %v", err)
	}
	var exitErr *os.PathError
	if errors.As(err, &exitErr) {
		t.Fatal("expected validation failure, not path error")
	}
}

func TestValidateMissingBinaryHint(t *testing.T) {
	d := t.TempDir()
	cfg := filepath.Join(d, "config.yaml")
	_ = os.WriteFile(cfg, []byte("x"), 0600)
	m, err := New(Options{Binary: "definitely-missing-mihomo", ConfigPath: cfg, Paths: nagiruntime.Paths{RuntimeDir: d}})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Validate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NAGI_MIHOMO_BIN") {
		t.Fatalf("missing hint: %v", err)
	}
}
