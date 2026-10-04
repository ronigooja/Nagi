package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/proxy"
)

func TestConnectionChainAndUnavailableSelectionText(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, false, map[string]any{"connections": []proxy.Connection{{ID: "42", Chains: []string{"Choice", "Node A"}}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Chain: Choice -> Node A") {
		t.Fatalf("chain absent: %s", out.String())
	}
	out.Reset()
	if err := Write(&out, false, proxy.Group{Name: "Choice", Now: "Old", All: []string{"New"}, SelectedStatus: "removed"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Old (removed)") {
		t.Fatalf("removed status absent: %s", out.String())
	}
}
