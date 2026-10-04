// Package diagnostic inspects a Nagi installation without changing it.
package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/platform"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Report struct {
	Healthy bool    `json:"healthy"`
	Checks  []Check `json:"checks"`
}

func Run(ctx context.Context, paths nagiruntime.Paths, binary string) Report {
	report := Report{Healthy: true, Checks: make([]Check, 0, 8)}
	add := func(name, status, message string) {
		report.Checks = append(report.Checks, Check{Name: name, Status: status, Message: message})
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
			add(item.name, "warning", fmt.Sprintf("%s does not exist yet; run `nagi start` after configuring mihomo.", item.path))
		case err != nil:
			add(item.name, "error", fmt.Sprintf("Cannot inspect %s: %v; check directory permissions.", item.path, err))
		case !info.IsDir():
			add(item.name, "error", fmt.Sprintf("%s is not a directory; move the conflicting file and retry.", item.path))
		case info.Mode().Perm()&0500 != 0500:
			add(item.name, "error", fmt.Sprintf("%s lacks owner read or search permission; fix its permissions.", item.path))
		default:
			add(item.name, "ok", fmt.Sprintf("%s is available.", item.path))
		}
	}
	if resolved, err := exec.LookPath(binary); err != nil {
		add("mihomo_executable", "error", fmt.Sprintf("Mihomo executable %s is unavailable; install it or set NAGI_MIHOMO_BIN to a usable path or PATH command.", binary))
	} else {
		add("mihomo_executable", "ok", fmt.Sprintf("Mihomo executable is available at %s.", resolved))
	}

	store := profile.NewStore(paths.ConfigDir, nil, nil)
	name, err := store.Current()
	if err != nil {
		add("selected_profile", "error", "Selected profile setting is unreadable or invalid; inspect settings.yaml and select a valid profile with `nagi profile use NAME`.")
	} else if _, err = store.Show(name); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if name == "default" {
				add("selected_profile", "warning", "Default profile is missing; run `nagi start` to create it or import a profile.")
			} else {
				add("selected_profile", "error", fmt.Sprintf("Selected profile %s is missing; import it with `nagi profile import %s FILE` or select another with `nagi profile use NAME`.", name, name))
			}
		} else {
			add("selected_profile", "error", fmt.Sprintf("Selected profile %s cannot be read; check its file and permissions.", name))
		}
	} else {
		add("selected_profile", "ok", fmt.Sprintf("Selected profile %s is readable.", name))
	}

	pidData, err := os.ReadFile(paths.PIDPath)
	running := false
	switch {
	case errors.Is(err, os.ErrNotExist):
		add("process", "warning", "No Nagi PID file is present; mihomo appears stopped. Run `nagi start` when ready.")
	case err != nil:
		add("process", "error", "Cannot read the Nagi PID file; check runtime directory permissions.")
	default:
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if parseErr != nil || pid <= 0 {
			add("process", "error", "Nagi PID file is invalid; inspect runtime files and `nagi status`.")
		} else if !platform.Alive(pid) {
			add("process", "error", fmt.Sprintf("PID %d is stale; inspect `nagi logs` and `nagi status`.", pid))
		} else {
			running = true
			add("process", "ok", fmt.Sprintf("PID %d exists; process identity is not verified.", pid))
		}
	}
	if _, err := os.Stat(paths.SocketPath); err != nil && errors.Is(err, os.ErrNotExist) && !running {
		add("control_api", "warning", "No control socket is present; start mihomo to check API reachability.")
	} else {
		requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var version struct {
			Version string `json:"version"`
		}
		err := control.New(paths.SocketPath).Get(requestCtx, "/version", &version)
		cancel()
		if err != nil {
			if running {
				add("control_api", "error", "Mihomo PID exists but its Unix Socket API is unavailable; inspect `nagi logs` and `nagi status`.")
			} else {
				add("control_api", "warning", "Unix Socket API is unavailable; start mihomo to check it.")
			}
		} else {
			add("control_api", "ok", "Mihomo Unix Socket API responds; proxy connectivity was not checked.")
		}
	}
	return report
}
