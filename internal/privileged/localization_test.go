package privileged

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestResponseOptionalMessages(t *testing.T) {
	original := i18n.Errorf("system.privileged.unsupported.protocol.version", "unsupported protocol version %d", 7)
	response := Response{Version: ProtocolVersion, Error: original.Error(), Messages: map[string]i18n.Descriptor{"/error": original.Descriptor()}}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Error != "unsupported protocol version 7" || decoded.Messages["/error"].ID != original.Descriptor().ID {
		t.Fatalf("response=%+v", decoded)
	}
	if got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(decoded.Messages["/error"], decoded.Error); got != "不支持的协议版本 7" {
		t.Fatalf("localized=%q", got)
	}
	// Old consumers continue reading the same English business value.
	var legacy struct {
		Version int
		Error   string
	}
	if err := json.Unmarshal(data, &legacy); err != nil || legacy.Error != response.Error {
		t.Fatalf("legacy=%+v error=%v", legacy, err)
	}
	for _, metadata := range []string{`null`, `[]`, `"invalid"`, `{"/error":null}`, `{"/error":{"id":"unknown"}}`, `{"/error":{"id":"system.privileged.untrusted.peer.uid","args":null}}`, `{"/error":{"id":"system.privileged.unsupported.protocol.version","args":{"arg0":[]}}}`} {
		var malformed Response
		wire := `{"version":1,"error":"original English","messages":` + metadata + `}`
		if err := json.Unmarshal([]byte(wire), &malformed); err != nil {
			t.Fatalf("optional %s: %v", metadata, err)
		}
		if malformed.Error != "original English" || len(malformed.Messages) != 0 {
			t.Fatalf("optional %s: %+v", metadata, malformed)
		}
	}
}

func TestResponseMessagesPreserveValidSibling(t *testing.T) {
	var response Response
	data := `{"version":1,"error":"unsupported protocol version 7","messages":{"/ignored":false,"/error":{"id":"system.privileged.unsupported.protocol.version","args":{"arg0":"7"}}}}`
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(i18n.New(i18n.SimplifiedChinese).RenderDescriptor(response.Messages["/error"], response.Error), "不支持") {
		t.Fatalf("messages=%+v", response.Messages)
	}
}
