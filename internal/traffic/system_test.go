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
	if e != nil || !strings.Contains(string(data), "auto") {
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
