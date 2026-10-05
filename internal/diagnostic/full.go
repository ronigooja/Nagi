package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
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
	r := FullReport{Healthy: true, Checks: []Check{}, Platform: runtime.GOOS}
	add := func(n, s, m string) {
		r.Checks = append(r.Checks, Check{n, s, m})
		if s == "error" {
			r.Healthy = false
		}
	}
	base := Run(ctx, paths, binary)
	for _, c := range base.Checks {
		add(c.Name, c.Status, c.Message)
	}
	checkPerm := func(name, path string, mode os.FileMode) {
		i, e := os.Stat(path)
		if errors.Is(e, os.ErrNotExist) {
			return
		}
		if e != nil {
			add(name, "error", "Cannot inspect permissions; check owner access.")
			return
		}
		if !i.Mode().IsRegular() && name != "socket_permissions" {
			return
		}
		if mode&0077 != 0 {
			add(name, "warning", fmt.Sprintf("%s is accessible to group or other users; restrict it to owner permissions.", path))
		} else {
			add(name, "ok", "Owner-only permissions are set.")
		}
	}
	checkPerm("config_permissions", paths.ConfigDir, os.FileMode(0077))
	checkPerm("runtime_permissions", paths.RuntimeDir, os.FileMode(0077))
	if i, e := os.Stat(paths.SocketPath); e == nil {
		if i.Mode().Perm()&0077 != 0 {
			add("socket_permissions", "error", "Unix control socket is accessible beyond the owner; restart Nagi after fixing runtime directory permissions.")
		} else {
			add("socket_permissions", "ok", "Unix control socket is owner-only.")
		}
	}
	var cfg map[string]any
	api := control.New(paths.SocketPath)
	if err := api.Get(ctx, "/configs", &cfg); err != nil {
		add("configuration", "warning", "Mihomo configuration is unavailable; start mihomo before checking listeners, DNS, and controller exposure.")
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
				add("dns", "ok", "Mihomo DNS is enabled.")
			} else {
				add("dns", "warning", "Mihomo DNS is disabled; system DNS may bypass proxy policy.")
			}
		} else {
			add("dns", "warning", "Mihomo DNS configuration is unavailable.")
		}
		if bind, ok := cfg["bind-address"].(string); ok && bind != "" && bind != "127.0.0.1" && bind != "::1" && bind != "localhost" {
			add("listener_exposure", "warning", "A proxy listener bind address is not loopback; LAN exposure is enabled. Review allow-lan and firewall settings.")
		} else if cfg["allow-lan"] == true {
			add("listener_exposure", "warning", "allow-lan is enabled; proxy listeners may be reachable from the LAN.")
		} else {
			add("listener_exposure", "ok", "Proxy listeners are configured for local access.")
		}
		if controller, ok := cfg["external-controller"].(string); ok && controller != "" && !strings.HasPrefix(controller, "127.0.0.1") && !strings.HasPrefix(controller, "127.0.0.1:") && !strings.HasPrefix(controller, "[::1]") {
			add("controller_exposure", "error", "External controller is exposed beyond loopback; use the private Unix Socket or bind the controller to loopback.")
		} else {
			add("controller_exposure", "ok", "No non-loopback external controller exposure was reported.")
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
func checkPort(add func(string, string, string), name string, port int) {
	conn, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if e != nil {
		add("port_"+name, "warning", fmt.Sprintf("Listener %d is configured but not reachable on loopback.", port))
		return
	}
	conn.Close()
	add("port_"+name, "ok", fmt.Sprintf("Listener is reachable on loopback port %d.", port))
}
func RedactedJSON(r FullReport) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
func Export(path string, r FullReport) error {
	if path == "" || filepath.IsAbs(path) == false {
		return errors.New("diagnostic export path must be an absolute path")
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
