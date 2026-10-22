package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
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
	if d.Args["arg0"] != "https://private.example/?token=%5Bredacted%5D" {
		t.Fatalf("unsafe metadata: %#v", d)
	}
	if got := RenderMessageField(check, "/message", check.Message, i18n.New(i18n.SimplifiedChinese)); got != "读取 mihomo 响应：https://private.example/?token=%5Bredacted%5D" {
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

func TestRenderMessageFieldNilProducerFallsBack(t *testing.T) {
	var check *diagnostic.Check
	if got := RenderMessageField(check, "/message", "legacy", i18n.New(i18n.SimplifiedChinese)); got != "legacy" {
		t.Fatal(got)
	}
}

func TestNestedSourceMetadataIsInvariantAndRedacted(t *testing.T) {
	inner := i18n.Errorf("backend.control.read_mihomo_response_w", "read mihomo response: %w", errors.New("https://secret.example/?token=secret"))
	outer := i18n.Errorf("backend.control.read_mihomo_response_w", "read mihomo response: %w", inner)
	var en, zh bytes.Buffer
	for _, item := range []struct {
		buffer   *bytes.Buffer
		language i18n.Language
	}{{&en, i18n.English}, {&zh, i18n.SimplifiedChinese}} {
		if err := WriteMessage(item.buffer, true, i18n.New(item.language), outer); err != nil {
			t.Fatal(err)
		}
	}
	if en.String() != zh.String() || strings.Contains(en.String(), "token=secret") || !strings.Contains(en.String(), "https://secret.example/") || !strings.Contains(en.String(), "argument_messages") {
		t.Fatal(en.String(), zh.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(en.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	descriptor := envelope.Messages["/data"]
	if got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(descriptor, "fallback"); got != "读取 mihomo 响应：读取 mihomo 响应：https://secret.example/?token=%5Bredacted%5D" {
		t.Fatal(got)
	}
}
