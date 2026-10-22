package diagnostic

import (
	"context"
	"encoding/json"
	"github.com/ronigooja/Nagi/internal/i18n"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"strings"
	"testing"
)

func TestDiagnosticSourceDescriptorsPreserveMachineValues(t *testing.T) {
	report := Run(context.Background(), nagiruntime.Paths{ConfigDir: t.TempDir(), DataDir: t.TempDir(), StateDir: t.TempDir(), RuntimeDir: t.TempDir(), PIDPath: "/nonexistent/nagi.pid", SocketPath: "/nonexistent/nagi.sock"}, "/nonexistent/mihomo")
	for _, check := range report.Checks {
		descriptor, ok := check.MessageDescriptors()["/message"]
		if !ok || !i18n.ValidDescriptor(descriptor) {
			t.Fatalf("missing source descriptor for %s: %#v", check.Name, descriptor)
		}
		got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(descriptor, check.Message)
		if got == check.Message {
			t.Fatalf("untranslated check %s: %s", check.Name, got)
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Checks) != len(report.Checks) || decoded.Checks[0].Message != report.Checks[0].Message || len(decoded.Checks[0].Messages) != 0 {
		t.Fatal("diagnostic JSON shape changed")
	}
}

func TestDiagnosticCheckRemovesPrivateURLDetails(t *testing.T) {
	url := "https://feeds.example/sub/private-token?jwt=secret"
	check := newCheck("control_api", "warning", i18n.Errorf("unregistered", "request %s failed", url))
	if strings.Contains(check.Message, "private-token") || strings.Contains(check.Message, "jwt=") || !strings.Contains(check.Message, "https://feeds.example") {
		t.Fatalf("unsafe diagnostic fallback: %q", check.Message)
	}
	descriptor := check.Messages["/message"]
	encodedDescriptor, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedDescriptor), "private-token") || strings.Contains(string(encodedDescriptor), "jwt=") || !strings.Contains(string(encodedDescriptor), "https://feeds.example") {
		t.Fatalf("unsafe diagnostic descriptor: %s", encodedDescriptor)
	}
	encoded, err := RedactedJSON(FullReport{Checks: []Check{check}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-token") || strings.Contains(string(encoded), "jwt=") || !strings.Contains(string(encoded), "https://feeds.example") {
		t.Fatalf("unsafe diagnostic export: %s", encoded)
	}
}
