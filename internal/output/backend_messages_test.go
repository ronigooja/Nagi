package output

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestCollectMessagesUsesExistingExplicitStringTargets(t *testing.T) {
	descriptor := i18n.Descriptor{ID: "backend.diagnostic.name.process"}
	result := i18n.Result{Value: map[string]any{"checks": []any{map[string]any{"message": "Keep original", "number": 1}}, "raw": "Ready to capture."}, Messages: map[string]i18n.Descriptor{
		"/checks/0/message": descriptor, "/checks/0/number": descriptor, "/checks/00/message": descriptor, "/checks/0/missing": descriptor, "/checks/0/~2bad": descriptor, "/missing": descriptor, "/raw": {ID: "unknown"},
	}}
	messages := CollectMessages(result)
	if len(messages) != 1 || messages["/data/checks/0/message"].ID != descriptor.ID {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["raw"] != "Ready to capture." {
		t.Fatalf("raw content changed: %s", data)
	}
}
func TestCollectMessagesNestedCheckAndRedaction(t *testing.T) {
	message := i18n.Errorf("backend.control.read_mihomo_response_w", "read mihomo response: %w", errors.New("https://private.example/?token=secret"))
	check := diagnostic.Check{Name: "process", Message: message.Error(), Messages: map[string]i18n.Descriptor{"/message": message.Descriptor()}}
	report := diagnostic.Report{Checks: []diagnostic.Check{check}}
	descriptors := CollectMessages(report)
	d := descriptors["/data/checks/0/message"]
	if d.Args["arg0"] != "[redacted URL]" {
		t.Fatalf("unsafe metadata: %#v", d)
	}
	if got := RenderMessageField(check, "/message", check.Message, i18n.New(i18n.SimplifiedChinese)); got != "读取 mihomo 响应：[redacted URL]" {
		t.Fatal(got)
	}
	if report.Checks[0].Message != message.Error() {
		t.Fatal("original diagnostic mutated")
	}
}
func TestCollectMessagesCycleDoesNotPanic(t *testing.T) {
	type node struct {
		Next *node `json:"next"`
	}
	n := &node{}
	n.Next = n
	if got := CollectMessages(n); got != nil {
		t.Fatal(got)
	}
}
