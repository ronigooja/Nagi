package command

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDynamicProxyBashCompletionWithSpaces(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { if [[ "$1 $2 $3" == "completion candidates groups" ]]; then printf '%s\n' 'Group One' Other; else printf '%s\n' 'Node A' 'Node B'; fi; }
` + script + `
COMP_WORDS=(nagi proxy show Gro); COMP_CWORD=3; _nagi_complete; printf 'GROUP:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy select 'Group One' 'Node '); COMP_CWORD=4; _nagi_complete; printf 'NODE:%s\n' "${COMPREPLY[*]}"
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("completion: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "GROUP:Group One") || !strings.Contains(string(out), "NODE:Node A Node B") {
		t.Fatalf("completion output: %s", out)
	}
}

func TestApplyDefaultBashCompletionWithProfileAndGroup(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { if [[ "$1 $2 $3" == "completion candidates profile" ]]; then printf '%s\n' work; elif [[ "$1 $2 $3 $4" == "completion candidates profile-groups work" ]]; then printf '%s\n' 'Proxy Group' Other; fi; }
` + script + `
COMP_WORDS=(nagi profile override apply-default wo); COMP_CWORD=4; _nagi_complete; printf 'PROFILE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi profile override apply-default work --d); COMP_CWORD=5; _nagi_complete; printf 'FLAG:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi profile override apply-default work --dns-proxy Pro); COMP_CWORD=6; _nagi_complete; printf 'GROUP:%s\n' "${COMPREPLY[*]}"
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("completion: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PROFILE:work") || !strings.Contains(string(out), "FLAG:--dns-proxy") || !strings.Contains(string(out), "GROUP:Proxy Group") {
		t.Fatalf("completion output: %s", out)
	}
	for _, shell := range []string{"zsh", "fish"} {
		generated, _ := completion(shell)
		if !strings.Contains(generated, "--dns-proxy") || !strings.Contains(generated, "completion candidates profile-groups") {
			t.Fatalf("%s completion missing DNS group: %s", shell, generated)
		}
	}
}
