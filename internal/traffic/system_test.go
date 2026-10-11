package traffic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestLinuxSystemProxyUnsupported(t *testing.T) {
	m := New(t.TempDir())
	m.OS = "linux"
	m.Run = func(_ context.Context, cmd string, args ...string) (string, error) {
		t.Fatalf("Linux system proxy must not invoke %s", cmd)
		return "", nil
	}
	_, err := m.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unsupported on Linux") {
		t.Fatalf("status error = %v", err)
	}
}
func TestMacServiceSnapshot(t *testing.T) {
	state := map[string]Setting{"Wi-Fi/web": {true, "old.local", 8888}, "Ethernet/web": {false, "another.local", 8000}}
	m := New(t.TempDir())
	m.OS = "darwin"
	m.Run = func(_ context.Context, cmd string, args ...string) (string, error) {
		if cmd != "networksetup" {
			return "", fmt.Errorf("unexpected %s", cmd)
		}
		if args[0] == "-listallnetworkservices" {
			return "An asterisk (*) denotes disabled service.\nWi-Fi\nEthernet", nil
		}
		kind := "web"
		if strings.Contains(args[0], "secure") {
			kind = "secure"
		}
		if strings.Contains(args[0], "socks") {
			kind = "socks"
		}
		key := args[1] + "/" + kind
		v := state[key]
		if strings.HasPrefix(args[0], "-get") {
			enabled := "No"
			if v.Enabled {
				enabled = "Yes"
			}
			return fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %d", enabled, v.Host, v.Port), nil
		}
		if strings.HasSuffix(args[0], "state") {
			v.Enabled = args[2] == "on"
		} else {
			v.Host = args[2]
			v.Port, _ = strconv.Atoi(args[3])
		}
		state[key] = v
		return "", nil
	}
	ctx := context.Background()
	_, e := m.Enable(ctx, os.Getpid(), 17890)
	if e != nil {
		t.Fatal(e)
	}
	if state["Ethernet/web"].Port != 17890 {
		t.Fatal("Ethernet not configured")
	}
	_, e = m.Disable(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if state["Ethernet/web"].Enabled || state["Ethernet/web"].Port != 8000 || state["Wi-Fi/web"].Port != 8888 {
		t.Fatalf("restored state: %+v", state)
	}
}

func TestMacRecheckOnlyKnownUnchangedServices(t *testing.T) {
	services := []string{"Wi-Fi", "Ethernet"}
	settings := map[string]Setting{}
	for _, service := range services {
		for _, kind := range []string{"web", "secure", "socks"} {
			settings[service+"/"+kind] = Setting{Host: "old.example", Port: 8080}
		}
	}
	m := New(t.TempDir())
	m.OS = "darwin"
	m.Run = func(_ context.Context, cmd string, args ...string) (string, error) {
		if cmd != "networksetup" {
			return "", fmt.Errorf("unexpected %s", cmd)
		}
		if args[0] == "-listallnetworkservices" {
			return strings.Join(services, "\n"), nil
		}
		kind := "web"
		if strings.Contains(args[0], "secure") {
			kind = "secure"
		} else if strings.Contains(args[0], "socks") {
			kind = "socks"
		}
		key := args[1] + "/" + kind
		value := settings[key]
		if strings.HasPrefix(args[0], "-get") {
			enabled := "No"
			if value.Enabled {
				enabled = "Yes"
			}
			return fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %d", enabled, value.Host, value.Port), nil
		}
		if strings.HasSuffix(args[0], "state") {
			value.Enabled = args[2] == "on"
		} else {
			value.Host = args[2]
			value.Port, _ = strconv.Atoi(args[3])
		}
		settings[key] = value
		return "", nil
	}
	ctx := context.Background()
	if _, err := m.Enable(ctx, os.Getpid(), 17890); err != nil {
		t.Fatal(err)
	}
	settings["Wi-Fi/web"] = Setting{Host: "old.example", Port: 8080}
	settings["Wi-Fi/secure"] = Setting{Host: "old.example", Port: 8080}
	settings["Ethernet/web"] = Setting{Enabled: true, Host: "other.example", Port: 9999}
	services = append(services, "New service")
	for _, kind := range []string{"web", "secure", "socks"} {
		settings["New service/"+kind] = Setting{Host: "new.example", Port: 9090}
	}
	result, err := m.Recheck(ctx, os.Getpid())
	if err != nil || result.Status != "attention" || len(result.Reasserted) != 1 || result.Reasserted[0] != "Wi-Fi" || len(result.Conflicts) != 1 || result.Conflicts[0] != "Ethernet" || len(result.Untracked) != 1 || result.Untracked[0] != "New service" {
		t.Fatalf("recheck: %+v %v", result, err)
	}
	if settings["Wi-Fi/web"].Port != 17890 || settings["Ethernet/web"].Port != 9999 || settings["New service/web"].Port != 9090 {
		t.Fatalf("unsafe change: %+v", settings)
	}
	services = []string{"Wi-Fi", "New service"}
	result, err = m.Recheck(ctx, os.Getpid())
	if err != nil || result.Status != "attention" || len(result.Missing) != 1 || result.Missing[0] != "Ethernet" {
		t.Fatalf("missing: %+v %v", result, err)
	}
}

func TestManagedPortSyncPreservesBaselineAndExternalEdits(t *testing.T) {
	settings := map[string]Setting{}
	for _, service := range []string{"Wi-Fi", "Ethernet"} {
		for _, kind := range []string{"web", "secure", "socks"} {
			settings[service+"/"+kind] = Setting{Host: "old.example", Port: 8080}
		}
	}
	m := New(t.TempDir())
	m.OS = "darwin"
	failNewPort := false
	m.Run = func(_ context.Context, cmd string, args ...string) (string, error) {
		if cmd != "networksetup" {
			return "", fmt.Errorf("unexpected %s", cmd)
		}
		if args[0] == "-listallnetworkservices" {
			return "Wi-Fi\nEthernet", nil
		}
		kind := "web"
		if strings.Contains(args[0], "secure") {
			kind = "secure"
		} else if strings.Contains(args[0], "socks") {
			kind = "socks"
		}
		key := args[1] + "/" + kind
		value := settings[key]
		if strings.HasPrefix(args[0], "-get") {
			enabled := "No"
			if value.Enabled {
				enabled = "Yes"
			}
			return fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %d", enabled, value.Host, value.Port), nil
		}
		if strings.HasSuffix(args[0], "state") {
			value.Enabled = args[2] == "on"
		} else {
			if failNewPort && args[1] == "Ethernet" && args[3] == "18000" {
				return "", errors.New("simulated networksetup failure")
			}
			value.Host = args[2]
			value.Port, _ = strconv.Atoi(args[3])
		}
		settings[key] = value
		return "", nil
	}
	ctx := context.Background()
	if _, err := m.Enable(ctx, os.Getpid(), 17890); err != nil {
		t.Fatal(err)
	}
	if err := m.SyncPort(ctx, 17900); err != nil {
		t.Fatal(err)
	}
	if settings["Wi-Fi/web"].Port != 17900 || settings["Ethernet/secure"].Port != 17900 {
		t.Fatalf("port not synchronized: %+v", settings)
	}
	j, err := m.readJournal()
	if err != nil || j.Before.PerService["Wi-Fi"].HTTP.Port != 8080 || j.Applied.PerService["Wi-Fi"].HTTP.Port != 17900 {
		t.Fatalf("journal: %+v %v", j, err)
	}
	settings["Ethernet/web"] = Setting{Enabled: true, Host: "external.example", Port: 9999}
	if err := m.SyncPort(ctx, 18000); err == nil || !strings.Contains(err.Error(), "externally") {
		t.Fatalf("expected conflict: %v", err)
	}
	if settings["Wi-Fi/web"].Port != 17900 || settings["Ethernet/web"].Port != 9999 {
		t.Fatalf("external setting overwritten: %+v", settings)
	}
	j, err = m.readJournal()
	if err != nil || j.Before.PerService["Wi-Fi"].HTTP.Port != 8080 || j.Applied.PerService["Wi-Fi"].HTTP.Port != 17900 {
		t.Fatalf("journal changed: %+v %v", j, err)
	}
	settings["Ethernet/web"] = Setting{Enabled: true, Host: "127.0.0.1", Port: 17900}
	failNewPort = true
	if err := m.SyncPort(ctx, 18000); err == nil {
		t.Fatal("expected partial OS write failure")
	}
	if settings["Wi-Fi/web"].Port != 17900 || settings["Wi-Fi/secure"].Port != 17900 {
		t.Fatalf("partial write was not compensated: %+v", settings)
	}
}
