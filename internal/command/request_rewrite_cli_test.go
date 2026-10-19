package command

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func rewriteCLIRun(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := RunWithInput(args, strings.NewReader(input), &out, &err, "test", "test")
	return code, out.String(), err.String()
}
func rewriteOK(t *testing.T, input string, args ...string) map[string]any {
	t.Helper()
	return rewriteAliasOK(t, "request-rewrite", input, args...)
}

func rewriteAliasOK(t *testing.T, root, input string, args ...string) map[string]any {
	t.Helper()
	args = append([]string{"--json", root}, args...)
	code, out, err := rewriteCLIRun(t, input, args...)
	if code != 0 || err != "" {
		t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, err)
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal([]byte(out), &envelope) != nil || !envelope.OK {
		t.Fatal(out)
	}
	return envelope.Data
}

const rewriteTestRule = `{"id":"edit-api","enabled":true,"match":{"domain":"api.service.example","methods":["GET"],"path_prefix":"/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"header_set","name":"Authorization","value":"fictitious-secret"}]}`

func TestRewriteCLIInventoryHelpAndUsageWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	for _, spec := range commandSpecs {
		if spec.path != "request-rewrite" && !strings.HasPrefix(spec.path, "request-rewrite ") && spec.path != "rewrite" && !strings.HasPrefix(spec.path, "rewrite ") {
			continue
		}
		for _, args := range [][]string{
			append([]string{"help"}, strings.Fields(spec.path)...),
			append(strings.Fields(spec.path), "--help"),
		} {
			code, out, err := rewriteCLIRun(t, "", args...)
			if code != 0 || err != "" || !strings.Contains(out, "Usage: nagi "+spec.path) {
				t.Fatalf("%v %d %s %s", args, code, out, err)
			}
		}
	}
	for _, args := range [][]string{{"request-rewrite"}, {"request-rewrite", "domains"}, {"request-rewrite", "ca", "trust"}, {"request-rewrite", "rules", "update", "id"}, {"request-rewrite", "enable", "--profile", "work"}, {"request-rewrite", "rules", "show", "../bad"}, {"request-rewrite", "preview", "https://fictitious.example/fixture"}, {"request-rewrite", "validate", "--profile", "work", "--profile", "other"}, {"request-rewrite", "export", "-", "--json"}, {"request-rewrite", "ca", "export", "-", "--json"}} {
		for _, root := range []string{"rewrite", "request-rewrite"} {
			args[0] = root
			code, out, err := rewriteCLIRun(t, "", append([]string{"--json"}, args...)...)
			if code != 2 || out != "" || !strings.Contains(err, `"code":"usage"`) {
				t.Fatalf("%v %d %s %s", args, code, out, err)
			}
		}
	}
}
func TestRewriteCLIOfflineJourneySensitiveBoundaries(t *testing.T) {
	for _, root := range []string{"rewrite", "request-rewrite"} {
		t.Run(root, func(t *testing.T) {
			paths := isolatedTestPaths(t, t.TempDir())
			t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
			code, out, err := rewriteCLIRun(t, "", root, "domains", "list")
			if code != 0 || !strings.Contains(out, "No authorized domains.") || err != "" {
				t.Fatal(code, out, err)
			}
			rewriteAliasOK(t, root, `{"id":"api","pattern":"API.Service.Example."}`, "domains", "add", "-")
			rewriteAliasOK(t, root, rewriteTestRule, "rules", "add", "-")
			for _, args := range [][]string{{"rules", "list"}, {"rules", "show", "edit-api"}} {
				v := rewriteAliasOK(t, root, "", args...)
				b, _ := json.Marshal(v)
				if strings.Contains(string(b), "fictitious-secret") || strings.Contains(string(b), "Authorization") {
					t.Fatal("ordinary read exposed operation value", string(b))
				}
			}
			fixture := `{"scheme":"https","host":"api.service.example","port":443,"method":"GET","path":"/","query":"","headers":{},"body_base64":""}`
			preview := rewriteAliasOK(t, root, fixture, "preview", "-")
			if preview["sensitive"] != true || preview["outcome"] != "rewritten" {
				t.Fatal(preview)
			}
			if b, _ := json.Marshal(preview); !strings.Contains(string(b), "fictitious-secret") {
				t.Fatal("explicit preview omitted requested value")
			}
			code, out, err = rewriteCLIRun(t, fixture, root, "preview", "-")
			if code != 0 || err != "" || !strings.HasPrefix(out, "Sensitive output:") {
				t.Fatal(code, out, err)
			}
			rewriteAliasOK(t, root, "", "validate")
			before, _ := os.ReadFile(rewrite.Path(paths.ConfigDir, "default"))
			for _, bad := range []string{"", `{"id":"other","pattern":"other.service.example","pattern":"evil.example"}`, `{"id":"other","pattern":"other.service.example","extra":true}`, `{"id":"other","pattern":"other.service.example"},{"id":"injected","pattern":"injected.example"}`} {
				code, out, err = rewriteCLIRun(t, bad, "--json", root, "domains", "add", "-")
				if code != 1 || out != "" || !strings.Contains(err, `"code":"request_rewrite_error"`) || strings.Contains(err, "evil.example") {
					t.Fatal(code, out, err)
				}
			}
			code, out, err = rewriteCLIRun(t, strings.Replace(rewriteTestRule, `"id":"edit-api"`, `"id":"edit-api","id":"duplicate"`, 1), "--json", root, "rules", "update", "edit-api", "-")
			if code != 1 || out != "" || strings.Contains(err, "fictitious-secret") {
				t.Fatal(code, out, err)
			}
			after, _ := os.ReadFile(rewrite.Path(paths.ConfigDir, "default"))
			if !bytes.Equal(before, after) {
				t.Fatal("invalid input modified persisted state")
			}
			rewriteAliasOK(t, root, `["edit-api"]`, "rules", "reorder", "-")
			rewriteAliasOK(t, root, "", "rules", "disable", "edit-api")
			rewriteAliasOK(t, root, "", "rules", "enable", "edit-api")
			code, raw, err := rewriteCLIRun(t, "", root, "export", "-")
			if code != 0 || err != "" || !strings.Contains(raw, "fictitious-secret") || strings.Contains(raw, `"enabled":true,"domains"`) {
				t.Fatal(code, raw, err)
			}
			portable, e := rewrite.Parse([]byte(raw))
			if e != nil || portable.Document().Enabled {
				t.Fatal(e)
			}
			rewriteAliasOK(t, root, raw, "import", "-", "--profile", "work")
			code, out, err = rewriteCLIRun(t, strings.Replace(raw, `"enabled":false`, `"enabled":true`, 1), "--json", root, "import", "-")
			if code != 1 || out != "" || !strings.Contains(err, "rewrite_invalid") {
				t.Fatal(code, out, err)
			}
			export := filepath.Join(t.TempDir(), "rules.json")
			code, _, err = rewriteCLIRun(t, "", root, "export", export)
			if code != 0 || err != "" {
				t.Fatal(code, err)
			}
			info, _ := os.Stat(export)
			if info.Mode().Perm() != 0600 {
				t.Fatal(info.Mode())
			}
			code, _, _ = rewriteCLIRun(t, "", root, "export", export)
			if code != 1 {
				t.Fatal("overwrote export")
			}
			code, out, err = rewriteCLIRun(t, "", "completion", "candidates", "rewrite-rules")
			if code != 0 || out != "edit-api\n" || err != "" {
				t.Fatal(code, out, err)
			}
			rewriteAliasOK(t, root, "", "rules", "remove", "edit-api")
			rewriteAliasOK(t, root, `{"id":"api","pattern":"api.changed.example"}`, "domains", "update", "api", "-")
			rewriteAliasOK(t, root, "", "domains", "remove", "api")

		})
	}
}

