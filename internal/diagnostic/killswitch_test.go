package diagnostic

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func mockFirewall(t *testing.T) (*Firewall, *bool, *string) {
	t.Helper()
	active := false
	script := ""
	f := &Firewall{StateDir: t.TempDir(), UID: 1234, OS: "linux", Root: true}
	f.Run = func(_ context.Context, args []string, input []byte) ([]byte, error) {
		switch args[0] {
		case "-j":
			if !active {
				return []byte("No such file or directory"), errors.New("missing")
			}
			return []byte(`{"comment":"nagi-managed-v1"}`), nil
		case "-f":
			script = string(input)
			active = true
			return nil, nil
		case "delete":
			active = false
			return nil, nil
		}
		t.Fatalf("unexpected args %v", args)
		return nil, nil
	}
	return f, &active, &script
}
func TestKillSwitchTransactionAndCrashRecovery(t *testing.T) {
	f, active, script := mockFirewall(t)
	ctx := context.Background()
	if e := f.Enable(ctx, "lo", []string{"203.0.113.10:443", "[2001:db8::1]:853"}); e != nil {
		t.Fatal(e)
	}
	if !*active || !strings.Contains(*script, "meta skuid 1234 drop") || !strings.Contains(*script, "ip6 daddr 2001:db8::1 udp dport 853") {
		t.Fatalf("script: %s", *script)
	}
	status := f.Status(ctx)
	if !status.Enabled || status.AllowedEndpoints != 2 || status.TargetUID != 1234 {
		t.Fatalf("status %+v", status)
	}
	if _, e := os.Stat(f.journal()); e != nil {
		t.Fatal(e)
	}
	if e := f.Disable(ctx); e != nil {
		t.Fatal(e)
	}
	if *active {
		t.Fatal("table remains")
	}
	if _, e := os.Stat(f.journal()); !os.IsNotExist(e) {
		t.Fatalf("journal remains: %v", e)
	}
}
func TestKillSwitchFailedVerificationRollsBack(t *testing.T) {
	f, active, _ := mockFirewall(t)
	f.Run = func(_ context.Context, args []string, input []byte) ([]byte, error) {
		switch args[0] {
		case "-j":
			return []byte("No such file or directory"), errors.New("missing")
		case "-f":
			*active = true
			return nil, nil
		case "delete":
			*active = false
			return nil, nil
		}
		return nil, errors.New("unexpected")
	}
	if e := f.Enable(context.Background(), "lo", []string{"203.0.113.10:443"}); e == nil {
		t.Fatal("expected verification failure")
	}
	if *active {
		t.Fatal("rollback did not remove table")
	}
	if _, e := os.Stat(f.journal()); !os.IsNotExist(e) {
		t.Fatalf("journal remains: %v", e)
	}
}
func TestKillSwitchRejectsInvalidEndpoints(t *testing.T) {
	for _, items := range [][]string{{}, {"example.com:443"}, {"127.0.0.1:0"}, {"bad"}} {
		if _, e := parseEndpoints(items); e == nil {
			t.Fatalf("accepted %v", items)
		}
	}
}
