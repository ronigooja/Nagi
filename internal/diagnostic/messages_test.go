package diagnostic

import (
	"context"
	"encoding/json"
	"github.com/ronigooja/Nagi/internal/i18n"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
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
