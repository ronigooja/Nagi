package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONContractAndExitCodes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "status"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
	}
	var success struct {
		OK   bool `json:"ok"`
		Data struct {
			Running bool `json:"running"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &success); err != nil || !success.OK || success.Data.Running {
		t.Fatalf("status response=%s err=%v", stdout.String(), err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "unknown"}, &stdout, &stderr, "test", "commit"); code != 2 {
		t.Fatalf("usage exit=%d", code)
	}
	var failure struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.OK || failure.Error.Code != "usage" || failure.Error.Message == "" {
		t.Fatalf("error response=%s err=%v", stderr.String(), err)
	}
}

func TestHelpDoesNotReadRuntimeConfiguration(t *testing.T) {
	root := t.TempDir()
	// A file where a config directory is expected makes runtime resolution fail.
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"status", "--help"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("help exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: nagi status") {
		t.Fatalf("unexpected help: %s", stdout.String())
	}
}

func TestNoArgsPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Commands:") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestEveryCommandHasHelpWithoutRuntime(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	t.Setenv("NAGI_MIHOMO_BIN", filepath.Join(root, "missing-mihomo"))
	for _, spec := range commandSpecs {
		t.Run(spec.path, func(t *testing.T) {
			for _, args := range [][]string{
				append([]string{"help"}, strings.Fields(spec.path)...),
				append(strings.Fields(spec.path), "--help"),
			} {
				var stdout, stderr bytes.Buffer
				if code := Run(args, &stdout, &stderr, "test", "commit"); code != 0 {
					t.Fatalf("args=%q exit=%d stderr=%s", args, code, stderr.String())
				}
				if !strings.Contains(stdout.String(), "Usage: nagi "+spec.syntax) || !strings.Contains(stdout.String(), "Example:") {
					t.Fatalf("args=%q help=%s", args, stdout.String())
				}
			}
		})
	}
}

func TestInvalidHelpPathIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"help", "missing"}, {"help", "service", "missing"},
		{"service", "missing", "--help"}, {"profile", "use", "work", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "test", "commit"); code != 2 {
			t.Fatalf("args=%q exit=%d", args, code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage: nagi help") {
			t.Fatalf("args=%q stdout=%s stderr=%s", args, stdout.String(), stderr.String())
		}
	}
}

func TestArgumentErrorsPrecedeRuntimeAndShowSyntax(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	cases := []struct {
		args, want []string
	}{
		{[]string{"start", "extra"}, []string{"too many arguments", "Usage: nagi start"}},
		{[]string{"config"}, []string{"missing subcommand", "Usage: nagi config <validate|show>"}},
		{[]string{"service", "invalid"}, []string{"unknown subcommand", "Usage: nagi service <install|uninstall>"}},
		{[]string{"profile", "use"}, []string{"missing argument", "Usage: nagi profile use NAME", "nagi profile list"}},
		{[]string{"subscription", "add", "work"}, []string{"missing argument", "Usage: nagi subscription add NAME URL"}},
		{[]string{"proxy", "select", "group"}, []string{"missing argument", "Usage: nagi proxy select GROUP NODE", "nagi proxy groups"}},
		{[]string{"logs", "12junk"}, []string{"lines must be an integer", "Usage: nagi logs [lines]"}},
		{[]string{"logs", "0"}, []string{"lines must be an integer", "Usage: nagi logs [lines]"}},
		{[]string{"connections", "list", "extra"}, []string{"too many arguments", "Usage: nagi connections list"}},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		if code := Run(tc.args, &stdout, &stderr, "test", "commit"); code != 2 {
			t.Fatalf("args=%q exit=%d stderr=%s", tc.args, code, stderr.String())
		}
		for _, want := range tc.want {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("args=%q missing %q: %s", tc.args, want, stderr.String())
			}
		}
	}
}

func TestInvalidNamesAndURLKeepErrorContract(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	secret := "ftp://example.com/private?token=secret"
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"profile", "use", "bad/name"}, "profile_error"},
		{[]string{"subscription", "add", "bad/name", "https://example.com"}, "subscription_error"},
		{[]string{"subscription", "add", "work", secret}, "subscription_error"},
	} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"--json"}, tc.args...)
		if code := Run(args, &stdout, &stderr, "test", "commit"); code != 1 {
			t.Fatalf("args=%q exit=%d stderr=%s", tc.args, code, stderr.String())
		}
		var failure struct {
			OK    bool `json:"ok"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.OK || failure.Error.Code != tc.code || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
			t.Fatalf("args=%q stdout=%s stderr=%s err=%v", tc.args, stdout.String(), stderr.String(), err)
		}
	}
}

func TestUsageErrorDoesNotEchoSubscriptionURL(t *testing.T) {
	secret := "https://example.com/private?token=secret"
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "subscription", "add", "work", secret, "extra"}, &stdout, &stderr, "test", "commit"); code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), secret) || !strings.Contains(stderr.String(), `"code":"usage"`) {
		t.Fatalf("unexpected error: %s", stderr.String())
	}
}
