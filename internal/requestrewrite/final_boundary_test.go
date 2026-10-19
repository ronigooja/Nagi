package requestrewrite

import (
	"context"
	"encoding/json"
	"testing"
)

func TestProtocolOwnedWebSocketHeadersRejected(t *testing.T) {
	for _, name := range []string{"Sec-WebSocket-Key", "sec-websocket-accept", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol", "Sec-WebSocket-Version"} {
		d := baseDoc()
		d.Rules = []Rule{rule("protocol", Operation{Op: "header_set", Name: name, Value: str("fictitious")})}
		if _, err := Compile(d); err == nil {
			t.Fatalf("accepted %s", name)
		}
		old := response("").Header
		raw, _ := json.Marshal(map[string][]string{name: {"fictitious"}})
		if _, err := scriptHeaders(raw, old, false); err == nil {
			t.Fatalf("script accepted %s", name)
		}
	}
}

func TestWebSocketScriptExplicitOperationMessageType(t *testing.T) {
	defer SetScriptExecutor(nil)
	d := baseDoc()
	d.Scripts = []Script{{ID: "type", BodyAccess: "buffer", Source: "function transform(i){}"}}
	r := rule("type", Operation{Op: "script", Script: "type", MessageType: "binary"})
	r.Stage = StageWebSocketClient
	d.Rules = []Rule{r}
	SetScriptExecutor(func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return raw(`{"action":"replace","message":{"body_base64":"/w=="}}`), nil
	})
	hs := response("")
	hs.StatusCode = 101
	result, err := compiled(t, d).TransformMessage(context.Background(), StageWebSocketClient, request(t, ""), hs, Message{Type: "text", Body: []byte("original")})
	if err != nil || result.Message.Type != "binary" || len(result.Message.Body) != 1 || result.Message.Body[0] != 255 {
		t.Fatal(result, err)
	}
}
