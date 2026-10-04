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
