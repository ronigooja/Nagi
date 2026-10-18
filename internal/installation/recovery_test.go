package installation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaintenanceRecoversBeforeReadingDamagedRecord(t *testing.T) {
	m, prefix := fixture(t)
	journal := filepath.Join(prefix, ".nagi-transaction")
	if err := os.Mkdir(journal, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(prefix, "share/nagi/install-state")
	if err := os.WriteFile(state, nil, 0644); err != nil {
		t.Fatal(err)
	}
	var events []string
	m.Run = func(_ context.Context, exe string, args ...string) ([]byte, error) {
		if exe == "bash" {
			if len(args) > 1 && args[1] == "recover" {
				events = append(events, "recover")
				if err := os.WriteFile(state, []byte("nagi-installer-v4\napp=\n"), 0644); err != nil {
					return nil, err
				}
				return nil, os.Remove(journal)
			}
			events = append(events, "installer")
			return nil, nil
		}
		events = append(events, strings.Join(args, " "))
		switch strings.Join(args, " ") {
		case "--json service status":
			return []byte(`{"ok":true,"data":{"installed":false}}`), nil
		case "--json status":
			return []byte(`{"ok":true,"data":{"running":false}}`), nil
		default:
			return []byte(`{"ok":true,"data":{}}`), nil
		}
	}
	result, err := m.Manage(context.Background(), "uninstall", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["completed"] != true || len(events) == 0 || events[0] != "recover" {
		t.Fatalf("maintenance did not recover before reading install-state: result=%v events=%v", result, events)
	}
}

func TestAppStatusDoesNotRecoverOrAlterPendingTransaction(t *testing.T) {
	m, prefix := fixture(t)
	m.OS = "darwin"
	journal := filepath.Join(prefix, ".nagi-transaction")
	if err := os.Mkdir(journal, 0700); err != nil {
		t.Fatal(err)
	}
	m.Run = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("read-only status invoked recovery")
		return nil, nil
	}
	if _, err := m.Manage(context.Background(), "app-status", ""); err == nil || !strings.Contains(err.Error(), "needs recovery") {
		t.Fatalf("expected pending transaction error: %v", err)
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatal("status changed pending journal:", err)
	}
}

func TestMaintenanceDoesNotRestoreServicesAfterRemovalOrUncertainCommit(t *testing.T) {
	for _, tc := range []struct {
		action, marker string
		restore        bool
	}{
		{"uninstall", "NAGI_INSTALL_FILES_COMMITTED:", false},
		{"uninstall", "NAGI_INSTALL_COMMIT_UNCERTAIN:", false},
		{"update", "NAGI_INSTALL_COMMIT_UNCERTAIN:", false},
		{"update", "NAGI_INSTALL_FILES_COMMITTED:", true},
	} {
		t.Run(tc.action+tc.marker, func(t *testing.T) {
			m, _ := fixture(t)
			m.ReleaseBase = releaseServer(t, standardArchive(t), false, nil).URL
			restored := false
			m.Run = func(_ context.Context, exe string, args ...string) ([]byte, error) {
				invocation := strings.Join(args, " ")
				if exe == "bash" {
					if strings.Contains(invocation, "--verify") {
						return nil, nil
					}
					return []byte(tc.marker + " injected cleanup/storage failure"), errors.New("installer failed")
				}
				switch invocation {
				case "--json service status":
					return []byte(`{"ok":true,"data":{"installed":true,"enabled":true}}`), nil
				case "--json status":
					return []byte(`{"ok":true,"data":{"running":false}}`), nil
				case "--json service install":
					restored = true
				}
				return []byte(`{"ok":true,"data":{}}`), nil
			}
			if _, err := m.Manage(context.Background(), tc.action, ""); err == nil {
				t.Fatal("partial completion was reported as success")
			}
			if restored != tc.restore {
				t.Fatalf("restored service=%t, want %t", restored, tc.restore)
			}
		})
	}
}
