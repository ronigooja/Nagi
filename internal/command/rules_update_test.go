package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/rules"
)

func TestRuleUpdateAndEditCLIContract(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(map[bool]string{true: "json", false: "text"}[jsonMode], func(t *testing.T) {
			root := t.TempDir()
			paths := isolatedTestPaths(t, root)
			binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
			t.Setenv("NAGI_MIHOMO_BIN", binary)
			file := filepath.Join(root, "rules.yaml")
			write := func(body string) {
				t.Helper()
				if err := os.WriteFile(file, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(want int, args ...string) string {
				t.Helper()
				if jsonMode {
					args = append([]string{"--json"}, args...)
				}
				var out, errs bytes.Buffer
				if code := Run(args, &out, &errs, "test", "commit"); code != want {
					t.Fatalf("%v exit=%d want=%d out=%s err=%s", args, code, want, out.String(), errs.String())
				}
				if want != 0 {
					if jsonMode && !strings.Contains(errs.String(), `"code":"rule_error"`) {
						t.Fatal(errs.String())
					}
					return errs.String()
				}
				if jsonMode && (!strings.Contains(out.String(), `"saved":true`) || !strings.Contains(out.String(), `"profile":"default"`)) {
					t.Fatal(out.String())
				}
				return out.String()
			}
			run(0, "rules", "add", "single", "DOMAIN", "old.example", "DIRECT")
			run(0, "rules", "disable", "single")
			run(0, "rules", "edit", "single", "MATCH", "-", "REJECT")
			doc, _ := rules.New(paths.ConfigDir, "default", nil).List()
			if doc.Entries[0].Enabled || doc.Entries[0].Type != "MATCH" || doc.Entries[0].Payload != "" {
				t.Fatalf("edit: %+v", doc)
			}
			write("rules: ['DOMAIN,old.example,DIRECT']")
			run(0, "rules", "import", "bundle", file)
			write("rules: ['DOMAIN,new.example,REJECT']")
			run(0, "rules", "update", "bundle")
			doc, _ = rules.New(paths.ConfigDir, "default", nil).List()
			if doc.Entries[1].Items[0] != "DOMAIN,new.example,REJECT" {
				t.Fatal(doc)
			}
			previous, _ := os.ReadFile(rules.Path(paths.ConfigDir, "default"))
			// Rejection must restore both content and source, and disclose saved/runtime state.
			if err := os.WriteFile(binary, []byte("#!/bin/sh\necho '{\"valid\":false,\"errors\":[{\"path\":\"rules[0]\",\"code\":\"rule_invalid_format\",\"message\":\"invalid rule\"}]}'\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			message := run(1, "rules", "update", "bundle")
			if !strings.Contains(message, "runtime configuration was not changed") {
				t.Fatal(message)
			}
			after, _ := os.ReadFile(rules.Path(paths.ConfigDir, "default"))
			if !bytes.Equal(previous, after) {
				t.Fatal("failed update changed saved rules")
			}
			run(1, "rules", "edit", "absent", "MATCH", "-", "DIRECT")
			run(1, "rules", "update", "single")
		})
	}
}

func TestRuleUpdateEditPreflight(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "missing"))
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	for _, args := range [][]string{
		{"rules", "update"}, {"rules", "update", "bad/name"}, {"rules", "update", "work", ""}, {"rules", "update", "work", "https://user:PRIVATE@example.com/rules"}, {"rules", "update", "work", "source", "extra"},
		{"rules", "edit", "bad/name", "MATCH", "-", "DIRECT"}, {"rules", "edit", "work", "BAD", "example.com", "DIRECT"}, {"rules", "edit", "work", "DOMAIN", "", "DIRECT"},
	} {
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "commit"); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "Usage:") || strings.Contains(errs.String(), "PRIVATE") {
			t.Fatalf("%v exit=%d out=%s err=%s", args, code, out.String(), errs.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("preflight created runtime state: %v", err)
	}
}
