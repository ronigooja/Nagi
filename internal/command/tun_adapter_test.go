package command

import (
	"errors"
	"net"
	"testing"
)

func TestTUNAdapterReport(t *testing.T) {
	interfaces := func() ([]net.Interface, error) {
		return []net.Interface{{Name: "Meta", Flags: net.FlagUp}, {Name: "Down"}}, nil
	}
	for _, tc := range []struct {
		name, want string
		tun        map[string]any
	}{
		{"disabled", "inactive", map[string]any{"enable": false, "device": "Meta"}},
		{"unnamed", "unknown", map[string]any{"enable": true}},
		{"up", "up", map[string]any{"enable": true, "device": "Meta"}},
		{"down", "down", map[string]any{"enable": true, "device": "Down"}},
		{"missing", "missing", map[string]any{"enable": true, "device": "Other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := tunAdapterReport(tc.tun, interfaces)
			if result["adapter_status"] != tc.want {
				t.Fatal(result)
			}
		})
	}
	result := tunAdapterReport(map[string]any{"enable": true, "device": "Meta"}, func() ([]net.Interface, error) {
		return nil, errors.New("unavailable")
	})
	if result["adapter_status"] != "unknown" {
		t.Fatal(result)
	}
}
