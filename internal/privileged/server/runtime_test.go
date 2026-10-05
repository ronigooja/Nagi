package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestRestrictedControl(t *testing.T) {
	allowed := []privileged.Request{
		{Method: "GET", Path: "/version"},
		{Method: "GET", Path: "/proxies/Group/delay?timeout=5000&url=https%3A%2F%2Fexample.com"},
		{Method: "PATCH", Path: "/configs", Body: []byte(`{"tun":{"enable":true,"auto-route":true,"auto-detect-interface":true}}`)},
		{Method: "PUT", Path: "/proxies/Group", Body: []byte(`{"name":"Node"}`)},
	}
	for _, req := range allowed {
		if err := validateControl(req); err != nil {
			t.Errorf("rejected %s %s: %v", req.Method, req.Path, err)
		}
	}
	denied := []privileged.Request{
		{Method: "PUT", Path: "/configs?force=true", Body: []byte(`{"path":"/etc/shadow"}`)},
		{Method: "PATCH", Path: "/configs", Body: []byte(`{"external-controller":"0.0.0.0:9090"}`)},
		{Method: "PATCH", Path: "/configs", Body: []byte(`{"tun":{"enable":true,"device":"/dev/evil"}}`)},
		{Method: "GET", Path: "/configs/../../etc/shadow"},
		{Method: "GET", Path: "/proxies/%2Fetc%2Fshadow"},
		{Method: "POST", Path: "/configs"},
	}
	for _, req := range denied {
		if err := validateControl(req); err == nil {
			t.Errorf("allowed %s %s", req.Method, req.Path)
		}
	}
}

func TestReadUserConfigRejectsSymlinksAndEscape(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profile.yaml")
	if err := os.WriteFile(path, []byte("mode: direct\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readUserConfig(root, path, os.Getuid())
	if err != nil || string(data) != "mode: direct\n" {
		t.Fatalf("safe profile: %q %v", data, err)
	}
	if _, err := readUserConfig(root, "/etc/passwd", os.Getuid()); err == nil {
		t.Fatal("accepted escaped path")
	}
	link := filepath.Join(root, "link.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readUserConfig(root, link, os.Getuid()); err == nil {
		t.Fatal("followed symlink")
	}
}
