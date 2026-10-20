package command

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNodeChainCompletionAllShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, ok := completion(shell)
		if !ok {
			t.Fatal(shell)
		}
		for _, want := range []string{"proxy-nodes", "chain-targets", "node-groups", "hostname", "control-url", "exit-node", "auth-key-file", "auth-key-stdin", "dry-run", "via"} {
			if !strings.Contains(script, want) {
				t.Fatalf("%s missing %s", shell, want)
			}
		}
		for _, old := range []string{"tailscale-nodes", "tailscale-targets", "bind unbind"} {
			if strings.Contains(script, old) {
				t.Fatalf("old completion retained: %s", old)
			}
		}
		if shell != "bash" {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Logf("%s syntax unavailable", shell)
				continue
			}
			cmd := exec.Command(path, "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s %v %s", shell, err, out)
			}
		}
	}
}
func TestNodeChainBashCompletion(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { case "$3" in proxy-nodes) printf '%s\n' 'My Tail' Other;; node-groups) printf '%s\n' 'My Group';; chain-targets) printf '%s\n' 'Target A';; esac; }
` + script + `
COMP_WORDS=(nagi proxy node ''); COMP_CWORD=3; _nagi_complete; printf 'NODE_ACTIONS:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy node add ''); COMP_CWORD=4; _nagi_complete; printf 'TYPE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi --json proxy node show My); COMP_CWORD=5; _nagi_complete; printf 'SHOW:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy node add tailscale Tail exit --group My); COMP_CWORD=8; _nagi_complete; printf 'GROUP:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy chain set ''); COMP_CWORD=4; _nagi_complete; printf 'KIND:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy chain set node Target); COMP_CWORD=5; _nagi_complete; printf 'TARGET:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy chain set node 'Target A' --via My); COMP_CWORD=7; _nagi_complete; printf 'VIA:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
`
	shells := []string{"bash"}
	if _, err := os.Stat("/root/environment/bash-3.2/bin/bash"); err == nil {
		shells = append(shells, "/root/environment/bash-3.2/bin/bash")
	}
	for _, shell := range shells {
		out, err := exec.Command(shell, "-c", probe).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v %s", shell, err, out)
		}
		for _, want := range []string{"NODE_ACTIONS:", "TYPE:tailscale", "SHOW:1:My Tail", "GROUP:1:My Group", "KIND:node provider subscription", "TARGET:1:Target A", "VIA:1:My Tail"} {
			if !strings.Contains(string(out), want) {
				t.Fatalf("%s omitted %s in %s", shell, want, out)
			}
		}
	}
}
