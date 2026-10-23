package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func validationStore(t *testing.T, binary string) (*profile.Store, string) {
	t.Helper()
	return validationStoreContext(t, context.Background(), binary)
}

func validationStoreContext(t *testing.T, ctx context.Context, binary string) (*profile.Store, string) {
	t.Helper()
	root := t.TempDir()
	paths := nagiruntime.Paths{
		ConfigDir: root, RuntimeDir: root,
		SocketPath: filepath.Join(root, "mihomo.sock"), PIDPath: filepath.Join(root, "mihomo.pid"),
		LockPath: filepath.Join(root, "mihomo.lock"), LogPath: filepath.Join(root, "mihomo.log"),
	}
	manager, err := engine.New(engine.Options{Binary: binary, ConfigPath: filepath.Join(root, "profiles", "default.yaml"), Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	return newProfileStoreWithContext(ctx, paths, binary, control.New(paths.SocketPath), manager), root
}

func TestProfileValidationStopsOnContextAndRetainsFiles(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "canceled"}[canceled], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			want := "validation timed out"
			if canceled {
				cancel()
				want = "validation canceled"
			}
			binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexec sleep 30\n")
			store, root := validationStoreContext(t, ctx, binary)
			old := []byte("rules: [MATCH,DIRECT]\n")
			if err := os.MkdirAll(filepath.Join(root, "profiles"), 0700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "profiles", "work.yaml")
			if err := os.WriteFile(source, old, 0600); err != nil {
				t.Fatal(err)
			}
			reloaded := false
			store.Reload = func(context.Context) error { reloaded = true; return nil }
			started := time.Now()
			err := store.ApplySubscription(ctx, "work", []byte("rules: ['MATCH,REJECT']\n"))
			if err == nil || !strings.Contains(err.Error(), want) || time.Since(started) > 3*time.Second {
				t.Fatalf("unbounded or unclear validation: %v (%s)", err, time.Since(started))
			}
			saved, readErr := os.ReadFile(source)
			current, currentErr := store.Current()
			if readErr != nil || string(saved) != string(old) || reloaded || currentErr != nil || current != "default" {
				t.Fatalf("failed validation changed state: %q %v %v %s %v", saved, readErr, reloaded, current, currentErr)
			}
			files, err := filepath.Glob(filepath.Join(root, "profiles", ".nagi-validate-*"))
			if err != nil || len(files) != 0 {
				t.Fatalf("temporary validation files remain: %v %v", files, err)
			}
		})
	}
}

func writeFakeMihomo(t *testing.T, directory, name, body string) string {
	t.Helper()
	binary := filepath.Join(directory, name)
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestProfileValidatorKeepsSourcePrivate(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho 'mihomo diagnostic SECRET' >&2\nexit 1\n")
	store, root := validationStore(t, binary)
	err := store.Write(context.Background(), "demo", []byte("proxy-password: SECRET\n"))
	if err == nil {
		t.Fatal("expected validation failure")
	}
	message := err.Error()
	for _, excluded := range []string{"SECRET", "mihomo diagnostic", ".nagi-validate-", root} {
		if strings.Contains(message, excluded) {
			t.Errorf("validation error exposed %q: %s", excluded, message)
		}
	}
	for _, want := range []string{"profile validation failed", "mihomo -t --check-json -f FILE -d DIRECTORY"} {
		if !strings.Contains(message, want) {
			t.Errorf("validation error missing %q: %s", want, message)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("validation left temporary files: %v", entries)
	}
}

func TestProfileValidatorUsesRedactedFieldPath(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho '{\"valid\":false,\"errors\":[{\"path\":\"proxy-providers.Work.url\",\"code\":\"invalid_url\",\"message\":\"invalid URL\"}]}'\nexit 1\n")
	store, _ := validationStore(t, binary)
	err := store.Write(context.Background(), "work", []byte("secret: PRIVATE\n"))
	if err == nil || !strings.Contains(err.Error(), "proxy-providers.Work.url") || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("field diagnostic: %v", err)
	}
}

func TestProfileValidatorReportsCandidateRuleFailure(t *testing.T) {
	candidate := []byte("rules:\n  - DOMAIN-SUFFIX,example.com,missing-target\n")
	operations := []struct {
		name string
		run  func(*profile.Store) error
	}{
		{"write", func(s *profile.Store) error { return s.Write(context.Background(), "work", candidate) }},
		{"apply", func(s *profile.Store) error { return s.Apply(context.Background(), "work", candidate) }},
		{"subscription", func(s *profile.Store) error { return s.ApplySubscription(context.Background(), "work", candidate) }},
		{"use", func(s *profile.Store) error { return s.Use(context.Background(), "work") }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho '{\"valid\":false,\"errors\":[{\"path\":\"rules[10446]\",\"code\":\"invalid_config\",\"message\":\"configuration validation failed\"}]}'\necho 'PRIVATE raw diagnostic' >&2\nexit 1\n")
			store, root := validationStore(t, binary)
			source := filepath.Join(root, "profiles", "work.yaml")
			if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(source, candidate, 0600); err != nil {
				t.Fatal(err)
			}
			err := operation.run(store)
			if err == nil {
				t.Fatal("expected validation failure")
			}
			for _, want := range []string{`profile "work" validation failed at rules[10446] (invalid_config): configuration validation failed`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("missing %q: %v", want, err)
				}
			}
			for _, excluded := range []string{`profile "default"`, "selected profile rejected", "PRIVATE", "missing-target", root} {
				if strings.Contains(err.Error(), excluded) {
					t.Errorf("unexpected %q: %v", excluded, err)
				}
			}
			current, currentErr := store.Current()
			if currentErr != nil || current != "default" {
				t.Fatalf("selection changed: %q, %v", current, currentErr)
			}
			saved, readErr := os.ReadFile(source)
			if readErr != nil || string(saved) != string(candidate) {
				t.Fatalf("source changed: %q, %v", saved, readErr)
			}
		})
	}
}

