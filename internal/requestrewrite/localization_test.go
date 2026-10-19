package requestrewrite

import (
	"errors"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestDomainEventDescriptorKeepsCodeRuleAndEnglish(t *testing.T) {
	original := &Error{Code: "rewrite_transform", RuleID: "work-rule"}
	var domain *Error
	var message *i18n.Message
	if !errors.As(original, &domain) || domain != original || !errors.As(original, &message) {
		t.Fatal("lost typed domain error or presentation descriptor")
	}
	if original.Error() != "rewrite_transform: rule work-rule" || message.Error() != original.Error() {
		t.Fatalf("English changed: %v", original)
	}
	if got := i18n.New(i18n.SimplifiedChinese).Render(message); got != "请求改写转换失败（rewrite_transform）；规则 work-rule" {
		t.Fatalf("localized=%q", got)
	}
	unsafe := &Error{Code: "rewrite_transform", RuleID: "https://fictional:secret@private.invalid/"}
	if !errors.As(unsafe, &message) {
		t.Fatal("missing source")
	}
	if strings.Contains(i18n.New(i18n.SimplifiedChinese).Render(message), "fictional") {
		t.Fatal("sensitive descriptor argument")
	}
	if (&Error{Code: "future_external_code"}).Unwrap() != nil {
		t.Fatal("inferred unknown domain event")
	}
}
