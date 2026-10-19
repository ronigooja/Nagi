package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSourceErrorPreservesOriginalAndSanitizesArguments(t *testing.T) {
	cause := errors.New("https://private.example/?token=secret")
	message := Errorf("backend.control.read_mihomo_response_w", "read mihomo response: %w", cause)
	if message.Error() != fmt.Errorf("read mihomo response: %w", cause).Error() || !errors.Is(message, cause) {
		t.Fatal("original error contract lost")
	}
	if got := New(SimplifiedChinese).Render(message); got != "读取 mihomo 响应：[redacted URL]" {
		t.Fatal(got)
	}
	descriptor := message.Descriptor()
	descriptor.Args["arg0"] = "changed"
	if message.Descriptor().Args["arg0"] != "[redacted URL]" {
		t.Fatal("descriptor aliases its arguments")
	}
}
func TestSourceErrorMultipleCauses(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	message := Errorf("unregistered", "operation: %w; recovery: %w", first, second)
	if !errors.Is(message, first) || !errors.Is(message, second) {
		t.Fatal("multi-cause graph lost")
	}
	if ValidDescriptor(message.Descriptor()) {
		t.Fatal("unknown ID accepted")
	}
}

func TestNestedSourceComposition(t *testing.T) {
	inner := Errorf("backend.subscription.invalid_subscription_name", "invalid subscription name")
	outer := Errorf("backend.control.read_mihomo_response_w", "read mihomo response: %w", inner)
	if outer.Error() != "read mihomo response: invalid subscription name" {
		t.Fatal(outer.Error())
	}
	if got := New(SimplifiedChinese).Render(outer); got != "读取 mihomo 响应：订阅名称无效" {
		t.Fatal(got)
	}
	if !errors.Is(outer, inner) {
		t.Fatal("cause lost")
	}
	descriptor := outer.Descriptor()
	if descriptor.Args["arg0"] != inner.Error() {
		t.Fatal("scalar mutated")
	}
	descriptor.ArgumentMessages["arg0"] = Descriptor{ID: "unknown"}
	if got := New(SimplifiedChinese).RenderDescriptor(descriptor, "fallback"); got != "读取 mihomo 响应：invalid subscription name" {
		t.Fatal(got)
	}
}

func TestCompositionOptionalFailuresBoundsAndCopy(t *testing.T) {
	raw := `{"id":"cli.language.invalid","args":{"value":"original"},"argument_messages":{"value":42,"missing":{"id":"cli.language.missing"}}}`
	var descriptor Descriptor
	if err := json.Unmarshal([]byte(raw), &descriptor); err != nil {
		t.Fatal(err)
	}
	if got := New(SimplifiedChinese).RenderDescriptor(descriptor, "fallback"); got != "不支持语言 original；请使用 en 或 zh-Hans。" {
		t.Fatal(got)
	}
	cyclic := Descriptor{ID: "cli.language.invalid", Args: map[string]any{"value": "original"}, ArgumentMessages: map[string]Descriptor{}}
	cyclic.ArgumentMessages["value"] = cyclic
	copied := CopyDescriptor(cyclic, func(s string) string { return "safe" })
	if _, err := json.Marshal(copied); err != nil {
		t.Fatal(err)
	}
	if got := New(SimplifiedChinese).RenderDescriptor(copied, "fallback"); len(got) > 2000 || !strings.Contains(got, "safe") {
		t.Fatal(got)
	}
}
