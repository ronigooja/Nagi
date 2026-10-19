package command

import (
	"context"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"

	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/platform"
	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/traffic"
)

var tunDNSOS = runtime.GOOS
var tunInterfaces = net.Interfaces
var startExitWatcher = spawnExitWatcher

// DNS ownership follows the active runtime, including temporary TUN changes.
func syncTUNDNS(ctx context.Context, paths nagiruntime.Paths, client *control.Client, pid int) (traffic.DNSState, error) {
	if tunDNSOS != "darwin" {
		return traffic.DNSState{}, nil
	}
	cfg, err := waitTUNAdapter(ctx, client)
	if err != nil {
		return traffic.DNSState{}, err
	}
	tun, _ := cfg["tun"].(map[string]any)
	mode, err := readBackend(paths)
	if err != nil {
		return traffic.DNSState{}, err
	}
	if mode == privilegedBackend {
		resp, err := (helperBackend{paths: paths}).call(ctx, privileged.Request{Operation: privileged.TUNDNSSync})
		if err != nil {
			return traffic.DNSState{}, err
		}
		var state traffic.DNSState
		err = json.Unmarshal(resp.Body, &state)
		return state, err
	}
	manager := traffic.NewDNS(paths.StateDir)
	if tun["enable"] != true {
		return manager.Disable(ctx)
	}
	if err := validateTUNDNS(paths, cfg, tun); err != nil {
		return traffic.DNSState{}, err
	}
	targets, err := traffic.DNSTargets(tun)
	if err != nil {
		return traffic.DNSState{}, err
	}
	if pid <= 0 {
		m, err := engine.New(engine.Options{Paths: paths, ConfigPath: filepath.Join(paths.ConfigDir, "profiles", "default.yaml")})
		if err != nil {
			return traffic.DNSState{}, err
		}
		status, err := m.Status(ctx)
		if err != nil || !status.Running {
			return traffic.DNSState{}, messageError("cli.error.tun_dns.cannot_identify_running_mihomo_for_dns_exit_recovery_inspect_nagi_st", "cannot identify running mihomo for DNS exit recovery; inspect `nagi status`")
		}
		pid = status.PID
	}
	state, err := manager.Sync(ctx, pid, targets)
	if err != nil {
		return state, messageError("cli.error.tun_dns.system_dns_takeover_failed_use_nagi_privileged_helper_install_and_na", "system DNS takeover failed; use `nagi privileged-helper install` and `nagi privileged-helper enable` for administrator DNS access: %w", err)
	}
	if err = startExitWatcher(paths, pid); err != nil {
		_, restoreErr := manager.Disable(context.Background())
		return state, errors.Join(messageError("cli.error.tun_dns.cannot_start_dns_exit_recovery_watcher_w", "cannot start DNS exit recovery watcher: %w", err), restoreErr)
	}
	return state, nil
}

