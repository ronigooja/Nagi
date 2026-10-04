package command

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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

func TestCompletionIsRuntimeIndependent(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"--json", "completion", shell}, &stdout, &stderr, "test", "commit"); code != 0 {
			t.Fatalf("shell=%s exit=%d stderr=%s", shell, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), `"ok":true`) || !strings.Contains(stdout.String(), "nagi") {
			t.Fatalf("shell=%s response=%s", shell, stdout.String())
		}
	}
	bash, _ := completion("bash")
	if !strings.Contains(bash, "list use import export backup restore diff override remove") || !strings.Contains(bash, "compgen -f") {
		t.Fatalf("bash completion missing profile/file completion: %s", bash)
	}
	fish, _ := completion("fish")
	if !strings.Contains(fish, "rule global direct") {
		t.Fatalf("fish completion missing mode values: %s", fish)
	}
}

func TestBashCompletionPositionsAndSpaces(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file with space.yaml")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	script, _ := completion("bash")
	probe := script + "\n" + `COMP_WORDS=(nagi --json profile); COMP_CWORD=3; _nagi_complete; printf 'SUB:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi profile import work ` + filepath.Join(root, "file") + `); COMP_CWORD=4; _nagi_complete; printf 'FILE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi profile import ` + filepath.Join(root, "file") + `); COMP_CWORD=3; _nagi_complete; printf 'NAME:%s\n' "${COMPREPLY[*]}"
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("bash completion failed: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "SUB:list use import export backup restore diff override remove") || !strings.Contains(text, "FILE:"+file) || strings.Contains(text, "NAME:"+file) {
		t.Fatalf("unexpected completion output: %s", text)
	}
}

func TestProfileRemoveCLI(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	configDir := filepath.Join(configHome, "nagi")
	if err := os.MkdirAll(filepath.Join(configDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "profiles", "old.yaml"), []byte("mode: rule\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "profiles", "default.yaml"), []byte("mode: rule\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte("profile: default\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	t.Setenv("NAGI_MIHOMO_BIN", filepath.Join(root, "missing-mihomo"))
	for _, name := range []string{"bad/name", "missing"} {
		var invalidOut, invalidErr bytes.Buffer
		if code := Run([]string{"--json", "profile", "remove", name}, &invalidOut, &invalidErr, "test", "commit"); code != 1 || !strings.Contains(invalidErr.String(), "profile_error") {
			t.Fatalf("invalid/absent remove name=%s exit=%d stderr=%s", name, code, invalidErr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "profile", "remove", "old"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("remove exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(configDir, "profiles", "old.yaml")); !os.IsNotExist(err) {
		t.Fatalf("profile still exists, err=%v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "profile", "remove", "default"}, &stdout, &stderr, "test", "commit"); code != 1 || !strings.Contains(stderr.String(), "profile_error") {
		t.Fatalf("selected remove exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRecoveryCommandsIgnoreCorruptSelectedProfile(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config", "nagi")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "settings.yaml"), []byte("profile: bad/name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(config))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	t.Setenv("NAGI_MIHOMO_BIN", filepath.Join(root, "missing-mihomo"))
	for _, args := range [][]string{{"version"}, {"logs"}, {"stop"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "test", "commit"); code == 1 && strings.Contains(stderr.String(), "invalid selected profile") {
			t.Fatalf("%q was blocked by corrupt selection: %s", args, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "profile", "list"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("profile list exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "current_error") {
		t.Fatalf("profile list did not report current error: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "status"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "profile_error") || strings.Contains(stdout.String(), `"profile":"default"`) {
		t.Fatalf("status did not expose unavailable selection: %s", stdout.String())
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

func TestProxyDelayValidationPrecedesRuntime(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	for _, args := range [][]string{
		{"proxy", "delay", "Node", "ftp://example.test"},
		{"proxy", "delay", "Node", "https://user:pass@example.test"},
		{"proxy", "delay", "Node", "https://example.test", "30001"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "test", "commit"); code != 2 {
			t.Fatalf("args=%q exit=%d stderr=%s", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "Usage: nagi proxy delay NODE") {
			t.Fatalf("args=%q stderr=%s", args, stderr.String())
		}
	}
}

func TestConnectionsCloseRejectsEmptyIDBeforeRuntime(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"connections", "close", ""}, &stdout, &stderr, "test", "commit"); code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "connection ID required") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestRulesHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{{"rules", "add"}, {"rules", "import-local"}, {"rules", "connection"}} {
		var out, errs bytes.Buffer
		if code := Run(append(args, "--help"), &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "Usage: nagi "+strings.Join(args, " ")) {
			t.Fatalf("help %v: %d %s %s", args, code, out.String(), errs.String())
		}
	}
	for _, args := range [][]string{{"--json", "rules", "add", "x"}, {"--json", "rules", "import-remote", "a", "bad", "https://example.com/set", "DIRECT"}, {"--json", "rules", "import-remote", "a", "domain", "file:///tmp/set", "DIRECT"}} {
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "commit"); code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) {
			t.Fatalf("usage %v: %d %s", args, code, errs.String())
		}
	}
}
