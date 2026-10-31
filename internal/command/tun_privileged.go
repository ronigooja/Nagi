package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ronigooja/Nagi/internal/engine"
	privilegedservice "github.com/ronigooja/Nagi/internal/privileged/service"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// tunPrivileged requests the complete, explicit macOS transition. It is kept
// separate from ordinary `tun enable`: changing the engine owner stops the
// user engine and requires administrator authorization when the helper is not
// installed yet.
func preparePrivilegedTUN(ctx context.Context, paths nagiruntime.Paths, binary string) (map[string]any, error) {
	if runtime.GOOS != "darwin" {
		return nil, messageError("cli.error.tun_privileged.macOS_only", "privileged TUN setup is supported only on macOS")
	}
	serviceStatus, err := privilegedservice.Status()
	if err != nil {
		return nil, fail("privileged_helper_error", err)
	}
	installed := false
	if !serviceStatus.Installed || !serviceStatus.Active {
		if err := installOrActivatePrivilegedTUNHelper(ctx, serviceStatus.Installed); err != nil {
			return nil, fail("privileged_helper_error", err)
		}
		serviceStatus, err = privilegedservice.Status()
		if err != nil {
			return nil, fail("privileged_helper_error", err)
		}
		installed = true
	}
	if !serviceStatus.Installed || !serviceStatus.Active {
		return nil, fail("privileged_helper_error", messageError("cli.error.tun_privileged.helper_not_active", "privileged helper is not active; run `nagi privileged-helper status`"))
	}

	helper := helperBackend{paths: paths}
	if _, err := helper.Status(ctx); err != nil {
		return nil, fail("privileged_helper_error", messageError("cli.error.tun_privileged.helper_socket_unavailable", "privileged helper is active but its authenticated socket is unavailable: %w", err))
	}
	mode, err := readBackend(paths)
	if err != nil {
		return nil, err
	}
	if mode == userBackend {
		userEngine, err := engine.New(engine.Options{Binary: binary, ConfigPath: filepathDefaultProfile(paths), Paths: paths})
		if err != nil {
			return nil, err
		}
		status, err := userEngine.Status(ctx)
		if err != nil {
			return nil, err
		}
		if status.Running {
			if err := restoreTUNDNS(ctx, paths); err != nil {
				return nil, fail("tun_error", messageError("cli.error.tun_privileged.user_dns_restore_failed", "cannot switch TUN backend because user-mode DNS restoration failed: %w", err))
			}
			if _, err := userEngine.Stop(ctx); err != nil && !errors.Is(err, engine.ErrNotRunning) {
				return nil, fail("privileged_helper_error", messageError("cli.error.tun_privileged.stop_user_engine_failed", "cannot switch TUN backend because user mihomo could not be stopped: %w", err))
			}
		}
		if err := writeBackend(paths, privilegedBackend); err != nil {
			return nil, err
		}
	}
	return map[string]any{"selected_backend": privilegedBackend, "helper_installed": installed, "helper_active": true, "changed": mode != privilegedBackend}, nil
}

func filepathDefaultProfile(paths nagiruntime.Paths) string {
	return filepath.Join(paths.ConfigDir, "profiles", "default.yaml")
}

func installOrActivatePrivilegedTUNHelper(ctx context.Context, installed bool) error {
	action := "install"
	if installed {
		action = "activate"
	}
	cmd := exec.CommandContext(ctx, "sudo", "/usr/local/bin/nagi", "privileged-helper", action)
	cmd.Stdin = os.Stdin
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) != 0 {
			return fmt.Errorf("install privileged helper: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("install privileged helper: %w", err)
	}
	return nil
}
