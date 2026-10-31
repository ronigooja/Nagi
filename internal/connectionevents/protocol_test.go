package connectionevents

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventLogAssignsStableIDsAndPreservesMetadata(t *testing.T) {
	log, err := NewEventLog("engine-test", 4)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 10, 31, 12, 34, 56, 789000000, time.FixedZone("test", 3600))
	metadata := &ConnectionMetadata{DestinationHost: "example.test", DestinationPort: 443, Chains: []string{"Proxy A"}}
	event := log.Publish(timestamp, StageDial, FailureRefused, metadata)

	if event.Kind != KindConnectionFailed || event.Version != ProtocolVersion {
		t.Fatalf("unexpected event envelope: %#v", event)
	}
	if event.EventID != "engine-test/1" || event.Sequence != 1 {
		t.Fatalf("unexpected event identity: %#v", event)
	}
	if !event.Timestamp.Equal(timestamp.UTC()) {
		t.Fatalf("timestamp=%s, want %s", event.Timestamp, timestamp.UTC())
	}
	metadata.Chains[0] = "mutated"
	if event.Metadata.Chains[0] != "Proxy A" {
		t.Fatal("published event retained caller-owned metadata")
	}

	messages, err := log.ReadAfter(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].EventID != event.EventID {
		t.Fatalf("read messages=%#v", messages)
	}
}

func TestEventLogReportsExplicitOverflowGap(t *testing.T) {
	log, err := NewEventLog("engine-test", 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		log.Publish(time.Unix(int64(i+1), 0), StageDial, FailureDial, nil)
	}

	hello := log.Hello()
	if hello.OldestSequence != 4 || hello.LatestSequence != 5 || hello.Capacity != 2 {
		t.Fatalf("hello=%#v", hello)
	}
	messages, err := log.ReadAfter(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages=%#v", messages)
	}
	gap := messages[0]
	if gap.Kind != KindGap || gap.Reason != "overflow" || gap.FromSequence != 1 || gap.ToSequence != 3 || gap.Dropped != 3 {
		t.Fatalf("gap=%#v", gap)
	}
	if messages[1].Sequence != 4 || messages[2].Sequence != 5 {
		t.Fatalf("retained events=%#v", messages[1:])
	}

	messages, err = log.ReadAfter(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Sequence != 4 {
		t.Fatalf("bounded read=%#v", messages)
	}
}

func TestMessageJSONAndValidation(t *testing.T) {
	log, err := NewEventLog("engine-test", 2)
	if err != nil {
		t.Fatal(err)
	}
	event := log.Publish(time.Date(2026, 10, 31, 1, 2, 3, 400, time.UTC), StageHandshake, FailureTLS, &ConnectionMetadata{Network: "tcp"})
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EventID != event.EventID || decoded.Code != FailureTLS || decoded.Metadata.Network != "tcp" {
		t.Fatalf("decoded=%#v", decoded)
	}
	if _, err := DecodeMessage([]byte(`{"version":1,"kind":"future"}`)); err == nil {
		t.Fatal("unknown message kind was accepted")
	}
	if _, err := DecodeMessage([]byte(`{"version":2,"kind":"gap"}`)); err == nil {
		t.Fatal("unknown protocol version was accepted")
	}
}

func TestEventLogRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewEventLog("", 1); err == nil {
		t.Fatal("empty stream ID was accepted")
	}
	if _, err := NewEventLog("engine-test", 0); err == nil {
		t.Fatal("zero queue capacity was accepted")
	}
	log, err := NewEventLog("engine-test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.ReadAfter(0, 0); err == nil {
		t.Fatal("zero read limit was accepted")
	}
}