func TestProfileValidatorPreservesSpecificRuleDiagnostic(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho '{\"valid\":false,\"errors\":[{\"path\":\"rules[0]\",\"code\":\"unsupported_type\",\"message\":\"unsupported rule type\"}]}'\nexit 1\n")
	store, _ := validationStore(t, binary)
	err := store.Write(context.Background(), "work", []byte("rules: [UNKNOWN,example.com,DIRECT]\n"))
	if err == nil || !strings.Contains(err.Error(), "(unsupported_type): unsupported rule type") {
		t.Fatalf("specific rule diagnostic lost: %v", err)
	}
	if strings.Contains(err.Error(), "did not provide a specific cause") || strings.Contains(err.Error(), "selected profile rejected") {
		t.Fatalf("specific rule diagnostic replaced: %v", err)
	}
}

func TestProfileValidatorUsesOnlyNativeMissingProxyEvidence(t *testing.T) {
	for _, code := range []string{"rule_proxy_not_found", "invalid_config"} {
		binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho '{\"valid\":false,\"errors\":[{\"path\":\"rules[0]\",\"code\":\""+code+"\",\"message\":\"outbound does not exist\",\"target\":\"missing-target\"}]}'\nexit 1\n")
		store, _ := validationStore(t, binary)
		err := store.Write(context.Background(), "work", []byte("rules: [\"AND,((DOMAIN,example.com)),missing-target\"]\n"))
		var diagnostic *profile.ValidationError
		if !errors.As(err, &diagnostic) {
			t.Fatal(err)
		}
		want := ""
		if code == "rule_proxy_not_found" {
			want = "missing-target"
		}
		if diagnostic.MissingProxy != want {
			t.Fatalf("unexpected evidence: %+v", diagnostic)
		}
	}
}

func TestProfileValidatorShowsGeoIPFailureWithoutRuleSyntaxAdvice(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho '{\"valid\":false,\"errors\":[{\"path\":\"rules[10446]\",\"code\":\"geodata_download_failed\",\"message\":\"GeoIP database (MMDB) download failed: connection refused\"}]}'\necho 'PRIVATE raw diagnostic' >&2\nexit 1\n")
	store, _ := validationStore(t, binary)
	err := store.ApplySubscription(context.Background(), "Jimiao", []byte("rules: [\"GEOIP,CN,DIRECT\"]\n"))
	want := `profile "Jimiao" validation failed at rules[10446] (geodata_download_failed): GeoIP database (MMDB) download failed: connection refused`
	if err == nil || err.Error() != want {
		t.Fatalf("GeoIP diagnostic: %v; want %s", err, want)
	}
}

