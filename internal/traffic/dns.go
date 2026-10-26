package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/platform"
)

// DNSManager owns only macOS network-service DNS settings, not DNS resolution.
// Dir must be private to its owner; privileged callers must use root-owned storage.
type DNSManager struct {
	Dir   string
	OS    string
	Run   func(context.Context, string, ...string) (string, error)
	Alive func(int) bool
}
type DNSState struct {
	Supported bool                `json:"supported"`
	Managed   bool                `json:"managed"`
	Status    string              `json:"status"`
	Targets   []string            `json:"targets,omitempty"`
	Services  map[string][]string `json:"services,omitempty"`
	Conflicts []string            `json:"conflicts,omitempty"`
}
type dnsEntry struct {
	Before  []string `json:"before"`
	Applied []string `json:"applied"`
	Pending []string `json:"pending,omitempty"`
}
type dnsJournal struct {
	PID      int                 `json:"pid"`
	Services map[string]dnsEntry `json:"services"`
}

func NewDNS(dir string) *DNSManager {
	return &DNSManager{Dir: dir, OS: runtime.GOOS, Run: run, Alive: platform.Alive}
}

// DNSTargets uses the same first-address-plus-one DNS endpoint as mihomo's TUN.
func DNSTargets(tun map[string]any) ([]string, error) {
	var result []string
	for _, field := range []string{"inet4-address", "inet6-address"} {
		var values []string
		switch v := tun[field].(type) {
		case nil:
		case []string:
			values = v
		case []any:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return nil, i18n.Errorf("backend.traffic.invalid_tun_s", "invalid tun.%s", field)
				}
				values = append(values, s)
			}
		default:
			return nil, i18n.Errorf("backend.traffic.invalid_tun_s", "invalid tun.%s", field)
		}
		for _, v := range values {
			p, err := netip.ParsePrefix(v)
			if err != nil {
				return nil, i18n.Errorf("backend.traffic.invalid_tun_s_w", "invalid tun.%s: %w", field, err)
			}
			a := p.Addr().Next()
			if !a.IsValid() || !p.Contains(a) || (field == "inet4-address") != a.Is4() {
				return nil, i18n.Errorf("backend.traffic.tun_s_has_no_usable_dns_endpoint", "tun.%s has no usable DNS endpoint", field)
			}
			if !containsDNS(result, a.String()) {
				result = append(result, a.String())
			}
		}
	}
	if len(result) == 0 {
		return nil, i18n.Errorf("backend.traffic.running_tun_has_no_dns_endpoint_addresses", "running TUN has no DNS endpoint addresses")
	}
	return result, nil
}
func containsDNS(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}
func dnsEqual(a, b []string) bool {
	return len(a) == len(b) && reflect.DeepEqual(append([]string{}, a...), append([]string{}, b...))
}
func (m *DNSManager) path() string { return filepath.Join(m.Dir, "tun-dns-restore.json") }
func (m *DNSManager) Managed() bool {
	if m.OS != "darwin" {
		return false
	}
	_, err := os.Stat(m.path())
	return err == nil
}
func (m *DNSManager) alive(pid int) bool {
	if m.Alive != nil {
		return m.Alive(pid)
	}
	return platform.Alive(pid)
}
func (m *DNSManager) call(ctx context.Context, args ...string) (string, error) {
	if m.Run != nil {
		return m.Run(ctx, "/usr/sbin/networksetup", args...)
	}
	return run(ctx, "/usr/sbin/networksetup", args...)
}
func (m *DNSManager) locked(fn func() error) error {
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.Dir, "tun-dns.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = platform.Lock(f); err != nil {
		return err
	}
	defer platform.Unlock(f)
	return fn()
}
func (m *DNSManager) read() (dnsJournal, error) {
	var j dnsJournal
	b, err := os.ReadFile(m.path())
	if err != nil {
		return j, err
	}
	err = json.Unmarshal(b, &j)
	if err == nil && (j.PID <= 0 || len(j.Services) == 0) {
		err = i18n.Errorf("backend.traffic.invalid_tun_dns_restore_journal", "invalid TUN DNS restore journal")
	}
	return j, err
}
func (m *DNSManager) save(j dnsJournal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.Dir, ".tun-dns-*")
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
	if err = os.Rename(f.Name(), m.path()); err != nil {
		return err
	}
	return m.syncDir()
}
func (m *DNSManager) syncDir() error {
	d, err := os.Open(m.Dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (m *DNSManager) remove() error {
	if err := os.Remove(m.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return m.syncDir()
}
func (m *DNSManager) current(ctx context.Context) (map[string][]string, error) {
	out, err := m.call(ctx, "-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	result := map[string][]string{}
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "An asterisk") {
			continue
		}
		name = strings.TrimPrefix(name, "*")
		value, err := m.call(ctx, "-getdnsservers", name)
		if err != nil {
			return nil, err
		}
		var servers []string
		if !strings.HasPrefix(value, "There aren't any DNS Servers set on ") {
			for _, line := range strings.Fields(value) {
				a, err := netip.ParseAddr(line)
				if err != nil {
					return nil, i18n.Errorf("backend.traffic.unexpected_dns_settings_for_q_w", "unexpected DNS settings for %q: %w", name, err)
				}
				servers = append(servers, a.String())
			}
		}
		result[name] = servers
	}
	if len(result) == 0 {
		return nil, i18n.Errorf("backend.traffic.no_macos_network_services_found", "no macOS network services found")
	}
	return result, nil
}
func (m *DNSManager) set(ctx context.Context, name string, servers []string) error {
	args := []string{"-setdnsservers", name}
	if len(servers) == 0 {
		args = append(args, "Empty")
	} else {
		args = append(args, servers...)
	}
	if _, err := m.call(ctx, args...); err != nil {
		return i18n.Errorf("backend.traffic.set_dns_for_q_administrator_permissions_required_w", "set DNS for %q (administrator permissions required): %w", name, err)
	}
	out, err := m.call(ctx, "-getdnsservers", name)
	if err != nil {
		return err
	}
	actual := strings.Fields(out)
	if strings.HasPrefix(out, "There aren't any DNS Servers set on ") {
		actual = nil
	}
	if !dnsEqual(actual, servers) {
		return i18n.Errorf("backend.traffic.macos_did_not_retain_dns_settings_for_q", "macOS did not retain DNS settings for %q", name)
	}
	return nil
}
func (m *DNSManager) Status(ctx context.Context) (DNSState, error) {
	s := DNSState{Supported: m.OS == "darwin", Status: "unsupported"}
	if !s.Supported {
		return s, nil
	}
	err := m.locked(func() error {
		s.Status = "unmanaged"
		var err error
		s.Services, err = m.current(ctx)
		if err != nil {
			return err
		}
		j, err := m.read()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		s.Managed = true
		s.Status = "managed"
		for name, e := range j.Services {
			if len(s.Targets) == 0 {
				s.Targets = append([]string{}, e.Applied...)
			}
			if a, ok := s.Services[name]; !ok || !dnsEqual(a, e.Applied) || len(e.Pending) > 0 {
				s.Conflicts = append(s.Conflicts, name)
			}
		}
		if len(s.Conflicts) > 0 {
			s.Status = "attention"
		}
		if !m.alive(j.PID) {
			s.Status = "stale"
		}
		return nil
	})
	return s, err
}
func (m *DNSManager) Enable(ctx context.Context, pid int, targets []string) (DNSState, error) {
	return m.Sync(ctx, pid, targets)
}
func (m *DNSManager) Sync(ctx context.Context, pid int, targets []string) (DNSState, error) {
	if m.OS != "darwin" {
		return DNSState{Status: "unsupported"}, nil
	}
	if pid <= 0 || len(targets) == 0 {
		return DNSState{}, i18n.Errorf("backend.traffic.running_tun_pid_and_dns_endpoint_required", "running TUN PID and DNS endpoint required")
	}
	for _, v := range targets {
		if _, err := netip.ParseAddr(v); err != nil {
			return DNSState{}, i18n.Errorf("backend.traffic.invalid_dns_endpoint_w", "invalid DNS endpoint: %w", err)
		}
	}
	err := m.locked(func() error {
		j, err := m.read()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && j.PID != pid {
			if m.alive(j.PID) {
				return i18n.Errorf("backend.traffic.tun_dns_is_owned_by_another_live_engine", "TUN DNS is owned by another live engine")
			}
			if err = m.restore(ctx, j); err != nil {
				return err
			}
			j = dnsJournal{}
		}
		actual, err := m.current(ctx)
		if err != nil {
			return err
		}
		if j.Services == nil {
			j = dnsJournal{PID: pid, Services: map[string]dnsEntry{}}
		}
		previous := dnsJournal{PID: j.PID, Services: map[string]dnsEntry{}}
		for name, e := range j.Services {
			previous.Services[name] = e
			a, ok := actual[name]
			if !ok {
				return i18n.Errorf("backend.traffic.managed_dns_service_q_is_missing", "managed DNS service %q is missing", name)
			}
			if len(e.Pending) > 0 {
				return i18n.Errorf("backend.traffic.unfinished_tun_dns_transaction_restore_dns_before_retrying", "unfinished TUN DNS transaction; restore DNS before retrying")
			}
			if !dnsEqual(a, e.Applied) {
				return i18n.Errorf("backend.traffic.dns_conflict_on_q_settings_changed_externally_left_untouched", "DNS conflict on %q: settings changed externally; left untouched", name)
			}
		}
		changed := false
		for name, a := range actual {
			e, ok := j.Services[name]
			if !ok {
				e = dnsEntry{Before: append([]string{}, a...), Applied: append([]string{}, a...)}
			}
			if !dnsEqual(a, targets) {
				e.Pending = append([]string{}, targets...)
				changed = true
			}
			j.Services[name] = e
		}
		if !changed {
			return nil
		}
		if err = m.save(j); err != nil {
			return err
		}
		var applyErr error
		for name, e := range j.Services {
			if len(e.Pending) > 0 {
				if applyErr = m.set(ctx, name, e.Pending); applyErr != nil {
					break
				}
			}
		}
		if applyErr != nil {
			return i18n.Errorf("backend.traffic.apply_tun_dns_w_rollback_v", "apply TUN DNS: %w; rollback: %v", applyErr, m.rollback(ctx, j, previous))
		}
		for name, e := range j.Services {
			e.Applied = append([]string{}, targets...)
			e.Pending = nil
			j.Services[name] = e
		}
		if err = m.save(j); err != nil {
			return i18n.Errorf("backend.traffic.commit_tun_dns_w_rollback_v", "commit TUN DNS: %w; rollback: %v", err, m.rollback(ctx, j, previous))
		}
		return nil
	})
	if err != nil {
		return DNSState{}, err
	}
	return m.Status(ctx)
}

// rollback and restore compare observed values before every write; third-party edits survive.
func (m *DNSManager) rollback(ctx context.Context, attempt, previous dnsJournal) error {
	actual, err := m.current(ctx)
	if err != nil {
		return err
	}
	var problems []error
	for name, e := range attempt.Services {
		desired := e.Before
		if old, ok := previous.Services[name]; ok {
			desired = old.Applied
		}
		a, ok := actual[name]
		if !ok {
			problems = append(problems, i18n.Errorf("backend.traffic.dns_service_q_missing_during_rollback", "DNS service %q missing during rollback", name))
			continue
		}
		if dnsEqual(a, desired) {
			continue
		}
		if !dnsEqual(a, e.Pending) && !dnsEqual(a, e.Applied) {
			problems = append(problems, i18n.Errorf("backend.traffic.dns_conflict_on_q_during_rollback", "DNS conflict on %q during rollback", name))
			continue
		}
		if err = m.set(ctx, name, desired); err != nil {
			problems = append(problems, err)
		}
	}
	if err = errors.Join(problems...); err != nil {
		return err
	}
	if len(previous.Services) == 0 {
		return m.remove()
	}
	return m.save(previous)
}
func (m *DNSManager) restore(ctx context.Context, j dnsJournal) error {
	actual, err := m.current(ctx)
	if err != nil {
		return err
	}
	var problems []error
	remaining := dnsJournal{PID: j.PID, Services: map[string]dnsEntry{}}
	for name, e := range j.Services {
		a, ok := actual[name]
		if !ok {
			// The service may have been removed externally. There is no
			// setting left to restore, so discard this journal entry.
			continue
		}
		if dnsEqual(a, e.Before) {
			continue
		}
		if !dnsEqual(a, e.Applied) && !(len(e.Pending) > 0 && dnsEqual(a, e.Pending)) {
			problems = append(problems, i18n.Errorf("backend.traffic.dns_conflict_on_q_settings_changed_externally_left_untouched", "DNS conflict on %q: settings changed externally; left untouched", name))
			remaining.Services[name] = e
			continue
		}
		if err = m.set(ctx, name, e.Before); err != nil {
			problems = append(problems, err)
			remaining.Services[name] = e
		}
	}
	if err = errors.Join(problems...); err != nil {
		if len(remaining.Services) == 0 {
			return err
		}
		if saveErr := m.save(remaining); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	return m.remove()
}
func (m *DNSManager) Disable(ctx context.Context) (DNSState, error) {
	if m.OS != "darwin" {
		return DNSState{Status: "unsupported"}, nil
	}
	err := m.locked(func() error {
		j, err := m.read()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return m.restore(ctx, j)
	})
	if err != nil {
		return DNSState{}, err
	}
	return m.Status(ctx)
}
func (m *DNSManager) RestoreIfStopped(ctx context.Context, pid int) error {
	if m.OS != "darwin" {
		return nil
	}
	return m.locked(func() error {
		j, err := m.read()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if pid != 0 && pid != j.PID || m.alive(j.PID) {
			return nil
		}
		return m.restore(ctx, j)
	})
}
func (m *DNSManager) Watch(ctx context.Context, pid int) error {
	if m.OS != "darwin" {
		return nil
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if !m.Managed() {
				return nil
			}
			if !m.alive(pid) {
				recoverCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				return m.RestoreIfStopped(recoverCtx, pid)
			}
		}
	}
}
