package i18n

import (
	"errors"
	"fmt"
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