func waitTUNAdapter(ctx context.Context, client *control.Client) (map[string]any, error) {
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		var cfg map[string]any
		if err := client.Get(ctx, "/configs", &cfg); err != nil {
			return nil, err
		}
		tun, _ := cfg["tun"].(map[string]any)
		if tun["enable"] != true || tunAdapterReport(tun, tunInterfaces)["adapter_status"] == "up" {
			return cfg, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, messageError("cli.error.tun_dns.tun_adapter_is_not_up_inspect_nagi_tun_status_and_nagi_logs_and_gran", "TUN adapter is not up; inspect `nagi tun status` and `nagi logs` and grant OS TUN permissions")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func restoreTUNDNS(ctx context.Context, paths nagiruntime.Paths) error {
	if tunDNSOS != "darwin" {
		return nil
	}
	mode, err := readBackend(paths)
	if err != nil {
		return err
	}
	if mode == privilegedBackend {
		_, err = (helperBackend{paths: paths}).call(ctx, privileged.Request{Operation: privileged.TUNDNSRestore})
		return err
	}
	_, err = traffic.NewDNS(paths.StateDir).Disable(ctx)
	if err == nil {
		_ = os.Remove(dnsRecoveryErrorPath(paths))
	}
	return err
}

func startWithTUNDNS(ctx context.Context, paths nagiruntime.Paths, manager lifecycleBackend, client *control.Client, configPath, profileName string) (any, error) {
	result, err := startAndVerify(ctx, manager, client, configPath, profileName)
	if err != nil && !errors.Is(err, engine.ErrAlreadyRunning) {
		return result, err
	}
	if err == nil {
		if confirmErr := confirmSavedTUN(ctx, paths, client); confirmErr != nil {
			recovery, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			_, stopErr := manager.Stop(recovery)
			return nil, fail("tun_error", errors.Join(confirmErr, stopErr))
		}
	}
	if tunDNSOS != "darwin" {
		return result, err
	}
	status, statusErr := manager.Status(ctx)
	if statusErr != nil {
		return nil, statusErr
	}
	if _, syncErr := synchronizeTUNDNS(ctx, paths, client, status.PID); syncErr != nil {
		recovery, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		restoreErr := restoreTUNDNS(recovery, paths)
		_, stopErr := manager.Stop(recovery)
		return nil, fail("tun_error", errors.Join(messageError("cli.error.tun_dns.tun_dns_startup_failed_mihomo_shutdown_attempted_w", "TUN DNS startup failed; mihomo shutdown attempted: %w", syncErr), restoreErr, stopErr))
	}
	return result, err
}

func spawnExitWatcher(paths nagiruntime.Paths, pid int) error {
	if err := paths.EnsureRuntime(); err != nil {
		return err
	}
	lock, err := os.OpenFile(exitWatcherLock(paths, pid), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := platform.TryLock(lock); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil
		}
		return err
	}
	_ = platform.Unlock(lock)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	watch := exec.Command(exe, "__traffic-watch", strconv.Itoa(pid))
	watch.Stdout, watch.Stderr = io.Discard, io.Discard
	watch.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := watch.Start(); err != nil {
		return err
	}
	return watch.Process.Release()
}
func exitWatcherLock(paths nagiruntime.Paths, pid int) string {
	return filepath.Join(paths.RuntimeDir, "traffic-watch-"+strconv.Itoa(pid)+".lock")
}
func dnsRecoveryErrorPath(paths nagiruntime.Paths) string {
	return filepath.Join(paths.StateDir, "tun-dns-recovery-error")
}

// A single watcher survives periods with no managed proxy/DNS journal, so later
// takeover operations for this process reuse it.
func runExitWatcher(ctx context.Context, paths nagiruntime.Paths, pid int) error {
	if err := paths.EnsureRuntime(); err != nil {
		return err
	}
	lock, err := os.OpenFile(exitWatcherLock(paths, pid), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := platform.TryLock(lock); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil
		}
		return err
	}
	defer platform.Unlock(lock)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for platform.Alive(pid) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proxyErr := traffic.New(paths.StateDir).RestoreIfStopped(recovery, pid)
	dnsErr := traffic.NewDNS(paths.StateDir).RestoreIfStopped(recovery, pid)
	if dnsErr != nil {
		_ = os.MkdirAll(paths.StateDir, 0700)
		_ = os.WriteFile(dnsRecoveryErrorPath(paths), []byte(dnsErr.Error()), 0600)
	}
	return errors.Join(proxyErr, dnsErr)
}

var synchronizeTUNDNS = syncTUNDNS

func changeRuntimeTUN(ctx context.Context, paths nagiruntime.Paths, client *control.Client, before, payload map[string]any) (map[string]any, error) {
	requested := payload["enable"] == true
	actionErr := client.Patch(ctx, "/configs", map[string]any{"tun": payload}, nil)
	var cfg map[string]any
	if actionErr == nil {
		actionErr = client.Get(ctx, "/configs", &cfg)
		tun, _ := cfg["tun"].(map[string]any)
		if actionErr == nil && (tun["enable"] == true) != requested {
			actionErr = messageError("cli.error.tun_dns.mihomo_did_not_report_the_requested_tun_state_inspect_nagi_logs_and_", "mihomo did not report the requested TUN state; inspect `nagi logs` and OS TUN permissions")
		}
		if actionErr == nil {
			_, actionErr = synchronizeTUNDNS(ctx, paths, client, 0)
		}
	}
	if actionErr == nil {
		return cfg, nil
	}
	recovery, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	rollbackErr := restoreRuntimeTUN(recovery, client, before)
	if rollbackErr == nil {
		_, rollbackErr = synchronizeTUNDNS(recovery, paths, client, 0)
	}
	if rollbackErr != nil {
		return nil, errors.Join(actionErr, messageError("cli.error.tun_dns.tun_dns_rollback_failed_inspect_nagi_tun_status_and_retry_nagi_tun_d", "TUN/DNS rollback failed; inspect `nagi tun status` and retry `nagi tun disable`: %w", rollbackErr))
	}
	return nil, messageError("cli.error.tun_dns.tun_change_failed_prior_tun_and_dns_state_restored_w", "TUN change failed; prior TUN and DNS state restored: %w", actionErr)
}

