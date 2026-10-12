package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

func TestTailscaleSnapshotUsesStableRootPrivateState(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root snapshot test")
	}
	private, err := os.MkdirTemp("/root", "nagi-ts-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	user := t.TempDir()
	userState := filepath.Join(user, "unsafe-state")
	if err := os.Symlink("/does/not/exist", userState); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{PrivateDir: private, SourceDir: user, SessionUID: os.Getuid()}
	raw := []byte("proxies: [{name: Tail, type: tailscale, exit-node: exit, state-dir: " + userState + "}]\n")
	for _, name := range []string{"work.yaml", ".effective-work.yaml", ".restore-work.yaml"} {
		snapshot, resources, err := h.prepareConfig(raw, filepath.Join(user, name))
		if err != nil {
			t.Fatal(err)
		}
		if len(resources) != 0 {
			t.Fatal("user state was copied")
		}
		data, err := os.ReadFile(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), userState) {
			t.Fatal("user state path retained")
		}
		var cfg map[string]any
		if yaml.Unmarshal(data, &cfg) != nil {
			t.Fatal("bad snapshot")
		}
		proxy := cfg["proxies"].([]any)[0].(map[string]any)
		want := nagiruntime.TailscaleStateName(private, "work", "Tail", "")
		if proxy["state-dir"] != want {
			t.Fatalf("unstable identity for %s: %v", name, proxy)
		}
		info, err := os.Stat(want)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("private state was not prepared")
		}
		removeSnapshots(snapshot, resources)
	}
	if _, err := os.Lstat(userState); err != nil {
		t.Fatal("user identity modified")
	}
}
func TestTailscaleSnapshotRejectsPrivateStateSymlinks(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root snapshot test")
	}
	private, err := os.MkdirTemp("/root", "nagi-ts-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	if err := os.Symlink(t.TempDir(), filepath.Join(private, ".tailscale-state")); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{PrivateDir: private}
	if _, _, err := h.prepareConfig([]byte("proxies: [{name: Tail, type: tailscale}]"), "work.yaml"); err == nil {
		t.Fatal("private state symlink accepted")
	}
}
