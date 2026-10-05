// Package traffic manages operating-system proxy settings and a durable restore journal.
package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/platform"
)

type Setting struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
}
type State struct {
	Enabled    bool                       `json:"enabled"`
	Backend    string                     `json:"backend"`
	HTTP       Setting                    `json:"http"`
	HTTPS      Setting                    `json:"https"`
	SOCKS      Setting                    `json:"socks"`
	Services   []string                   `json:"services,omitempty"`
	PerService map[string]ServiceSettings `json:"per_service,omitempty"`
	Mode       string                     `json:"mode,omitempty"`
}
type ServiceSettings struct {
	HTTP  Setting `json:"http"`
	HTTPS Setting `json:"https"`
	SOCKS Setting `json:"socks"`
}
type journal struct {
	PID    int   `json:"pid"`
	Before State `json:"before"`
}
type Manager struct {
	Dir string
	Run func(context.Context, string, ...string) (string, error)
	OS  string
}

func New(dir string) *Manager { return &Manager{Dir: dir, Run: run, OS: runtime.GOOS} }
func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
func (m *Manager) call(ctx context.Context, name string, args ...string) (string, error) {
	if m.Run != nil {
		return m.Run(ctx, name, args...)
	}
	return run(ctx, name, args...)
}
func (m *Manager) journalPath() string { return filepath.Join(m.Dir, "system-proxy-restore.json") }
func (m *Manager) lockPath() string    { return filepath.Join(m.Dir, "system-proxy.lock") }
func (m *Manager) locked(fn func() error) error {
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(m.lockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := platform.Lock(f); err != nil {
		return err
	}
	defer platform.Unlock(f)
	return fn()
}
func (m *Manager) readJournal() (journal, error) {
	var j journal
	b, err := os.ReadFile(m.journalPath())
	if err != nil {
		return j, err
	}
	err = json.Unmarshal(b, &j)
	return j, err
}
func (m *Manager) writeJournal(j journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.Dir, ".system-proxy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.journalPath())
}
func (m *Manager) Status(ctx context.Context) (State, error) {
	var result State
	err := m.locked(func() error { var e error; result, e = m.current(ctx); return e })
	return result, err
}
func (m *Manager) Enable(ctx context.Context, pid int, port int) (State, error) {
	var result State
	err := m.locked(func() error {
		if port < 1 || port > 65535 {
			return errors.New("a running local HTTP/mixed port is required; check `nagi ports status`")
		}
		j, e := m.readJournal()
		if e == nil {
			if j.PID != pid && !platform.Alive(j.PID) {
				if e = m.apply(ctx, j.Before); e != nil {
					return fmt.Errorf("restore settings from previous engine: %w", e)
				}
				_ = os.Remove(m.journalPath())
			} else {
				return errors.New("system proxy is already managed; use `nagi system-proxy disable` first")
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		before, e := m.current(ctx)
		if e != nil {
			return e
		}
		if e = m.writeJournal(journal{PID: pid, Before: before}); e != nil {
			return e
		}
		target := before
		target.Enabled = true
		target.HTTP = Setting{true, "127.0.0.1", port}
		target.HTTPS = target.HTTP
		target.SOCKS = Setting{}
		if target.PerService != nil {
			for service := range target.PerService {
				target.PerService[service] = ServiceSettings{HTTP: target.HTTP, HTTPS: target.HTTPS}
			}
		}
		target.Mode = "manual"
		if e = m.apply(ctx, target); e != nil {
			restoreErr := m.apply(ctx, before)
			if restoreErr == nil {
				_ = os.Remove(m.journalPath())
			}
			return fmt.Errorf("enable system proxy: %w; restore: %v", e, restoreErr)
		}
		result, e = m.current(ctx)
		if e == nil && (!result.HTTP.Enabled || !result.HTTPS.Enabled || result.HTTP.Port != port || result.HTTPS.Port != port) {
			e = errors.New("OS did not accept HTTP/HTTPS proxy settings; check desktop session or networksetup permissions")
		}
		if e != nil {
			restoreErr := m.apply(ctx, before)
			if restoreErr == nil {
				_ = os.Remove(m.journalPath())
			}
			return fmt.Errorf("verify system proxy: %w; restore: %v", e, restoreErr)
		}
		return nil
	})
	return result, err
}
func (m *Manager) Disable(ctx context.Context) (State, error) {
	var result State
	err := m.locked(func() error {
		j, e := m.readJournal()
		if errors.Is(e, os.ErrNotExist) {
			result, e = m.current(ctx)
			if e == nil {
				return nil
			}
			return e
		}
		if e != nil {
			return e
		}
		if e = m.apply(ctx, j.Before); e != nil {
			return fmt.Errorf("restore system proxy settings: %w; saved settings retained for retry", e)
		}
		if e = os.Remove(m.journalPath()); e != nil {
			return e
		}
		result, e = m.current(ctx)
		return e
	})
	return result, err
}
func (m *Manager) RestoreIfStopped(ctx context.Context, pid int) error {
	return m.locked(func() error {
		j, e := m.readJournal()
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if pid != 0 && j.PID != pid {
			return nil
		}
		if platform.Alive(j.PID) {
			return nil
		}
		if e = m.apply(ctx, j.Before); e != nil {
			return fmt.Errorf("restore system proxy settings after engine exit: %w", e)
		}
		return os.Remove(m.journalPath())
	})
}
func (m *Manager) Managed() bool { _, e := os.Stat(m.journalPath()); return e == nil }
func (m *Manager) Watch(ctx context.Context, pid int) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.Managed() {
				return
			}
			if !platform.Alive(pid) {
				_ = m.RestoreIfStopped(context.Background(), pid)
				return
			}
		}
	}
}
func (m *Manager) current(ctx context.Context) (State, error) {
	switch m.OS {
	case "darwin":
		return m.macCurrent(ctx)
	case "linux":
		return m.gnomeCurrent(ctx)
	default:
		return State{}, fmt.Errorf("system proxy is unsupported on %s", m.OS)
	}
}
func (m *Manager) apply(ctx context.Context, s State) error {
	switch s.Backend {
	case "macos-networksetup":
		return m.macApply(ctx, s)
	case "gnome-gsettings":
		return m.gnomeApply(ctx, s)
	default:
		return errors.New("unknown system proxy backend")
	}
}
func parseMacSetting(v string) (Setting, error) {
	var s Setting
	for _, line := range strings.Split(v, "\n") {
		key, value, _ := strings.Cut(line, ": ")
		switch key {
		case "Enabled":
			s.Enabled = value == "Yes"
		case "Server":
			s.Host = value
		case "Port":
			s.Port, _ = strconv.Atoi(value)
		}
	}
	return s, nil
}
func (m *Manager) macCurrent(ctx context.Context) (State, error) {
	services, e := m.call(ctx, "networksetup", "-listallnetworkservices")
	if e != nil {
		return State{}, fmt.Errorf("networksetup unavailable; macOS network service access is required: %w", e)
	}
	s := State{Backend: "macos-networksetup", PerService: map[string]ServiceSettings{}}
	for _, service := range strings.Split(services, "\n") {
		if service == "" || strings.HasPrefix(service, "An asterisk") || strings.HasPrefix(service, "*") {
			continue
		}
		s.Services = append(s.Services, service)
		var values ServiceSettings
		for _, item := range []struct {
			flag string
			dest *Setting
		}{{"-getwebproxy", &values.HTTP}, {"-getsecurewebproxy", &values.HTTPS}, {"-getsocksfirewallproxy", &values.SOCKS}} {
			out, e := m.call(ctx, "networksetup", item.flag, service)
			if e != nil {
				return s, e
			}
			*item.dest, _ = parseMacSetting(out)
		}
		s.PerService[service] = values
		if len(s.Services) == 1 {
			s.HTTP = values.HTTP
			s.HTTPS = values.HTTPS
			s.SOCKS = values.SOCKS
		}
		s.Enabled = s.Enabled || values.HTTP.Enabled || values.HTTPS.Enabled || values.SOCKS.Enabled
	}
	if len(s.Services) == 0 {
		return s, errors.New("no active macOS network services found")
	}
	return s, nil
}
func (m *Manager) macApply(ctx context.Context, s State) error {
	for _, service := range s.Services {
		values, ok := s.PerService[service]
		if !ok {
			values = ServiceSettings{s.HTTP, s.HTTPS, s.SOCKS}
		}
		for _, item := range []struct {
			set, state string
			value      Setting
		}{{"-setwebproxy", "-setwebproxystate", values.HTTP}, {"-setsecurewebproxy", "-setsecurewebproxystate", values.HTTPS}, {"-setsocksfirewallproxy", "-setsocksfirewallproxystate", values.SOCKS}} {
			if item.value.Host != "" && item.value.Port > 0 {
				if _, e := m.call(ctx, "networksetup", item.set, service, item.value.Host, strconv.Itoa(item.value.Port)); e != nil {
					return e
				}
			}
			state := "off"
			if item.value.Enabled {
				state = "on"
			}
			if _, e := m.call(ctx, "networksetup", item.state, service, state); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m *Manager) gnomeCurrent(ctx context.Context) (State, error) {
	mode, e := m.call(ctx, "gsettings", "get", "org.gnome.system.proxy", "mode")
	if e != nil {
		return State{}, errors.New("GNOME gsettings is unavailable; Linux system proxy requires an active GNOME desktop session")
	}
	s := State{Backend: "gnome-gsettings", Mode: strings.Trim(mode, "' "), Enabled: strings.Trim(mode, "' ") == "manual"}
	for _, item := range []struct {
		name string
		dest *Setting
	}{{"http", &s.HTTP}, {"https", &s.HTTPS}, {"socks", &s.SOCKS}} {
		schema := "org.gnome.system.proxy." + item.name
		host, e := m.call(ctx, "gsettings", "get", schema, "host")
		if e != nil {
			return s, e
		}
		port, e := m.call(ctx, "gsettings", "get", schema, "port")
		if e != nil {
			return s, e
		}
		item.dest.Host = strings.Trim(host, "' ")
		item.dest.Port, _ = strconv.Atoi(port)
		item.dest.Enabled = s.Enabled && item.dest.Port > 0
	}
	return s, nil
}
func (m *Manager) gnomeApply(ctx context.Context, s State) error {
	for _, item := range []struct {
		name  string
		value Setting
	}{{"http", s.HTTP}, {"https", s.HTTPS}, {"socks", s.SOCKS}} {
		schema := "org.gnome.system.proxy." + item.name
		if _, e := m.call(ctx, "gsettings", "set", schema, "host", item.value.Host); e != nil {
			return e
		}
		if _, e := m.call(ctx, "gsettings", "set", schema, "port", strconv.Itoa(item.value.Port)); e != nil {
			return e
		}
	}
	mode := s.Mode
	if mode == "" {
		mode = "none"
	}
	if s.Enabled {
		mode = "manual"
	}
	_, e := m.call(ctx, "gsettings", "set", "org.gnome.system.proxy", "mode", mode)
	return e
}
