// Package diagnostic inspects a Nagi installation without changing it.
package diagnostic

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/platform"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type Check struct {
	Name     string                     `json:"name"`
	Status   string                     `json:"status"`
	Message  string                     `json:"message"`
	Messages map[string]i18n.Descriptor `json:"-"`
}

type Report struct {
	Healthy bool    `json:"healthy"`
	Checks  []Check `json:"checks"`
}

func Run(ctx context.Context, paths nagiruntime.Paths, binary string) Report {
	return RunWithAccess(ctx, paths, binary, control.New(paths.SocketPath), nil)
}

// RunWithAccess checks the selected backend without assuming the user owns
// mihomo's PID marker or controller socket.
func RunWithAccess(ctx context.Context, paths nagiruntime.Paths, binary string, api *control.Client, selected *engine.Status) Report {
	report := Report{Healthy: true, Checks: make([]Check, 0, 8)}
	add := func(name, status string, message *i18n.Message) {
		report.Checks = append(report.Checks, newCheck(name, status, message))
		if status == "error" {
			report.Healthy = false
		}
	}
	for _, item := range []struct{ name, path string }{
		{"config_directory", paths.ConfigDir}, {"data_directory", paths.DataDir}, {"state_directory", paths.StateDir}, {"runtime_directory", paths.RuntimeDir},
	} {
		info, err := os.Stat(item.path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			add(item.name, "warning", i18n.Errorf("backend.diagnostic.s_does_not_exist_yet_run_nagi_start_after_configuring_mihomo", "%s does not exist yet; run `nagi start` after configuring mihomo.", item.path))
		case err != nil:
			add(item.name, "error", i18n.Errorf("backend.diagnostic.cannot_inspect_s_v_check_directory_permissions", "Cannot inspect %s: %v; check directory permissions.", item.path, err))
		case !info.IsDir():
			add(item.name, "error", i18n.Errorf("backend.diagnostic.s_is_not_a_directory_move_the_conflicting_file_and_retry", "%s is not a directory; move the conflicting file and retry.", item.path))
		case info.Mode().Perm()&0500 != 0500:
			add(item.name, "error", i18n.Errorf("backend.diagnostic.s_lacks_owner_read_or_search_permission_fix_its_permissions", "%s lacks owner read or search permission; fix its permissions.", item.path))
		default:
			add(item.name, "ok", i18n.Errorf("backend.diagnostic.s_is_available", "%s is available.", item.path))
		}
	}
	if resolved, err := exec.LookPath(binary); err != nil {
		add("mihomo_executable", "error", i18n.Errorf("backend.diagnostic.mihomo_executable_s_is_unavailable_install_it_or_set_nagi_mihomo_bin_to_a_u", "Mihomo executable %s is unavailable; install it or set NAGI_MIHOMO_BIN to a usable path or PATH command.", binary))
	} else {
		add("mihomo_executable", "ok", i18n.Errorf("backend.diagnostic.mihomo_executable_is_available_at_s", "Mihomo executable is available at %s.", resolved))
	}

	store := profile.NewStore(paths.ConfigDir, nil, nil)
	name, err := store.Current()
	if err != nil {
		add("selected_profile", "error", i18n.Errorf("backend.diagnostic.selected_profile_setting_is_unreadable_or_invalid_inspect_settings_yaml_and", "Selected profile setting is unreadable or invalid; inspect settings.yaml and select a valid profile with `nagi profile use NAME`."))
	} else if _, err = store.Show(name); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if name == "default" {
				add("selected_profile", "warning", i18n.Errorf("backend.diagnostic.default_profile_is_missing_run_nagi_start_to_create_it_or_import_a_profile", "Default profile is missing; run `nagi start` to create it or import a profile."))
			} else {
				add("selected_profile", "error", i18n.Errorf("backend.diagnostic.selected_profile_s_is_missing_import_it_with_nagi_profile_import_s_file_or_", "Selected profile %s is missing; import it with `nagi profile import %s FILE` or select another with `nagi profile use NAME`.", name, name))
			}
		} else {
			add("selected_profile", "error", i18n.Errorf("backend.diagnostic.selected_profile_s_cannot_be_read_check_its_file_and_permissions", "Selected profile %s cannot be read; check its file and permissions.", name))
		}
	} else {
		add("selected_profile", "ok", i18n.Errorf("backend.diagnostic.selected_profile_s_is_readable", "Selected profile %s is readable.", name))
	}

	running := false
	if selected != nil {
		running = selected.Running
		if running {
			add("process", "ok", i18n.Errorf("backend.diagnostic.privileged_mihomo_pid_d_is_reported_by_the_helper", "Privileged mihomo PID %d is reported by the helper.", selected.PID))
		} else {
			add("process", "warning", i18n.Errorf("backend.diagnostic.privileged_mihomo_is_stopped_run_nagi_start_when_ready", "Privileged mihomo is stopped; run `nagi start` when ready."))
		}
	} else {
		pidData, err := os.ReadFile(paths.PIDPath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			add("process", "warning", i18n.Errorf("backend.diagnostic.no_nagi_pid_file_is_present_mihomo_appears_stopped_run_nagi_start_when_read", "No Nagi PID file is present; mihomo appears stopped. Run `nagi start` when ready."))
		case err != nil:
			add("process", "error", i18n.Errorf("backend.diagnostic.cannot_read_the_nagi_pid_file_check_runtime_directory_permissions", "Cannot read the Nagi PID file; check runtime directory permissions."))
		default:
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
			if parseErr != nil || pid <= 0 {
				add("process", "error", i18n.Errorf("backend.diagnostic.nagi_pid_file_is_invalid_inspect_runtime_files_and_nagi_status", "Nagi PID file is invalid; inspect runtime files and `nagi status`."))
			} else if !platform.Alive(pid) {
				add("process", "error", i18n.Errorf("backend.diagnostic.pid_d_is_stale_inspect_nagi_logs_and_nagi_status", "PID %d is stale; inspect `nagi logs` and `nagi status`.", pid))
			} else {
				running = true
				add("process", "ok", i18n.Errorf("backend.diagnostic.pid_d_exists_process_identity_is_not_verified", "PID %d exists; process identity is not verified.", pid))
			}
		}
	}
	if selected == nil && !running && !socketExists(paths.SocketPath) {
		add("control_api", "warning", i18n.Errorf("backend.diagnostic.no_control_socket_is_present_start_mihomo_to_check_api_reachability", "No control socket is present; start mihomo to check API reachability."))
	} else {
		requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var version struct {
			Version string `json:"version"`
		}
		err := api.Get(requestCtx, "/version", &version)
		cancel()
		if err != nil {
			if running {
				add("control_api", "error", i18n.Errorf("backend.diagnostic.mihomo_pid_exists_but_its_unix_socket_api_is_unavailable_inspect_nagi_logs_", "Mihomo PID exists but its Unix Socket API is unavailable; inspect `nagi logs` and `nagi status`."))
			} else {
				add("control_api", "warning", i18n.Errorf("backend.diagnostic.unix_socket_api_is_unavailable_start_mihomo_to_check_it", "Unix Socket API is unavailable; start mihomo to check it."))
			}
		} else {
			add("control_api", "ok", i18n.Errorf("backend.diagnostic.mihomo_unix_socket_api_responds_proxy_connectivity_was_not_checked", "Mihomo Unix Socket API responds; proxy connectivity was not checked."))
		}
	}
	return report
}

func socketExists(path string) bool { _, err := os.Stat(path); return err == nil }
