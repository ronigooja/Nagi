package traffic

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type fakeDNS struct {
	values      map[string][]string
	fail        string
	writes      int
	beforeWrite func()
}

func (f *fakeDNS) run(_ context.Context, name string, args ...string) (string, error) {
	if name != "/usr/sbin/networksetup" {
		return "", errors.New("nonfixed executable")
	}
	switch args[0] {
	case "-listallnetworkservices":
		var names []string
		for n := range f.values {
			names = append(names, n)
		}
		return "An asterisk (*) denotes disabled services.\n" + strings.Join(names, "\n"), nil
	case "-getdnsservers":
		v, ok := f.values[args[1]]
		if !ok {
			return "", errors.New("missing service")
		}
		if len(v) == 0 {
			return "There aren't any DNS Servers set on " + args[1] + ".", nil
		}
		return strings.Join(v, "\n"), nil
	case "-setdnsservers":
		f.writes++
		if f.beforeWrite != nil {
			f.beforeWrite()
		}
		if f.fail == args[1] {
			return "", errors.New("permission denied")
		}
		v := args[2:]
		if len(v) == 1 && v[0] == "Empty" {
			v = nil
		}
		f.values[args[1]] = append([]string{}, v...)
		return "", nil
	}
	return "", errors.New("unexpected command")
}
func dnsFixture(t *testing.T) (*DNSManager, *fakeDNS) {
	t.Helper()
	f := &fakeDNS{values: map[string][]string{"Wi-Fi": nil, "Ethernet": {"1.1.1.1", "2606:4700:4700::1111"}}}
	m := NewDNS(t.TempDir())
	m.OS = "darwin"
	m.Run = f.run
	m.Alive = func(int) bool { return true }
	return m, f
}
func TestDNSTargets(t *testing.T) {
	target, err := DNSTargets(map[string]any{"inet4-address": []any{"198.19.0.1/30"}, "inet6-address": []string{"fd12::1/126"}})
	if err != nil || !dnsEqual(target, []string{"198.19.0.2", "fd12::2"}) {
		t.Fatalf("%v %v", target, err)
	}
	for _, v := range []any{nil, "198.19.0.1/30", []any{true}, []string{"198.19.0.1/32"}, []string{"fd12::1/126"}} {
		if _, err := DNSTargets(map[string]any{"inet4-address": v}); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
}
func TestDNSLifecycleAndAddressSync(t *testing.T) {
	m, f := dnsFixture(t)
	ctx := context.Background()
	target := []string{"198.19.0.2"}
	f.beforeWrite = func() {
		j, err := m.read()
		if err != nil || len(j.Services) != 2 {
			t.Fatalf("write occurred before journal: %v", err)
		}
	}
	if _, err := m.Enable(ctx, 123, target); err != nil {
		t.Fatal(err)
	}
	initial := f.writes
	if _, err := m.Enable(ctx, 123, target); err != nil {
		t.Fatal(err)
	}
	if f.writes != initial {
		t.Fatal("repeated enable wrote DNS")
	}
	f.beforeWrite = nil
	f.values["USB LAN"] = []string{"8.8.8.8"}
	if _, err := m.Sync(ctx, 123, []string{"198.20.0.2", "fd12::2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.values["Wi-Fi"]) != 0 || !dnsEqual(f.values["Ethernet"], []string{"1.1.1.1", "2606:4700:4700::1111"}) || !dnsEqual(f.values["USB LAN"], []string{"8.8.8.8"}) || m.Managed() {
		t.Fatalf("baseline not restored: %#v", f.values)
	}
}
func TestDNSFailureRollsBackAndRetainsFailedRestore(t *testing.T) {
	m, f := dnsFixture(t)
	ctx := context.Background()
	f.fail = "Ethernet"
	if _, err := m.Sync(ctx, 1, []string{"198.19.0.2"}); err == nil {
		t.Fatal("ignored failure")
	}
	if m.Managed() || len(f.values["Wi-Fi"]) != 0 {
		t.Fatal("failed enable did not roll back")
	}
	f.fail = ""
	if _, err := m.Sync(ctx, 1, []string{"198.19.0.2"}); err != nil {
		t.Fatal(err)
	}
	f.fail = "Ethernet"
	if _, err := m.Disable(ctx); err == nil || !m.Managed() {
		t.Fatal("failed restore discarded journal")
	}
	f.fail = ""
	if _, err := m.Disable(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestDNSConflictIsReadOnlyAndRetainsRecovery(t *testing.T) {
	m, f := dnsFixture(t)
	ctx := context.Background()
	if _, err := m.Sync(ctx, 1, []string{"198.19.0.2"}); err != nil {
		t.Fatal(err)
	}
	f.values["Wi-Fi"] = []string{"9.9.9.9"}
	writes := f.writes
	state, err := m.Status(ctx)
	if err != nil || state.Status != "attention" || len(state.Conflicts) != 1 || f.writes != writes {
		t.Fatalf("status %#v %v", state, err)
	}
	if _, err = m.Sync(ctx, 1, []string{"198.20.0.2"}); err == nil || f.writes != writes {
		t.Fatal("sync overwrote external edit")
	}
	if _, err = m.Disable(ctx); err == nil || !m.Managed() || !dnsEqual(f.values["Wi-Fi"], []string{"9.9.9.9"}) {
		t.Fatal("restore ignored conflict")
	}
	// Restoring the service manually to the recorded baseline makes recovery retryable.
	f.values["Wi-Fi"] = nil
	if _, err = m.Disable(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestDNSInterruptedTransactionRecovery(t *testing.T) {
	m, f := dnsFixture(t)
	ctx := context.Background()
	j := dnsJournal{PID: 123, Services: map[string]dnsEntry{"Wi-Fi": {Before: nil, Applied: []string{"198.19.0.2"}, Pending: []string{"198.20.0.2"}}}}
	if err := m.save(j); err != nil {
		t.Fatal(err)
	}
	f.values["Wi-Fi"] = j.Services["Wi-Fi"].Pending
	m.Alive = func(int) bool { return false }
	if err := m.RestoreIfStopped(ctx, 123); err != nil {
		t.Fatal(err)
	}
	if m.Managed() || len(f.values["Wi-Fi"]) != 0 {
		t.Fatal("interrupted update was not restored")
	}
}
func TestDNSMissingServiceAndCorruptJournalFail(t *testing.T) {
	m, f := dnsFixture(t)
	ctx := context.Background()
	if _, err := m.Sync(ctx, 1, []string{"198.19.0.2"}); err != nil {
		t.Fatal(err)
	}
	delete(f.values, "Ethernet")
	if _, err := m.Disable(ctx); err == nil || !m.Managed() {
		t.Fatal("missing service silently dropped")
	}
	if err := os.WriteFile(m.path(), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Disable(ctx); err == nil {
		t.Fatal("corrupt journal accepted")
	}
}
func TestDNSNonDarwinNoop(t *testing.T) {
	m := NewDNS(t.TempDir())
	m.OS = "linux"
	m.Run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("executed OS command")
		return "", nil
	}
	if _, err := m.Sync(context.Background(), 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.RestoreIfStopped(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}
