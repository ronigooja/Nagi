package command

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
)

func TestCustomRulesFollowSelectedProfile(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n"))
	profiles := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
	for _, name := range []string{"work", "home"} {
		if err := profiles.Write(context.Background(), name, []byte(defaultProfile)); err != nil {
			t.Fatal(err)
		}
	}
	// A former global file is ignored, including when it is unreadable YAML.
	legacy := filepath.Join(paths.ConfigDir, "rules.yaml")
	if err := os.WriteFile(legacy, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) json.RawMessage {
		t.Helper()
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 0 {
			t.Fatalf("%v: exit=%d out=%s err=%s", args, code, out.String(), errs.String())
		}
		var result struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Data
	}
	custom := func(name string, enabled bool, count int) {
		t.Helper()
		var data struct {
			Profile string        `json:"profile"`
			Entries []rules.Entry `json:"entries"`
		}
		if err := json.Unmarshal(run("rules", "custom"), &data); err != nil {
			t.Fatal(err)
		}
		if data.Profile != name || len(data.Entries) != count || count > 0 && data.Entries[0].Enabled != enabled {
			t.Fatalf("custom rules: %+v", data)
		}
	}
	run("profile", "use", "work")
	added := run("rules", "add", "personal", "DOMAIN", "work.example", "DIRECT")
	if !bytes.Contains(added, []byte(`"profile":"work"`)) {
		t.Fatalf("mutation profile missing: %s", added)
	}
	custom("work", true, 1)
	run("profile", "use", "home")
	custom("home", false, 0)
	run("rules", "add", "personal", "DOMAIN", "home.example", "REJECT")
	run("rules", "disable", "personal")
	custom("home", false, 1)
	run("profile", "use", "work")
	custom("work", true, 1)
	if data, err := profiles.Effective("work"); err != nil || !bytes.Contains(data, []byte("work.example")) || bytes.Contains(data, []byte("home.example")) {
		t.Fatalf("work effective=%s err=%v", data, err)
	}
	run("profile", "remove", "home")
	if _, err := os.Stat(rules.Path(paths.ConfigDir, "home")); !os.IsNotExist(err) {
		t.Fatalf("removed profile rules remain: %v", err)
	}
	custom("work", true, 1)
	run("rules", "remove", "personal")
	custom("work", false, 0)
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "invalid: [" {
		t.Fatalf("legacy file changed: %s %v", data, err)
	}
}

func TestRuleCandidatesUseSelectedProfileWithoutEngine(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	for name, entry := range map[string]string{"work": "work-rule", "home": "home-rule"} {
		if err := rules.New(paths.ConfigDir, name, nil).Add(context.Background(), rules.Entry{Name: entry, Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "DIRECT"}); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"work": "work-rule\n", "home": "home-rule\n"} {
		if err := os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: "+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		if code := Run([]string{"completion", "candidates", "rules"}, &out, &errs, "test", "commit"); code != 0 || out.String() != want {
			t.Fatalf("%s: exit=%d out=%s err=%s", name, code, out.String(), errs.String())
		}
	}
}

func TestRuleChangesBashCompletion(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { [[ "$3" == rules ]] && printf '%s\n' work-rule home-rule; }
` + script + `
for action in remove enable disable; do
 COMP_WORDS=(nagi --json rules "$action" wo); COMP_CWORD=4; _nagi_complete; printf '%s:%s\n' "$action" "${COMPREPLY[*]}"
done
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("completion: %v %s", err, out)
	}
	for _, action := range []string{"remove", "enable", "disable"} {
		if !strings.Contains(string(out), action+":work-rule") {
			t.Fatalf("missing %s: %s", action, out)
		}
	}
}

func TestRuleTargetsOnlyUseCandidateProfile(t *testing.T) {
	dir := t.TempDir()
	if err := rules.New(dir, "other", nil).Add(context.Background(), rules.Entry{Name: "missing-target", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "Unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err := validateRuleTargets(dir, "work", []byte("rules: [MATCH,DIRECT]\n")); err != nil {
		t.Fatalf("other profile blocked candidate: %v", err)
	}
}
