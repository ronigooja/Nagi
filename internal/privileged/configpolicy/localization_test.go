package configpolicy

import (
	"errors"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestPolicyLocalizedSourcePreservesEnglishAndCause(t *testing.T) {
	err := Validate([]byte("mode: invalid\n"))
	if err == nil || err.Error() != "configuration is not supported for privileged mihomo: mode: unsupported value" || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error=%v", err)
	}
	var message *i18n.Message
	if !errors.As(err, &message) {
		t.Fatal("missing descriptor")
	}
	got := i18n.New(i18n.SimplifiedChinese).Render(message)
	if got != "特权 mihomo 不支持此配置：mode：不支持的值" {
		t.Fatalf("localized=%q", got)
	}
	err = Validate(nil)
	if !errors.As(err, &message) || !errors.Is(err, ErrUnsupported) {
		t.Fatal("missing invalid-size descriptor/cause")
	}
	got = i18n.New(i18n.SimplifiedChinese).Render(message)
	if strings.Contains(got, "configuration is not supported") || !strings.Contains(got, "字节") {
		t.Fatalf("localized=%q", got)
	}
}
