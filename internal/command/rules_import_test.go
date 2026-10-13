package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/rules"
)

func TestRuleImportPreflight(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{
		{"rules", "import"},
		{"rules", "import", "work"},
		{"rules", "import", "", "./rules.yaml"},
		{"rules", "import", "bad/name", "./rules.yaml"},
		{"rules", "import", strings.Repeat("a", 129), "./rules.yaml"},
		{"rules", "import", "work", ""},
		{"rules", "import", "work", " "},
		{"rules", "import", "work", "rules", "./rules.yaml"},
		{"rules", "import", "work", "domain", "./rules.yaml", "DIRECT"},
		{"rules", "import", "work", "classical", "./rules.yaml", "DIRECT"},
		{"rules", "import", "work", "ipcidr", "./rules.yaml", "DIRECT"},
		{"rules", "import", "work", "./rules.yaml", "DIRECT"},
		{"rules", "import", "work", "file:///PRIVATE"},
		{"rules", "import", "work", "ftp://example.com/PRIVATE"},
		{"rules", "import", "work", "https://user:PRIVATE@example.com/feed"},
		{"rules", "import", "work", "https:///PRIVATE"},
		{"rules", "import", "work", "http:PRIVATE"},
		{"rules", "import", "work", "mailto:PRIVATE@example.com"},
		{"rules", "import", "work", "//example.com/PRIVATE"},
		{"rules", "import", "work", "https://%PRIVATE"},
	} {
		for _, jsonMode := range []bool{false, true} {
			invocation := append([]string(nil), args...)
			if jsonMode {
				invocation = append([]string{"--json"}, invocation...)
			}
			var out, errs bytes.Buffer
			if code := Run(invocation, &out, &errs, "test", "commit"); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "Usage: nagi rules import NAME SOURCE") || strings.Contains(errs.String(), "PRIVATE") {
				t.Fatalf("args=%q json=%v exit=%d out=%s err=%s", args, jsonMode, code, out.String(), errs.String())
			}
			if jsonMode {
				var envelope struct {
					OK    bool `json:"ok"`
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(errs.Bytes(), &envelope); err != nil || envelope.OK || envelope.Error.Code != "usage" {
					t.Fatalf("error contract: %s (%v)", errs.String(), err)
				}
			}
		}
	}
	for _, args := range [][]string{
		{"rules", "import", "work", "./file with space.yaml"},
		{"rules", "import", "work", "/tmp/rules.yaml"},
		{"rules", "import", "work", "./a:b.yaml"},
		{"rules", "import", "work", "/tmp/a:b.yaml"},
		{"rules", "import", "work", "https://example.com/feed?token=PRIVATE"},
		{"rules", "import", "work", "http://example.com/feed"},
	} {
		if err := validateInvocation(args); err != nil {
			t.Fatalf("valid args=%q: %v", args, err)
		}
	}
}

func TestRuleImportHelpAndRemovedCommands(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{{"rules", "import", "--help"}, {"help", "rules", "import"}, {"--json", "rules", "import", "--help"}} {
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "commit"); code != 0 || errs.Len() != 0 || !strings.Contains(out.String(), "rules import NAME SOURCE") || !strings.Contains(out.String(), "own targets") || strings.Contains(out.String(), "FORMAT") {
			t.Fatalf("help %q code=%d out=%s err=%s", args, code, out.String(), errs.String())
		}
	}
	for _, command := range []string{"import-local", "import-remote"} {
		for _, args := range [][]string{{"rules", command, "work", "domain", "./set.yaml", "DIRECT"}, {"rules", command, "--help"}, {"help", "rules", command}} {
			var out, errs bytes.Buffer
			if code := Run(args, &out, &errs, "test", "commit"); code != 2 || out.Len() != 0 {
				t.Fatalf("removed %q code=%d out=%s err=%s", args, code, out.String(), errs.String())
			}
		}
	}
}

func TestRuleImportStoppedCLI(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonMode], func(t *testing.T) {
			root := t.TempDir()
			paths := isolatedTestPaths(t, root)
			t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n"))
			file := filepath.Join(root, "rules.yaml")
			if err := os.WriteFile(file, []byte("rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n  - IP-CIDR,192.0.2.0/24,REJECT,no-resolve\n"), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("rules:\n  - DOMAIN-SUFFIX,ads.example.com,REJECT\n"))
			}))
			defer server.Close()
			for _, args := range [][]string{{"rules", "import", "native", file}, {"rules", "import", "ads", server.URL + "/set?token=PRIVATE"}} {
				if jsonMode {
					args = append([]string{"--json"}, args...)
				}
				var out, errs bytes.Buffer
				if code := Run(args, &out, &errs, "test", "commit"); code != 0 || errs.Len() != 0 {
					t.Fatalf("args=%q code=%d out=%s err=%s", args, code, out.String(), errs.String())
				}
				if strings.Contains(out.String(), "PRIVATE") {
					t.Fatalf("source leaked: %s", out.String())
				}
				if jsonMode {
					var envelope struct {
						OK   bool `json:"ok"`
						Data struct {
							Name   string `json:"name"`
							Action string `json:"action"`
							Saved  bool   `json:"saved"`
						} `json:"data"`
					}
					if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !envelope.OK || envelope.Data.Name == "" || envelope.Data.Action != "import" || !envelope.Data.Saved {
						t.Fatalf("success contract %s: %v", out.String(), err)
					}
				} else if !strings.Contains(out.String(), "Rule ") || !strings.Contains(out.String(), ": import.") {
					t.Fatalf("text response: %s", out.String())
				}
			}
			doc, err := rules.New(paths.ConfigDir, nil).List()
			if err != nil || len(doc.Entries) != 2 || doc.Entries[0].Name != "native" || doc.Entries[1].Name != "ads" {
				t.Fatalf("persisted entries=%+v err=%v", doc.Entries, err)
			}
			var out, errs bytes.Buffer
			if code := Run([]string{"--json", "rules", "import", "missing", filepath.Join(root, "absent.yaml")}, &out, &errs, "test", "commit"); code != 1 || out.Len() != 0 || !strings.Contains(errs.String(), `"code":"rule_error"`) {
				t.Fatalf("failure exit=%d out=%s err=%s", code, out.String(), errs.String())
			}
		})
	}
}

func TestRuleImportCompletion(t *testing.T) {
	_, children := completionCommands()
	if !strings.Contains(" "+strings.Join(children["rules"], " ")+" ", " import ") {
		t.Fatalf("rules children=%v", children["rules"])
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, _ := completion(shell)
		if strings.Contains(script, "import-local") || strings.Contains(script, "import-remote") || strings.Contains(script, "rules classical domain ipcidr") {
			t.Fatalf("%s completion retains removed formats or commands", shell)
		}
	}
	root := t.TempDir()
	file := filepath.Join(root, "rules with spaces.yaml")
	if err := os.WriteFile(file, []byte("rules: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script, _ := completion("bash")
	probe := script + "\n" + `COMP_WORDS=(nagi --json rules import work '` + filepath.Join(root, "rules") + `'); COMP_CWORD=5; _nagi_complete; printf 'FILE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi rules import work '` + filepath.Join(root, "rules") + `'); COMP_CWORD=4; _nagi_complete; printf 'PLAIN:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi rules import '` + filepath.Join(root, "rules") + `'); COMP_CWORD=3; _nagi_complete; printf 'NAME:%s\n' "${COMPREPLY[*]}"
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PLAIN:"+file) || strings.Contains(string(out), "NAME:"+file) || !strings.Contains(string(out), "FILE:"+file) {
		t.Fatalf("bash completion: %s (%v)", out, err)
	}
}
