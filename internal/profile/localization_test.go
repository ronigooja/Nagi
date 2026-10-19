package profile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestValidationDiagnosticKeepsEnglishAndExternalCause(t *testing.T) {
	original := &ValidationError{Profile: "work", Path: "rules[2]", Code: "rule_invalid", Message: "configuration validation failed"}
	expected := "profile \"work\" validation failed at rules[2] (rule_invalid): configuration validation failed; mihomo did not provide a specific cause; inspect rule syntax, proxy targets, and referenced rule providers or sub-rules in the candidate configuration; validate a local copy with `mihomo -t -f FILE -d DIRECTORY` for detailed diagnostics (may contain private configuration values)"
	if original.Error() != expected {
		t.Fatal(original.Error())
	}
	var message *i18n.Message
	if !errors.As(original, &message) || message.Error() != expected {
		t.Fatal("typed diagnostic lost original English")
	}
	localized := i18n.New(i18n.SimplifiedChinese).Render(message)
	if !strings.Contains(localized, "验证失败") || !strings.Contains(localized, "configuration validation failed") || !strings.Contains(localized, "检查候选配置") {
		t.Fatal(localized)
	}
	// External prose cannot change event identity or typed control flow.
	next := *original
	next.Message = "external parser detail"
	var second *i18n.Message
	errors.As(&next, &second)
	if second.Descriptor().ID != message.Descriptor().ID {
		t.Fatal("descriptor inferred from external prose")
	}
	original.Message = "https://private.example/?token=secret"
	errors.As(original, &message)
	if message.Descriptor().Args["detail"] != "[redacted URL]" {
		t.Fatal(message.Descriptor())
	}
	var typed *ValidationError
	if !errors.As(original, &typed) || typed != original {
		t.Fatal("typed validation identity lost")
	}
}
func TestHealthcheckExplanationHasSourceMetadata(t *testing.T) {
	store, _, _ := providerFixture(t)
	for _, target := range []struct{ kind, name string }{{"group", "Auto"}, {"provider", "original"}} {
		result, err := store.Healthcheck("default", target.kind, target.name)
		if err != nil {
			t.Fatal(err)
		}
		d := result.MessageDescriptors()["/reason"]
		if !i18n.ValidDescriptor(d) {
			t.Fatal(d)
		}
		if got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(d, result.Reason); got == result.Reason {
			t.Fatal(got)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Healthcheck
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Reason != result.Reason || decoded.Name != target.name || len(decoded.Messages) > 0 {
			t.Fatal(string(raw))
		}
	}
}