func TestRewriteCLIInputFilesAndBounds(t *testing.T) {
	for _, root := range []string{"rewrite", "request-rewrite"} {
		t.Run(root, func(t *testing.T) {
			isolatedTestPaths(t, t.TempDir())
			inputDir := t.TempDir()
			input := filepath.Join(inputDir, "domain.json")
			data := `{"id":"api","pattern":"api.service.example"}`
			if e := os.WriteFile(input, []byte(data), 0644); e != nil {
				t.Fatal(e)
			}
			code, out, err := rewriteCLIRun(t, "", "--json", root, "domains", "add", input)
			if code != 1 || out != "" || !strings.Contains(err, "rewrite_input_private_file_required") {
				t.Fatal(code, out, err)
			}
			os.Chmod(input, 0600)
			rewriteAliasOK(t, root, "", "domains", "add", input)
			link := filepath.Join(inputDir, "link")
			os.Symlink(input, link)
			code, _, _ = rewriteCLIRun(t, "", root, "domains", "add", link)
			if code != 1 {
				t.Fatal("accepted symlink")
			}
			code, out, err = rewriteCLIRun(t, strings.Repeat("x", rewrite.MaxDocumentBytes+1), "--json", root, "import", "-")
			if code != 1 || out != "" || !strings.Contains(err, "rewrite_limit") {
				t.Fatal(code, out, err)
			}
			code, out, err = rewriteCLIRun(t, strings.Repeat("x", rewrite.MaxPreviewBytes+1), "--json", root, "preview", "-")
			if code != 1 || out != "" || !strings.Contains(err, "rewrite_limit") {
				t.Fatal(code, out, err)
			}

		})
	}
}