// Current engines expose DNS.enable. For older engines, prefer a recorded image
// with a matching activation marker; a newly activated profile uses the selected
// effective configuration until the transaction records its running image.
func validateTUNDNS(paths nagiruntime.Paths, cfg, tun map[string]any) error {
	if tun["auto-route"] != true {
		return messageError("cli.error.tun_dns.tun_dns_takeover_requires_auto_route_run_nagi_dns_tun_on", "TUN DNS takeover requires auto-route; run `nagi dns tun on`")
	}
	hijack := false
	switch values := tun["dns-hijack"].(type) {
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok && strings.HasSuffix(text, ":53") {
				hijack = true
			}
		}
	case []string:
		for _, value := range values {
			if strings.HasSuffix(value, ":53") {
				hijack = true
			}
		}
	}
	if !hijack {
		return messageError("cli.error.tun_dns.tun_dns_takeover_requires_port_53_interception_run_nagi_dns_tun_on", "TUN DNS takeover requires port 53 interception; run `nagi dns tun on`")
	}
	if dns, ok := cfg["dns"].(map[string]any); ok {
		if dns["enable"] != true {
			return messageError("cli.error.tun_dns.tun_dns_takeover_requires_mihomo_dns_enabled_run_nagi_dns_set_direct", "TUN DNS takeover requires mihomo DNS enabled; run `nagi dns set direct HTTPS_DOH_URL` before enabling TUN")
		}
		return nil
	}
	var data []byte
	var saved runningImage
	if encoded, err := os.ReadFile(runningImagePath(paths.ConfigDir)); err == nil && json.Unmarshal(encoded, &saved) == nil && saved.ActivationID != "" && saved.ActivationID == cfg["activation_id"] {
		data = saved.Effective
	}
	if len(data) == 0 {
		store := profile.NewStore(paths.ConfigDir, nil, nil)
		name, err := store.Current()
		if err != nil {
			return err
		}
		data, err = store.Effective(name)
		if err != nil {
			return err
		}
	}
	var parsed struct {
		DNS struct {
			Enable bool `yaml:"enable"`
		} `yaml:"dns"`
	}
	if yaml.Unmarshal(data, &parsed) != nil || !parsed.DNS.Enable {
		return messageError("cli.error.tun_dns.tun_dns_takeover_requires_mihomo_dns_enabled_run_nagi_dns_set_direct", "TUN DNS takeover requires mihomo DNS enabled; run `nagi dns set direct HTTPS_DOH_URL` before enabling TUN")
	}
	return nil
}

func statusTUNDNS(ctx context.Context, paths nagiruntime.Paths) (traffic.DNSState, error) {
	mode, err := readBackend(paths)
	if err != nil {
		return traffic.DNSState{}, err
	}
	if mode == privilegedBackend {
		resp, err := (helperBackend{paths: paths}).call(ctx, privileged.Request{Operation: privileged.TUNDNSStatus})
		if err != nil {
			return traffic.DNSState{}, err
		}
		var state traffic.DNSState
		err = json.Unmarshal(resp.Body, &state)
		return state, err
	}
	return traffic.NewDNS(paths.StateDir).Status(ctx)
}

func tunDNSHijack(existing any) []string {
	var result []string
	switch values := existing.(type) {
	case []string:
		result = append(result, values...)
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
	}
	for _, required := range []string{"any:53", "tcp://any:53"} {
		present := false
		for _, value := range result {
			if value == required {
				present = true
				break
			}
		}
		if !present {
			result = append(result, required)
		}
	}
	return result
}

func tunRuntimeSnapshot(tun map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"enable", "auto-route", "auto-detect-interface", "strict-route"} {
		result[key] = tun[key] == true
	}
	values := []string{}
	switch existing := tun["dns-hijack"].(type) {
	case []string:
		values = append(values, existing...)
	case []any:
		for _, value := range existing {
			if text, ok := value.(string); ok {
				values = append(values, text)
			}
		}
	}
	result["dns-hijack"] = values
	return result
}

func restoreRuntimeTUN(ctx context.Context, client *control.Client, before map[string]any) error {
	expected := tunRuntimeSnapshot(before)
	if err := client.Patch(ctx, "/configs", map[string]any{"tun": expected}, nil); err != nil {
		return err
	}
	var cfg map[string]any
	if err := client.Get(ctx, "/configs", &cfg); err != nil {
		return err
	}
	actual, _ := cfg["tun"].(map[string]any)
	if !reflect.DeepEqual(tunRuntimeSnapshot(actual), expected) {
		return messageError("cli.error.tun_dns.mihomo_did_not_confirm_the_prior_temporary_tun_settings", "mihomo did not confirm the prior temporary TUN settings")
	}
	return nil
}
