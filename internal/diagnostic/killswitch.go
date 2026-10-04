package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type nftRunner func(context.Context, []string, []byte) ([]byte, error)
type Firewall struct {
	StateDir string
	UID      int
	OS       string
	Run      nftRunner
	Root     bool
}
type firewallJournal struct {
	Version   int      `json:"version"`
	UID       int      `json:"uid"`
	Interface string   `json:"interface"`
	Endpoints []string `json:"endpoints"`
}

func newFirewall(dir string) *Firewall {
	uid := os.Getuid()
	if sudo := os.Getenv("SUDO_UID"); sudo != "" {
		if n, e := strconv.Atoi(sudo); e == nil && n >= 0 {
			uid = n
		}
	}
	return &Firewall{StateDir: dir, UID: uid, OS: runtime.GOOS, Root: os.Geteuid() == 0}
}
func nftCommand(ctx context.Context, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "nft", args...)
	cmd.Stdin = bytes.NewReader(input)
	return cmd.CombinedOutput()
}
func (f *Firewall) table() string   { return fmt.Sprintf("nagi_killswitch_%d", f.UID) }
func (f *Firewall) journal() string { return filepath.Join(f.StateDir, "kill-switch.json") }
func (f *Firewall) backend() error {
	if f.OS != "linux" {
		return fmt.Errorf("kill switch is unsupported on %s; Linux nftables is required", f.OS)
	}
	if _, e := exec.LookPath("nft"); e != nil && f.Run == nil {
		return errors.New("nft executable is unavailable; install nftables")
	}
	return nil
}
func (f *Firewall) run(ctx context.Context, args []string, input []byte) ([]byte, error) {
	runner := f.Run
	if runner == nil {
		runner = nftCommand
	}
	return runner(ctx, args, input)
}
func (f *Firewall) installed(ctx context.Context) (bool, bool, error) {
	out, e := f.run(ctx, []string{"-j", "list", "table", "inet", f.table()}, nil)
	if e != nil {
		if bytes.Contains(out, []byte("No such file or directory")) || bytes.Contains(out, []byte("does not exist")) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("inspect nftables table: %w: %s", e, strings.TrimSpace(string(out)))
	}
	return true, bytes.Contains(out, []byte("nagi-managed-v1")), nil
}
func (f *Firewall) Status(ctx context.Context) KillSwitchState {
	if e := f.backend(); e != nil {
		return KillSwitchState{Supported: false, Message: e.Error()}
	}
	state := KillSwitchState{Supported: true, Backend: "nftables", TargetUID: f.UID}
	exists, owned, e := f.installed(ctx)
	if e != nil {
		state.Message = e.Error()
		return state
	}
	state.Enabled = exists && owned
	if exists && !owned {
		state.Message = "nftables table name conflicts with an unmanaged table; Nagi will not modify it"
		return state
	}
	journalData, journalErr := os.ReadFile(f.journal())
	if journalErr == nil {
		var j firewallJournal
		if json.Unmarshal(journalData, &j) == nil && j.Version == 1 {
			state.Interface = j.Interface
			state.AllowedEndpoints = len(j.Endpoints)
		}
	}
	if state.Enabled {
		if journalErr == nil {
			state.Message = "Active: only loopback, the selected TUN interface, and listed upstream endpoints are allowed for the target UID."
		} else {
			state.Message = "Active but restore journal is missing; inspect nftables table before disabling."
		}
	} else if journalErr == nil {
		state.Message = "Restore journal exists but firewall table is absent; run kill-switch disable to clear stale state."
	} else {
		state.Message = "Disabled."
	}
	return state
}
func KillSwitchStatus(stateDir string) KillSwitchState {
	return newFirewall(stateDir).Status(context.Background())
}
func validInterface(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
func parseEndpoints(items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, errors.New("at least one numeric upstream IP:PORT endpoint is required; include proxy and DNS upstreams")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		host, portText, e := net.SplitHostPort(item)
		if e != nil {
			return nil, fmt.Errorf("invalid endpoint %q; use IP:PORT (brackets for IPv6)", item)
		}
		ip := net.ParseIP(host)
		port, e := strconv.Atoi(portText)
		if ip == nil || e != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid endpoint %q; numeric IP and port 1..65535 required", item)
		}
		out = append(out, net.JoinHostPort(ip.String(), portText))
	}
	return out, nil
}
func (f *Firewall) script(iface string, endpoints []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\nadd chain inet %s output { type filter hook output priority -100; policy accept; }\n", f.table(), f.table())
	fmt.Fprintf(&b, "add rule inet %s output meta skuid %d oifname \"lo\" accept comment \"nagi-managed-v1\"\n", f.table(), f.UID)
	fmt.Fprintf(&b, "add rule inet %s output meta skuid %d oifname \"%s\" accept\n", f.table(), f.UID, iface)
	for _, endpoint := range endpoints {
		host, port, _ := net.SplitHostPort(endpoint)
		family := "ip"
		if net.ParseIP(host).To4() == nil {
			family = "ip6"
		}
		for _, proto := range []string{"tcp", "udp"} {
			fmt.Fprintf(&b, "add rule inet %s output meta skuid %d %s daddr %s %s dport %s accept\n", f.table(), f.UID, family, host, proto, port)
		}
	}
	fmt.Fprintf(&b, "add rule inet %s output meta skuid %d drop\n", f.table(), f.UID)
	return b.String()
}
func (f *Firewall) writeJournal(j firewallJournal) error {
	if e := os.MkdirAll(f.StateDir, 0700); e != nil {
		return e
	}
	data, e := json.Marshal(j)
	if e != nil {
		return e
	}
	file, e := os.CreateTemp(f.StateDir, ".kill-switch-*")
	if e != nil {
		return e
	}
	defer os.Remove(file.Name())
	if e = file.Chmod(0600); e != nil {
		file.Close()
		return e
	}
	if _, e = file.Write(data); e != nil {
		file.Close()
		return e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	return os.Rename(file.Name(), f.journal())
}
func (f *Firewall) Enable(ctx context.Context, iface string, rawEndpoints []string) error {
	if e := f.backend(); e != nil {
		return e
	}
	if !f.Root {
		return errors.New("kill switch requires root; use sudo and preserve SUDO_UID to target your user")
	}
	if !validInterface(iface) {
		return errors.New("TUN interface must be an existing interface name of at most 15 letters, digits, _, -, or .")
	}
	if _, e := net.InterfaceByName(iface); e != nil {
		return fmt.Errorf("TUN interface %s is unavailable; enable TUN first: %w", iface, e)
	}
	endpoints, e := parseEndpoints(rawEndpoints)
	if e != nil {
		return e
	}
	exists, owned, e := f.installed(ctx)
	if e != nil {
		return e
	}
	if exists {
		if owned {
			return errors.New("kill switch is already enabled; run `kill-switch status`")
		}
		return errors.New("nftables table name is already in use by an unmanaged table")
	}
	if _, e = os.Stat(f.journal()); e == nil {
		return errors.New("stale kill-switch journal exists; run `kill-switch disable` before enabling")
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e = f.writeJournal(firewallJournal{1, f.UID, iface, endpoints}); e != nil {
		return e
	}
	out, e := f.run(ctx, []string{"-f", "-"}, []byte(f.script(iface, endpoints)))
	if e != nil {
		_ = os.Remove(f.journal())
		return fmt.Errorf("install nftables kill switch: %w: %s", e, strings.TrimSpace(string(out)))
	}
	exists, owned, e = f.installed(ctx)
	if e != nil || !exists || !owned {
		rollbackErr := f.removeTable(ctx)
		if rollbackErr == nil {
			_ = os.Remove(f.journal())
		}
		return fmt.Errorf("could not verify installed kill switch (exists=%t owned=%t): %v; rollback: %v", exists, owned, e, rollbackErr)
	}
	return nil
}
func (f *Firewall) removeTable(ctx context.Context) error {
	out, e := f.run(ctx, []string{"delete", "table", "inet", f.table()}, nil)
	if e != nil {
		return fmt.Errorf("remove nftables table: %w: %s", e, strings.TrimSpace(string(out)))
	}
	return nil
}
func (f *Firewall) Disable(ctx context.Context) error {
	if e := f.backend(); e != nil {
		return e
	}
	if !f.Root {
		return errors.New("kill switch disable requires root; use sudo with the same SUDO_UID")
	}
	data, e := os.ReadFile(f.journal())
	if errors.Is(e, os.ErrNotExist) {
		return errors.New("Nagi kill-switch journal is missing; inspect the nftables table before changing firewall rules")
	}
	if e != nil {
		return e
	}
	var j firewallJournal
	if e = json.Unmarshal(data, &j); e != nil || j.Version != 1 || j.UID != f.UID {
		return errors.New("kill-switch journal is invalid or belongs to a different UID; no firewall rules were changed")
	}
	exists, owned, e := f.installed(ctx)
	if e != nil {
		return e
	}
	if exists && !owned {
		return errors.New("nftables table is not Nagi-managed; no firewall rules were changed")
	}
	if exists {
		if e = f.removeTable(ctx); e != nil {
			return e
		}
	}
	return os.Remove(f.journal())
}
func EnableKillSwitch(ctx context.Context, stateDir, iface string, endpoints []string) error {
	return newFirewall(stateDir).Enable(ctx, iface, endpoints)
}
func DisableKillSwitch(ctx context.Context, stateDir string) error {
	return newFirewall(stateDir).Disable(ctx)
}
