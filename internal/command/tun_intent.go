package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func tunIntentPath(paths nagiruntime.Paths) string {
	return filepath.Join(paths.StateDir, "tun-intent")
}

func readTUNIntent(paths nagiruntime.Paths) (bool, error) {
	data, err := os.ReadFile(tunIntentPath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(data)) {
	case "enabled":
		return true, nil
	case "disabled", "":
		return false, nil
	default:
		return false, errors.New("invalid saved TUN intent")
	}
}

func writeTUNIntent(paths nagiruntime.Paths, enabled bool) error {
	if err := os.MkdirAll(paths.StateDir, 0700); err != nil {
		return err
	}
	value := "disabled\n"
	if enabled {
		value = "enabled\n"
	}
	tmp, err := os.CreateTemp(paths.StateDir, ".tun-intent-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, tunIntentPath(paths))
}

func restoreSavedTUN(ctx context.Context, paths nagiruntime.Paths, client *control.Client) error {
	wanted, err := readTUNIntent(paths)
	if err != nil {
		return err
	}
	if !wanted {
		return nil
	}
	cfg, err := trafficConfig(ctx, client)
	if err != nil {
		return err
	}
	tun, _ := cfg["tun"].(map[string]any)
	payload := map[string]any{"enable": true, "auto-route": true, "auto-detect-interface": true, "strict-route": true, "dns-hijack": tunDNSHijack(tun["dns-hijack"])}
	if _, err := changeRuntimeTUN(ctx, paths, client, tun, payload); err != nil {
		return fmt.Errorf("saved TUN preference could not be restored: %w", err)
	}
	return nil
}
