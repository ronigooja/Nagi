package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestHealthcheckFullURLReadForEditing(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	const testURL = "https://check.invalid/path%2Fcheck?token=placeholder&status=204"
	os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700)
	os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600)
	os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"), []byte(`proxy-providers:
  feed:
    type: http
    url: https://feed.invalid/subscription?token=feed-secret
    health-check:
      enable: true
      url: `+testURL+`
proxy-groups:
  - name: Auto
    type: url-test
    proxies: [DIRECT]
    url: `+testURL+"\n"), 0600)
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil)
	if err := subs.Add("saved", "https://feed.invalid/subscription?token=feed-secret"); err != nil {
		t.Fatal(err)
	}
	if err := subs.SetHealthcheckURL("saved", stringPointer(testURL)); err != nil {
		t.Fatal(err)
	}
	for _, target := range [][]string{{"provider", "feed"}, {"group", "Auto"}, {"subscription", "saved"}} {
		for _, full := range []bool{false, true} {
			args := append([]string{"--json", "proxy", "healthcheck", "show"}, target...)
			if full {
				args = append(args, "--full-url")
			}
			var out, errs bytes.Buffer
			if code := Run(args, &out, &errs, "test", "commit"); code != 0 {
				t.Fatal(args, code, errs.String())
			}
			var result struct {
				Data struct {
					TestURL string `json:"test_url"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			expected := "https://check.invalid"
			if full {
				expected = testURL
			}
			if result.Data.TestURL != expected || strings.Contains(out.String(), "feed-secret") || strings.Contains(out.String(), "FullTestURL") {
				t.Fatal(args, out.String())
			}
		}
	}
	var out, errs bytes.Buffer
	if code := Run([]string{"proxy", "healthcheck", "show", "subscription", "saved", "--full-url"}, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), testURL) {
		t.Fatal(code, out.String(), errs.String())
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("read started engine", err)
	}
}

func stringPointer(value string) *string { return &value }

func TestHealthcheckFullURLHelpAndInvalidOptionBeforeRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	var out, errs bytes.Buffer
	if code := Run([]string{"proxy", "healthcheck", "show", "--help"}, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "--full-url") {
		t.Fatal(code, out.String(), errs.String())
	}
	out.Reset()
	errs.Reset()
	if code := Run([]string{"--json", "proxy", "healthcheck", "show", "provider", "feed", "--unknown"}, &out, &errs, "test", "commit"); code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) {
		t.Fatal(code, errs.String())
	}
}