func TestRewriteCLICAPublicExport(t *testing.T) {
	for _, root := range []string{"rewrite", "request-rewrite"} {
		t.Run(root, func(t *testing.T) {
			isolatedTestPaths(t, t.TempDir())
			v := rewriteAliasOK(t, root, "", "ca", "generate")
			if v["trust"] == "trusted" {
				t.Fatal("generation installed trust")
			}
			code, pem, err := rewriteCLIRun(t, "", root, "ca", "export", "-")
			if code != 0 || err != "" || !strings.Contains(pem, "BEGIN CERTIFICATE") || strings.Contains(pem, "PRIVATE KEY") {
				t.Fatal(code, pem, err)
			}
			code, out, err := rewriteCLIRun(t, "", "--json", root, "ca", "generate")
			if code != 1 || out != "" || !strings.Contains(err, "rewrite_ca_exists") {
				t.Fatal(code, out, err)
			}

		})
	}
}

func TestRewriteBashCompletionInventoryAndIDs(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { printf '%s\n' edit-api api work; }
` + script + `
COMP_WORDS=(nagi request-rewrite rules ''); COMP_CWORD=3; _nagi_complete; printf 'RULES:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi request-rewrite ca trust ''); COMP_CWORD=4; _nagi_complete; printf 'TRUST:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi request-rewrite rules show edit); COMP_CWORD=4; _nagi_complete; printf 'ID:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi --json request-rewrite --profile work rules show edit); COMP_CWORD=7; _nagi_complete; printf 'NAMED:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi request-rewrite domains update a); COMP_CWORD=4; _nagi_complete; printf 'DOMAIN:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi request-rewrite domains --profile work update a); COMP_CWORD=6; _nagi_complete; printf 'DOMAIN_NAMED:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi request-rewrite preview ''); COMP_CWORD=3; _nagi_complete; printf 'FILE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi request-rewrite rules list --p); COMP_CWORD=4; _nagi_complete; printf 'PROFILE:%s\n' "${COMPREPLY[*]}"
`
	for _, shell := range []string{"bash", "/root/environment/bash-3.2/bin/bash"} {
		if _, e := exec.LookPath(shell); e != nil {
			continue
		}
		for _, root := range []string{"rewrite", "request-rewrite"} {
			out, e := exec.Command(shell, "-c", strings.ReplaceAll(strings.ReplaceAll(probe, "nagi request-rewrite", "nagi "+root), "nagi --json request-rewrite", "nagi --json "+root)).CombinedOutput()
			if e != nil {
				t.Fatalf("%s %s %v", shell, out, e)
			}
			for _, want := range []string{"RULES:list show add update remove enable disable reorder", "TRUST:install remove", "ID:edit-api", "NAMED:edit-api", "DOMAIN:api", "DOMAIN_NAMED:api", "PROFILE:--profile", "FILE:"} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("missing %s: %s", want, out)
				}
			}
		}
	}
	for _, shell := range []string{"zsh", "fish"} {
		script, _ := completion(shell)
		for _, want := range []string{"rewrite-domains", "rewrite-rules", "--profile", "reorder", "trust"} {
			if !strings.Contains(script, want) && !(shell == "fish" && want == "--profile") {
				t.Fatal(shell, want)
			}
		}
	}
}
