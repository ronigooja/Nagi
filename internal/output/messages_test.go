package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestLocalizedMessageJSONAndText(t *testing.T) {
	message := i18n.NewMessage("cli.language.invalid", map[string]any{"value": "fr"}, "Unsupported language fr; use en or zh-Hans.", nil)
	var out bytes.Buffer
	if err := WriteMessage(&out, true, i18n.New(i18n.SimplifiedChinese), message); err != nil {
		t.Fatal(err)
	}
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data != message.Error() || env.Messages["/data"].ID != "cli.language.invalid" {
		t.Fatal(out.String())
	}
	out.Reset()
	WriteMessageError(&out, false, i18n.New(i18n.SimplifiedChinese), "usage", message)
	if !strings.Contains(out.String(), "不支持语言 fr") {
		t.Fatal(out.String())
	}
}
func TestMessageRedaction(t *testing.T) {
	value := "https://user:secret@example.invalid/private?token=secret"
	message := i18n.NewMessage("cli.language.invalid", map[string]any{"value": value}, "Unsupported "+value, nil)
	for _, jsonMode := range []bool{true, false} {
		var out bytes.Buffer
		WriteMessageError(&out, jsonMode, i18n.New(i18n.SimplifiedChinese), "usage", message)
		if strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "example.invalid") {
			t.Fatal(out.String())
		}
	}
}
func TestLegacyEnvelopeOmitsMessages(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, true, "unchanged"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "messages") {
		t.Fatal(out.String())
	}
}

func TestWrappedMessageRetainsDescriptorAndCause(t *testing.T) {
	cause := errors.New("sentinel")
	message := i18n.NewMessage("cli.language.missing", nil, "--lang requires a language; use en or zh-Hans.", cause)
	wrapped := fmt.Errorf("context: %w", message)
	if !errors.Is(wrapped, cause) {
		t.Fatal("lost cause")
	}
	var out bytes.Buffer
	WriteErrorCause(&out, true, i18n.New(i18n.SimplifiedChinese), "usage", wrapped)
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Messages["/error/message"].ID != "cli.language.missing" || env.Error.Message != message.Error() {
		t.Fatal(out.String())
	}
}
