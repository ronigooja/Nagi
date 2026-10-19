package subscription

import (
	"encoding/json"
	"github.com/ronigooja/Nagi/internal/i18n"
	"testing"
)

func TestStoredReasonCodesOwnPresentationMetadata(t *testing.T) {
	entry := Entry{Name: "running", PauseReason: "dns_target_missing", LastFailureReason: "network_error"}
	d := entry.MessageDescriptors()
	if !i18n.ValidDescriptor(d["/pause_reason"]) || !i18n.ValidDescriptor(d["/last_failure_reason"]) {
		t.Fatal(d)
	}
	if got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(d["/pause_reason"], entry.PauseReason); got != "DNS 代理目标缺失" {
		t.Fatal(got)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["pause_reason"] != "dns_target_missing" || decoded["name"] != "running" || decoded["last_failure_reason"] != "network_error" {
		t.Fatal(string(data))
	}
	entry.PauseReason = "unknown_future_reason"
	if _, ok := entry.MessageDescriptors()["/pause_reason"]; ok {
		t.Fatal("guessed unknown stored reason")
	}
}
func TestPreviewHintsAreAttachedBeforePresentation(t *testing.T) {
	result := Compare("work", "mihomo", map[string]any{"dns": map[string]any{"enable": true}}, map[string]any{"dns": map[string]any{"enable": false}})
	for _, change := range result.Changes {
		descriptor := change.MessageDescriptors()["/behavior"]
		if !i18n.ValidDescriptor(descriptor) {
			t.Fatal(change)
		}
		if got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(descriptor, change.Behavior); got != "DNS 解析或上游路由可能变化。" {
			t.Fatal(got)
		}
	}
	if result.Changes[0].Behavior != "DNS resolution or upstream routing may change." {
		t.Fatal(result)
	}
}
