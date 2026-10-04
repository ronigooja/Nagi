package diagnostic

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func paths(root string) nagiruntime.Paths {
	return nagiruntime.Paths{
		ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"),
		StateDir: filepath.Join(root, "state"), RuntimeDir: filepath.Join(root, "run"),
		PIDPath: filepath.Join(root, "run", "mihomo.pid"), SocketPath: filepath.Join(root, "run", "mihomo.sock"),
	}
}

func check(report Report, name string) Check {
	for _, item := range report.Checks {
		if item.Name == name {
			return item
		}
	}
	return Check{}
}

func TestFreshDoctorDoesNotCreateDirectories(t *testing.T) {
	root := t.TempDir()
	p := paths(root)
	report := Run(context.Background(), p, filepath.Join(root, "missing-mihomo"))
	if report.Healthy {
		t.Fatal(report)
	}
	if got := check(report, "process").Status; got != "warning" {
		t.Fatal(got)
	}
	if got := check(report, "mihomo_executable").Status; got != "error" {
		t.Fatal(got)
	}
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.StateDir, p.RuntimeDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("doctor created %s: %v", dir, err)
		}
	}
}

func TestMihomoCommandFoundOnPATH(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "mihomo"), []byte("stub"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	item := check(Run(context.Background(), paths(root), "mihomo"), "mihomo_executable")
	if item.Status != "ok" || !strings.Contains(item.Message, filepath.Join(binDir, "mihomo")) {
		t.Fatal(item)
	}
}

func TestMissingSelectedProfileRequiresRepair(t *testing.T) {
	root := t.TempDir()
	p := paths(root)
	if err := os.MkdirAll(p.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), p, filepath.Join(root, "missing"))
	item := check(report, "selected_profile")
	if item.Status != "error" || !strings.Contains(item.Message, "nagi profile import work FILE") || strings.Contains(item.Message, "nagi start") {
		t.Fatal(item)
	}
	if report.Healthy {
		t.Fatal(report)
	}
}

func TestCorruptSelectionAndStalePID(t *testing.T) {
	root := t.TempDir()
	p := paths(root)
	if err := os.MkdirAll(p.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.RuntimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: bad/name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.PIDPath, []byte("999999999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), p, filepath.Join(root, "missing"))
	if got := check(report, "selected_profile").Status; got != "error" {
		t.Fatal(got)
	}
	if got := check(report, "process").Status; got != "error" {
		t.Fatal(got)
	}
	if strings.Contains(check(report, "selected_profile").Message, "bad/name") {
		t.Fatal("selection leaked")
	}
}

func TestReachableUnixAPI(t *testing.T) {
	root := t.TempDir()
	p := paths(root)
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.StateDir, p.RuntimeDir, filepath.Join(p.ConfigDir, "profiles")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(root, "mihomo")
	if err := os.WriteFile(binary, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "profiles", "default.yaml"), []byte("mode: rule\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.PIDPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", p.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"test"}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	report := Run(context.Background(), p, binary)
	if !report.Healthy {
		t.Fatal(report)
	}
	if got := check(report, "control_api").Status; got != "ok" {
		t.Fatal(got)
	}
	if !strings.Contains(check(report, "process").Message, "identity is not verified") {
		t.Fatal(report)
	}
}
