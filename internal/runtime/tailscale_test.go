package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailscaleStateStablePrivateAndSymlinkSafe(t *testing.T) {
	root := t.TempDir()
	a, err := TailscaleStatePath(root, "work", "Tail", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := TailscaleStatePath(root, "work", "Tail", TailscaleControlURL+"/")
	if err != nil || a != b {
		t.Fatalf("unstable default path: %s %s %v", a, b, err)
	}
	for _, path := range []string{filepath.Join(root, ".tailscale-state"), a} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("unsafe permissions: %v %v", info, err)
		}
	}
	if a == TailscaleStateName(root, "other", "Tail", "") || a == TailscaleStateName(root, "work", "Other", "") || a == TailscaleStateName(root, "work", "Tail", "https://control.example") {
		t.Fatal("identity namespace collision")
	}
	bad := t.TempDir()
	if err := os.Symlink(root, filepath.Join(bad, ".tailscale-state")); err != nil {
		t.Fatal(err)
	}
	if _, err := TailscaleStatePath(bad, "work", "Tail", ""); err == nil {
		t.Fatal("followed state symlink")
	}
	parent := t.TempDir()
	link := filepath.Join(parent, "root")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := TailscaleStatePath(link, "work", "Tail", ""); err == nil {
		t.Fatal("followed ancestor symlink")
	}
}
func TestTailscaleEnvGrantsManagedSubtree(t *testing.T) {
	result := TailscaleEnv([]string{"PATH=/usr/bin", "SAFE_PATHS=/explicit", "OTHER=value"}, "/config")
	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "SAFE_PATHS=/explicit"+string(filepath.ListSeparator)+"/config/.tailscale-state") || strings.Contains(joined, "SKIP_SAFE_PATH_CHECK") || !strings.Contains(joined, "OTHER=value") {
		t.Fatal(result)
	}
}
