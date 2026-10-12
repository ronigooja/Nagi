package command

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestTailscaleBashCompletionLocalNamesKindsAndFlags(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { case "$3" in tailscale-nodes) printf '%s\n' 'My Tail' Other ;; tailscale-targets) if [[ "$4" == node ]]; then printf '%s\n' 'Node One'; else printf '%s\n' 'Provider One'; fi ;; esac; }
` + script + `
COMP_WORDS=(nagi proxy tailscale ''); COMP_CWORD=3; _nagi_complete; printf 'ACTIONS:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi --json proxy tailscale show My); COMP_CWORD=5; _nagi_complete; printf 'SHOW:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy tailscale bind 'My Tail' ''); COMP_CWORD=5; _nagi_complete; printf 'KIND:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy tailscale bind 'My Tail' node Node); COMP_CWORD=6; _nagi_complete; printf 'NODE:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy tailscale unbind provider Provider); COMP_CWORD=5; _nagi_complete; printf 'PROVIDER:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy tailscale bind 'My Tail' node 'First Node' Node); COMP_CWORD=7; _nagi_complete; printf 'BATCH_NODE:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy tailscale unbind provider 'First Feed' Provider); COMP_CWORD=6; _nagi_complete; printf 'BATCH_PROVIDER:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy tailscale edit 'My Tail' --a); COMP_CWORD=5; _nagi_complete; printf 'EDIT:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy tailscale add Tail exit --c); COMP_CWORD=6; _nagi_complete; printf 'PASTE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy tailscale add Tail exit key --c); COMP_CWORD=7; _nagi_complete; printf 'ADD:%s\n' "${COMPREPLY[*]}"
`
	shells := []string{"bash"}
	if _, err := os.Stat("/root/environment/bash-3.2/bin/bash"); err == nil {
		shells = append(shells, "/root/environment/bash-3.2/bin/bash")
	}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			out, err := exec.Command(shell, "-c", probe).CombinedOutput()
			if err != nil {
				t.Fatalf("%v %s", err, out)
			}
			for _, want := range []string{"ACTIONS:list show targets add edit bind unbind remove", "SHOW:1:My Tail", "KIND:node provider", "NODE:1:Node One", "PROVIDER:1:Provider One", "BATCH_NODE:1:Node One", "BATCH_PROVIDER:1:Provider One", "EDIT:--auth-key --auth-key-stdin --auth-key-file", "ADD:--control-url", "PASTE:--control-url"} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("missing %s in %s", want, out)
				}
			}
		})
	}
}
func TestTailscaleCompletionAllShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, ok := completion(shell)
		if !ok {
			t.Fatal(shell)
		}
		for _, want := range []string{"tailscale-nodes", "tailscale-targets", "list show targets add edit bind unbind remove", "hostname", "control-url", "exit-node", "auth-key-file", "auth-key-stdin"} {
			if !strings.Contains(script, want) {
				t.Fatalf("%s omitted %s", shell, want)
			}
		}
		if shell != "bash" {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Logf("%s syntax check unavailable: executable not installed", shell)
				continue
			}
			cmd := exec.Command(path, "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s syntax: %v %s", shell, err, out)
			}
		}
	}
}
