package traffic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGnomeEnableRestore(t *testing.T) {
	values := map[string]string{"org.gnome.system.proxy/mode": "'auto'"}
	for _, name := range []string{"http", "https", "socks"} {
		schema := "org.gnome.system.proxy." + name
		values[schema+"/host"] = "'old.example'"
		values[schema+"/port"] = "8080"
	}
	m := New(t.TempDir())
	m.OS = "linux"
	m.Run = func(_ context.Context, cmd string, args ...string) (string, error) {
		if cmd != "gsettings" {
			return "", fmt.Errorf("unexpected %s", cmd)
		}
		key := args[1] + "/" + args[2]
		switch args[0] {
		case "get":
			return values[key], nil
		case "set":
			values[key] = args[3]
			return "", nil
		}
		return "", fmt.Errorf("unexpected %v", args)
	}
	ctx := context.Background()
	before, e := m.Status(ctx)
	if e != nil || before.Enabled || before.Mode != "auto" {
		t.Fatalf("before: %+v, %v", before, e)
	}
	enabled, e := m.Enable(ctx, os.Getpid(), 17890)
	if e != nil || !enabled.Enabled || enabled.HTTP.Port != 17890 {
		t.Fatalf("enable: %+v, %v", enabled, e)
	}
	data, e := os.ReadFile(filepath.Join(m.Dir, "system-proxy-restore.json"))
	if e != nil || !strings.Contains(string(data), "auto") || !strings.Contains(string(data), "\"applied\"") {
		t.Fatalf("journal: %s, %v", data, e)
	}
	restored, e := m.Disable(ctx)
	if e != nil || restored.Mode != "auto" || restored.Enabled {
		t.Fatalf("restore: %+v, %v", restored, e)
	}
	if _, e = os.Stat(filepath.Join(m.Dir, "system-proxy-restore.json")); !os.IsNotExist(e) {
		t.Fatalf("journal remains: %v", e)
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

func TestGnomeRecheckRestoresOnlySavedBaseline(t *testing.T) {
	values := map[string]string{"org.gnome.system.proxy/mode": "'auto'"}
	for _, name := range []string{"http", "https", "socks"} {
		schema := "org.gnome.system.proxy." + name
		values[schema+"/host"] = "'old.example'"
		values[schema+"/port"] = "8080"
	}
	m := New(t.TempDir())
	m.OS = "linux"
	m.Run = func(_ context.Context, cmd string, args ...string) (string, error) {
		if cmd != "gsettings" {
			return "", fmt.Errorf("unexpected %s", cmd)
		}
		key := args[1] + "/" + args[2]
		if args[0] == "get" {
			return values[key], nil
		}
		values[key] = args[3]
		return "", nil
	}
	ctx := context.Background()
	before, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enable(ctx, os.Getpid(), 17890); err != nil {
		t.Fatal(err)
	}
	result, err := m.Recheck(ctx, os.Getpid())
	if err != nil || result.Status != "current" {
		t.Fatalf("current: %+v %v", result, err)
	}
	if err := m.gnomeApply(ctx, before); err != nil {
		t.Fatal(err)
	}
	result, err = m.Recheck(ctx, os.Getpid())
	if err != nil || result.Status != "reasserted" || len(result.Reasserted) != 1 || values["org.gnome.system.proxy/mode"] != "manual" {
		t.Fatalf("reasserted: %+v %v values=%v", result, err, values)
	}
	values["org.gnome.system.proxy.http/host"] = "'other.example'"
	result, err = m.Recheck(ctx, os.Getpid())
	if err != nil || result.Status != "attention" || len(result.Conflicts) == 0 || values["org.gnome.system.proxy.http/host"] != "'other.example'" {
		t.Fatalf("conflict: %+v %v values=%v", result, err, values)
	}
	result, err = m.Recheck(ctx, os.Getpid()+10000)
	if err != nil || result.Status != "stale_journal" || values["org.gnome.system.proxy.http/host"] != "'other.example'" {
		t.Fatalf("stale: %+v %v", result, err)
	}
	j, err := m.readJournal()
	if err != nil {
		t.Fatal(err)
	}
	j.Applied = State{}
	if err := m.writeJournal(j); err != nil {
		t.Fatal(err)
	}
	result, err = m.Recheck(ctx, os.Getpid())
	if err != nil || result.Status != "legacy_journal" || values["org.gnome.system.proxy.http/host"] != "'other.example'" {
		t.Fatalf("legacy: %+v %v", result, err)
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