func TestProfileValidatorResolvesBinaryOnPATH(t *testing.T) {
	binDir := t.TempDir()
	writeFakeMihomo(t, binDir, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)
	store, root := validationStore(t, "mihomo")
	if err := store.Write(context.Background(), "demo", []byte("mixed-port: 17890\n")); err != nil {
		t.Fatalf("PATH executable validation failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "demo.yaml")); err != nil {
		t.Fatalf("validated profile was not saved: %v", err)
	}
}

func TestProfileValidatorMissingBinaryHint(t *testing.T) {
	store, root := validationStore(t, filepath.Join(t.TempDir(), "missing-mihomo"))
	err := store.Write(context.Background(), "demo", []byte("mixed-port: 17890\n"))
	if err == nil || !strings.Contains(err.Error(), "NAGI_MIHOMO_BIN") {
		t.Fatalf("missing executable guidance: %v", err)
	}
	if strings.Contains(err.Error(), ".nagi-validate-") {
		t.Fatalf("error mentions deleted temporary file: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "profiles", "demo.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("profile was unexpectedly saved: %v", statErr)
	}
}

func TestProfileValidatorDistinguishesLaunchFailure(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "not an executable format\n")
	store, root := validationStore(t, binary)
	err := store.Write(context.Background(), "demo", []byte("mixed-port: 17890\n"))
	if err == nil || !strings.Contains(err.Error(), "could not run mihomo validation") || !strings.Contains(err.Error(), "executable permissions") {
		t.Fatalf("launch failure guidance: %v", err)
	}
	if strings.Contains(err.Error(), ".nagi-validate-") || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "source YAML") {
		t.Fatalf("launch failure presented as config failure or exposed a temporary path: %v", err)
	}
}

// Structured diagnostics belong to the failed field, regardless of unrelated
// Tailscale configuration. Raw process output is never a substitute.
func TestTailscaleProfileValidationPreservesStructuredCause(t *testing.T) {
	for _, issue := range []struct{ path, code, message, target string }{
		{"rules[0]", "rule_proxy_not_found", "outbound does not exist: missing-target", "missing-target"},
		{"proxies[0]", "invalid_config", "configuration validation failed", ""},
	} {
		t.Run(issue.code, func(t *testing.T) {
			result, err := json.Marshal(map[string]any{"valid": false, "errors": []map[string]string{{"path": issue.path, "code": issue.code, "message": issue.message, "target": issue.target}}})
			if err != nil {
				t.Fatal(err)
			}
			binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nprintf '%s\\n' '"+string(result)+"'\necho 'PRIVATE_RAW_STDERR' >&2\nexit 1\n")
			store, root := validationStore(t, binary)
			err = store.Write(context.Background(), "work", []byte("proxies: [{name: Tail, type: tailscale, auth-key: FICTITIOUS_AUTH_KEY}]\nrules: ['MATCH,DIRECT']\n"))
			var diagnostic *profile.ValidationError
			if !errors.As(err, &diagnostic) {
				t.Fatalf("structured cause hidden: %v", err)
			}
			if diagnostic.Profile != "work" || diagnostic.Path != issue.path || diagnostic.Code != issue.code || diagnostic.Message != issue.message || diagnostic.MissingProxy != issue.target {
				t.Fatalf("lost diagnostic: %+v", diagnostic)
			}
			for _, excluded := range []string{"FICTITIOUS_AUTH_KEY", "PRIVATE_RAW_STDERR", root, "Tailscale diagnostics are private"} {
				if strings.Contains(err.Error(), excluded) {
					t.Fatalf("unexpected %q: %v", excluded, err)
				}
			}
			files, err := os.ReadDir(filepath.Join(root, "profiles"))
			if err != nil || len(files) != 0 {
				t.Fatalf("temporary validation file remains: %v %v", files, err)
			}
		})
	}
}

func TestTailscaleProfileValidationWithoutStructuredCause(t *testing.T) {
	for _, stdout := range []string{"PRIVATE_RAW_STDOUT", `{"valid":false,"errors":[]}`} {
		binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nprintf '%s\\n' '"+stdout+"'\necho 'PRIVATE_RAW_STDERR' >&2\nexit 1\n")
		store, _ := validationStore(t, binary)
		err := store.Write(context.Background(), "work", []byte("proxies: [{name: Tail, type: tailscale, auth-key: FICTITIOUS_AUTH_KEY}]\n"))
		if err == nil || !strings.Contains(err.Error(), "mihomo -t --check-json -f FILE -d DIRECTORY") {
			t.Fatalf("missing useful fallback: %v", err)
		}
		for _, excluded := range []string{"PRIVATE_RAW_STDOUT", "PRIVATE_RAW_STDERR", "FICTITIOUS_AUTH_KEY", "Tailscale diagnostics are private"} {
			if strings.Contains(err.Error(), excluded) {
				t.Fatalf("unexpected %q: %v", excluded, err)
			}
		}
	}
}
