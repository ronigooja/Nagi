// Package browserprefs manages explicitly requested per-user browser preferences.
// It does not configure mihomo or intercept browser traffic.
package browserprefs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/platform"
)

const RestrictedValue = "disable_non_proxied_udp"

type State struct {
	Available       bool    `json:"available"`
	CurrentValue    *string `json:"current_value"`
	Restricted      bool    `json:"restricted"`
	Managed         bool    `json:"managed"`
	Forced          bool    `json:"forced"`
	CanEnable       bool    `json:"can_enable"`
	CanDisable      bool    `json:"can_disable"`
	CanRestore      bool    `json:"can_restore"`
	RequiresRestart bool    `json:"requires_restart"`
}

type preference struct {
	Value  *string `json:"value"`
	Forced bool    `json:"forced"`
	Valid  bool    `json:"valid"`
}
type journal struct {
	Version int     `json:"version"`
	Before  *string `json:"before"`
	Applied string  `json:"applied"`
}
type Manager struct {
	Dir string
	OS  string
	Run func(context.Context, string, ...string) (string, error)
}

func New(dir string) *Manager { return &Manager{Dir: dir, OS: runtime.GOOS, Run: run} }
func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", i18n.Errorf("backend.browserprefs.command_failed", "Cannot read or update Chrome preferences; check macOS preference access.")
	}
	return string(out), nil
}
func (m *Manager) path() string { return filepath.Join(m.Dir, "chrome-webrtc-restore.json") }
func equal(a, b *string) bool   { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func valid(v *string) bool {
	if v == nil {
		return true
	}
	switch *v {
	case "default", "default_public_and_private_interfaces", "default_public_interface_only", RestrictedValue:
		return true
	}
	return false
}
func (m *Manager) read(ctx context.Context) (preference, error) { return m.call(ctx, "read", nil, nil) }
func (m *Manager) call(ctx context.Context, action string, expected, target *string) (preference, error) {
	var p preference
	a, _ := json.Marshal(expected)
	b, _ := json.Marshal(target)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	runner := m.Run
	if runner == nil {
		runner = run
	}
	raw, err := runner(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", chromeScript, action, string(a), string(b))
	if err != nil {
		return p, err
	}
	if json.Unmarshal([]byte(raw), &p) != nil || !p.Valid || !valid(p.Value) {
		return p, i18n.Errorf("backend.browserprefs.invalid_value", "Chrome WebRTC preference has an unsupported value; change it through its original management source.")
	}
	return p, nil
}
func (m *Manager) saved() (*journal, error) {
	b, err := os.ReadFile(m.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j journal
	if json.Unmarshal(b, &j) != nil || j.Version != 1 || j.Applied != RestrictedValue || !valid(j.Before) || j.Before != nil && *j.Before == RestrictedValue {
		return nil, i18n.Errorf("backend.browserprefs.invalid_journal", "Chrome WebRTC recovery record is invalid; preserve the record and resolve it before retrying.")
	}
	return &j, nil
}
func (m *Manager) save(j journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.Dir, ".chrome-webrtc-*")
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
	return os.Rename(f.Name(), m.path())
}
func state(p preference, j *journal) State {
	restricted := p.Value != nil && *p.Value == RestrictedValue
	return State{Available: true, CurrentValue: p.Value, Restricted: restricted, Managed: j != nil, Forced: p.Forced, CanEnable: !p.Forced && !restricted && (j == nil || equal(p.Value, j.Before)), CanDisable: !p.Forced && (p.Value != nil || j != nil), CanRestore: !p.Forced && j != nil && (restricted || equal(p.Value, j.Before))}
}
func (m *Manager) locked(fn func() (State, error)) (State, error) {
	if m.OS != "darwin" {
		return State{}, i18n.Errorf("backend.browserprefs.unsupported", "Chrome WebRTC preferences are supported only on macOS.")
	}
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return State{}, err
	}
	f, err := os.OpenFile(filepath.Join(m.Dir, "chrome-webrtc.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return State{}, err
	}
	defer f.Close()
	if err = platform.Lock(f); err != nil {
		return State{}, err
	}
	defer platform.Unlock(f)
	return fn()
}
func (m *Manager) Status(ctx context.Context) (State, error) {
	return m.locked(func() (State, error) {
		p, err := m.read(ctx)
		if err != nil {
			return State{}, err
		}
		j, err := m.saved()
		return state(p, j), err
	})
}
func (m *Manager) Enable(ctx context.Context) (State, error) {
	return m.locked(func() (State, error) {
		p, err := m.read(ctx)
		if err != nil {
			return State{}, err
		}
		j, err := m.saved()
		if err != nil {
			return State{}, err
		}
		if p.Forced {
			return State{}, i18n.Errorf("backend.browserprefs.forced", "Chrome WebRTC preference is managed by an administrator and cannot be changed by Nagi.")
		}
		if p.Value != nil && *p.Value == RestrictedValue {
			return state(p, j), nil
		}
		if j != nil && !equal(p.Value, j.Before) {
			return State{}, conflict()
		}
		if j == nil {
			j = &journal{Version: 1, Before: p.Value, Applied: RestrictedValue}
			if err = m.save(*j); err != nil {
				return State{}, err
			}
		}
		target := RestrictedValue
		p, err = m.call(ctx, "write", p.Value, &target)
		if err != nil {
			return State{}, err
		}
		if p.Forced || !equal(p.Value, &target) {
			return State{}, conflict()
		}
		result := state(p, j)
		result.RequiresRestart = true
		return result, nil
	})
}

// Disable restores Chrome's default preference behavior, including when the
// restriction was originally configured outside Nagi. Unlike Restore, it does
// not reapply the saved preference; successful removal discards that backup.
func (m *Manager) Disable(ctx context.Context) (State, error) {
	return m.locked(func() (State, error) {
		p, err := m.read(ctx)
		if err != nil {
			return State{}, err
		}
		if p.Forced {
			return State{}, i18n.Errorf("backend.browserprefs.forced", "Chrome WebRTC preference is managed by an administrator and cannot be changed by Nagi.")
		}
		changed := p.Value != nil
		if changed {
			p, err = m.call(ctx, "write", p.Value, nil)
			if err != nil {
				return State{}, err
			}
			if p.Forced || p.Value != nil {
				return State{}, conflict()
			}
		}
		if err = os.Remove(m.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return State{}, err
		}
		result := state(p, nil)
		result.RequiresRestart = changed
		return result, nil
	})
}

func conflict() error {
	return i18n.Errorf("backend.browserprefs.conflict", "Chrome WebRTC preference changed outside Nagi; no further changes were applied. Refresh its state before retrying.")
}
func (m *Manager) Restore(ctx context.Context) (State, error) {
	return m.locked(func() (State, error) {
		p, err := m.read(ctx)
		if err != nil {
			return State{}, err
		}
		j, err := m.saved()
		if err != nil {
			return State{}, err
		}
		if j == nil {
			return state(p, nil), nil
		}
		if p.Forced {
			return State{}, i18n.Errorf("backend.browserprefs.forced", "Chrome WebRTC preference is managed by an administrator and cannot be changed by Nagi.")
		}
		applied := RestrictedValue
		if !equal(p.Value, &applied) && !equal(p.Value, j.Before) {
			return State{}, conflict()
		}
		if !equal(p.Value, j.Before) {
			p, err = m.call(ctx, "write", p.Value, j.Before)
			if err != nil {
				return State{}, err
			}
			if p.Forced || !equal(p.Value, j.Before) {
				return State{}, conflict()
			}
		}
		if err = os.Remove(m.path()); err != nil {
			return State{}, err
		}
		result := state(p, nil)
		result.RequiresRestart = true
		return result, nil
	})
}

// Foundation uses the same per-user preferences service as defaults. The fixed
// domain/key cannot be supplied by callers; managed policy is checked again in
// the write process, and expected-value comparison guards concurrent changes.
const chromeScript = `ObjC.import('Foundation');
function run(argv) {
 var domain='com.google.Chrome', key='WebRtcIPHandling';
 var prefs=$.NSUserDefaults.alloc.initWithSuiteName(domain);
 function snapshot() {
  var raw=prefs.objectForKey(key), value=raw.isNil()?null:ObjC.unwrap(raw);
  return {value:value,forced:!!$.NSUserDefaults.standardUserDefaults.objectIsForcedForKeyInDomain(key,domain),valid:value===null || typeof value==='string'};
 }
 var current=snapshot();
 if (argv[0]==='write') {
  var expected=JSON.parse(argv[1]), target=JSON.parse(argv[2]);
  if (current.forced || !current.valid || current.value!==expected) return JSON.stringify(current);
  if (target===null) prefs.removeObjectForKey(key); else prefs.setObjectForKey(target,key);
  if (!prefs.synchronize) throw Error('Preferences could not be synchronized.');
  current=snapshot();
 }
 return JSON.stringify(current);
}`
