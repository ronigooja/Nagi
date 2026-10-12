package command

import (
	"os"
	"path/filepath"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func TestTUNIntentRoundTrip(t *testing.T) {
	state := t.TempDir()
	paths := nagiruntime.Paths{StateDir: state}
	if got, err := readTUNIntent(paths); err != nil || got {
		t.Fatalf("missing intent = %v, %v; want false, nil", got, err)
	}
	if err := writeTUNIntent(paths, true); err != nil {
		t.Fatal(err)
	}
	if got, err := readTUNIntent(paths); err != nil || !got {
		t.Fatalf("enabled intent = %v, %v; want true, nil", got, err)
	}
	info, err := os.Stat(filepath.Join(state, "tun-intent"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("intent permissions = %o, want 600", info.Mode().Perm())
	}
	if err := writeTUNIntent(paths, false); err != nil {
		t.Fatal(err)
	}
	if got, err := readTUNIntent(paths); err != nil || got {
		t.Fatalf("disabled intent = %v, %v; want false, nil", got, err)
	}
}
