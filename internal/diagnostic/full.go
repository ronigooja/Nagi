package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// FullReport is a redacted, read-only health report. It never includes profile
// contents, subscription URLs, controller secrets, or command output.
type FullReport struct {
	Healthy  bool    `json:"healthy"`
	Checks   []Check `json:"checks"`
	Platform string  `json:"platform"`
}

func RunFull(ctx context.Context, paths nagiruntime.Paths, binary string) FullReport {
	return RunFullWithAccess(ctx, paths, binary, control.New(paths.SocketPath), nil)
}

func RunFullWithAccess(ctx context.Context, paths nagiruntime.Paths, binary string, api *control.Client, selected *engine.Status) FullReport {
	r := FullReport{Healthy: true, Checks: []Check{}, Platform: runtime.GOOS}
	add := func(n, s string, m *i18n.Message) {
		r.Checks = append(r.Checks, newCheck(n, s, m))
		if s == "error" {
			r.Healthy = false
		}
	}
	base := RunWithAccess(ctx, paths, binary, api, selected)
	for _, c := range base.Checks {
		r.Checks = append(r.Checks, c)
		if c.Status == "error" {
			r.Healthy = false
		}
	}
	checkPerm := func(name, path string, mode os.FileMode) {
		i, e := os.Stat(path)
		if errors.Is(e, os.ErrNotExist) {
			return
		}
		if e != nil {
			add(name, "error", i18n.Errorf("backend.diagnostic.cannot_inspect_permissions_check_owner_access", "Cannot inspect permissions; check owner access."))
			return
		}
		if !i.Mode().IsRegular() && name != "socket_permissions" {
			return
		}
		if mode&0077 != 0 {
			add(name, "warning", i18n.Errorf("backend.diagnostic.s_is_accessible_to_group_or_other_users_restrict_it_to_owner_permissions", "%s is accessible to group or other users; restrict it to owner permissions.", path))
		} else {
			add(name, "ok", i18n.Errorf("backend.diagnostic.owner_only_permissions_are_set", "Owner-only permissions are set."))
		}
	}
	checkPerm("config_permissions", paths.ConfigDir, os.FileMode(0077))
	checkPerm("runtime_permissions", paths.RuntimeDir, os.FileMode(0077))
	socketPath := paths.SocketPath
	if selected != nil {
		socketPath = selected.SocketPath
	}
	if i, e := os.Stat(socketPath); e == nil {
		if i.Mode().Perm()&0077 != 0 {
			add("socket_permissions", "error", i18n.Errorf("backend.diagnostic.unix_control_socket_is_accessible_beyond_the_owner_restart_nagi_after_fixin", "Unix control socket is accessible beyond the owner; restart Nagi after fixing runtime directory permissions."))
		} else {
			add("socket_permissions", "ok", i18n.Errorf("backend.diagnostic.unix_control_socket_is_owner_only", "Unix control socket is owner-only."))
		}
	}
	var cfg map[string]any
	if err := api.Get(ctx, "/configs", &cfg); err != nil {
		add("configuration", "warning", i18n.Errorf("backend.diagnostic.mihomo_configuration_is_unavailable_start_mihomo_before_checking_listeners_", "Mihomo configuration is unavailable; start mihomo before checking listeners, DNS, and controller exposure."))
	} else {
		ports := []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port"}
		for _, key := range ports {
			if n := number(cfg[key]); n > 0 {
				checkPort(add, key, n)
			}
		}
		if dns, ok := cfg["dns"].(map[string]any); ok {
			enabled, _ := dns["enable"].(bool)
			if enabled {
				add("dns", "ok", i18n.Errorf("backend.diagnostic.mihomo_dns_is_enabled", "Mihomo DNS is enabled."))
			} else {
				add("dns", "warning", i18n.Errorf("backend.diagnostic.mihomo_dns_is_disabled_system_dns_may_bypass_proxy_policy", "Mihomo DNS is disabled; system DNS may bypass proxy policy."))
			}
		} else {
			add("dns", "warning", i18n.Errorf("backend.diagnostic.mihomo_dns_configuration_is_unavailable", "Mihomo DNS configuration is unavailable."))
		}
		if bind, ok := cfg["bind-address"].(string); ok && bind != "" && bind != "127.0.0.1" && bind != "::1" && bind != "localhost" {
			add("listener_exposure", "warning", i18n.Errorf("backend.diagnostic.a_proxy_listener_bind_address_is_not_loopback_lan_exposure_is_enabled_revie", "A proxy listener bind address is not loopback; LAN exposure is enabled. Review allow-lan and firewall settings."))
		} else if cfg["allow-lan"] == true {
			add("listener_exposure", "warning", i18n.Errorf("backend.diagnostic.allow_lan_is_enabled_proxy_listeners_may_be_reachable_from_the_lan", "allow-lan is enabled; proxy listeners may be reachable from the LAN."))
		} else {
			add("listener_exposure", "ok", i18n.Errorf("backend.diagnostic.proxy_listeners_are_configured_for_local_access", "Proxy listeners are configured for local access."))
		}
		if controller, ok := cfg["external-controller"].(string); ok && controller != "" && !strings.HasPrefix(controller, "127.0.0.1") && !strings.HasPrefix(controller, "127.0.0.1:") && !strings.HasPrefix(controller, "[::1]") {
			add("controller_exposure", "error", i18n.Errorf("backend.diagnostic.external_controller_is_exposed_beyond_loopback_use_the_private_unix_socket_", "External controller is exposed beyond loopback; use the private Unix Socket or bind the controller to loopback."))
		} else {
			add("controller_exposure", "ok", i18n.Errorf("backend.diagnostic.no_non_loopback_external_controller_exposure_was_reported", "No non-loopback external controller exposure was reported."))
		}
	}
	if baseHealthy := base.Healthy; baseHealthy == false {
		r.Healthy = false
	}
	return r
}
func number(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
func checkPort(add func(string, string, *i18n.Message), name string, port int) {
	conn, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if e != nil {
		add("port_"+name, "warning", i18n.Errorf("backend.diagnostic.listener_d_is_configured_but_not_reachable_on_loopback", "Listener %d is configured but not reachable on loopback.", port))
		return
	}
	conn.Close()
	add("port_"+name, "ok", i18n.Errorf("backend.diagnostic.listener_is_reachable_on_loopback_port_d", "Listener is reachable on loopback port %d.", port))
}
func RedactedJSON(r FullReport) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
func Export(path string, r FullReport) error {
	if path == "" || filepath.IsAbs(path) == false {
		return i18n.Errorf("backend.diagnostic.diagnostic_export_path_must_be_an_absolute_path", "diagnostic export path must be an absolute path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := RedactedJSON(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return err
	}
	return f.Chmod(0600)
}
