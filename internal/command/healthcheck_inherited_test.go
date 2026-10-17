package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestEditingInheritedValuesAreReadOnlyAndRespectPolicyLayers(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	profilePath := filepath.Join(paths.ConfigDir, "profiles", "work.yaml")
	os.MkdirAll(filepath.Dir(profilePath), 0700)
	source := []byte(`proxy-providers:
  feed:
    type: http
    url: https://feed.invalid/nodes
    health-check:
      enable: true
      interval: 450
      url: https://source.invalid/full?status=204
proxy-groups:
  - name: Auto
    type: url-test
    proxies: [DIRECT]
    interval: 450
    url: https://source.invalid/full?status=204
`)
	os.WriteFile(profilePath, source, 0600)
	os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600)
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil)
	if err := subs.Add("saved", "https://feed.invalid/subscription"); err != nil {
		t.Fatal(err)
	}
	read := func(kind, name string) map[string]any {
		t.Helper()
		var out, errs bytes.Buffer
		if code := Run([]string{"--json", "proxy", "healthcheck", "show", kind, name, "--full-url"}, &out, &errs, "test", "commit"); code != 0 {
			t.Fatal(code, errs.String())
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	if r := read("subscription", "saved"); r["inherited_url_status"] != "not_applied" || r["inherited_test_url"] != nil {
		t.Fatal(r)
	}
	if err := subs.StageProfileAssociation("saved", "work"); err != nil {
		t.Fatal(err)
	}
	seconds := 600
	policyURL := "https://policy.invalid/path?status=204"
	if err := subs.SetHealthcheckInterval("saved", &seconds); err != nil {
		t.Fatal(err)
	}
	if err := subs.SetHealthcheckURL("saved", &policyURL); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(paths.ConfigDir, "subscriptions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := read("subscription", "saved")
	if r["interval_seconds"] != float64(600) || r["inherited_interval_seconds"] != float64(450) || r["inherited_test_url"] != "https://source.invalid/full?status=204" {
		t.Fatal(r)
	}
	overridePath := filepath.Join(paths.ConfigDir, "overrides", "work.yaml")
	os.MkdirAll(filepath.Dir(overridePath), 0700)
	override := []byte("proxy-providers:\n  feed:\n    health-check:\n      interval: 900\n      url: https://local.invalid/full?status=200\n")
	os.WriteFile(overridePath, override, 0600)
	r = read("provider", "feed")
	if r["interval_seconds"] != float64(900) || r["inherited_interval_seconds"] != float64(600) || r["inherited_test_url"] != policyURL {
		t.Fatal(r)
	}
	r = read("subscription", "saved")
	if r["inherited_interval_status"] != "mixed" || r["inherited_url_status"] != "mixed" || r["inherited_interval_seconds"] != nil || r["inherited_test_url"] != nil {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(filepath.Join(paths.ConfigDir, "subscriptions.yaml"))
	diskOverride, _ := os.ReadFile(overridePath)
	diskSource, _ := os.ReadFile(profilePath)
	if !bytes.Equal(before, after) || !bytes.Equal(override, diskOverride) || !bytes.Equal(source, diskSource) {
		t.Fatal("preview changed persisted configuration")
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("preview started engine", err)
	}
}
