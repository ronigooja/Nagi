package command

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
)

func TestConnectionFailureJSONOutputContract(t *testing.T) {
	var out bytes.Buffer
	event := control.ConnectionFailureEvent{
		Type:       "connection_failure",
		OccurredAt: "2026-10-31T12:34:56Z",
		Target:     "example.test:443",
		Proxy:      "Node A",
		Error:      control.ConnectionFailureError{Code: "refused", Message: "connection refused"},
	}
	if err := writeConnectionFailure(&out, true, event); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK   bool                           `json:"ok"`
		Data control.ConnectionFailureEvent `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Type != event.Type || envelope.Data.Error.Code != "refused" || envelope.Data.OccurredAt != event.OccurredAt {
		t.Fatalf("envelope=%+v", envelope)
	}
}
