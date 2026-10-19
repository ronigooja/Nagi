package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func send(t *testing.T, s *Store, e Event) {
	t.Helper()
	e.SessionID = "session"
	if e.Type == "body" {
		s.mu.Lock()
		e.Offset = s.records[e.RecordID].Snapshots[e.Snapshot].BodySize
		s.mu.Unlock()
	}
	if e.Type == "finish" {
		s.mu.Lock()
		e.BodySize = s.records[e.RecordID].Snapshots[e.Snapshot].BodySize
		s.mu.Unlock()
	}
	r := s.Handle(Request{Action: "event", SessionID: "session", Event: &e})
	if r.Error != "" {
		t.Fatal(r.Error)
	}
}
func sampleArchive(t *testing.T) *Store {
	t.Helper()
	s := started(t, DefaultBodyLimit)
	add(t, s, "handshake")
	send(t, s, Event{Type: "body", RecordID: "handshake", Snapshot: "request_original", Data: []byte("original secret")})
	send(t, s, Event{Type: "finish", RecordID: "handshake", Snapshot: "request_original", State: "complete"})
	send(t, s, Event{Type: "complete", RecordID: "handshake", Outcome: "complete"})
	ws := Record{ID: "ws-1", SessionID: "session", ParentID: "handshake", Kind: "websocket", Stage: "client_to_server", Identity: Identity{SessionID: "session", Revision: 1}, Snapshots: map[string]Snapshot{"message_original": {BodyState: "collecting", MessageType: "binary", Headers: map[string][]string{}}}}
	send(t, s, Event{Type: "record", RecordID: ws.ID, Record: &ws})
	send(t, s, Event{Type: "body", RecordID: ws.ID, Snapshot: "message_original", Data: []byte{0, 255, 128, 0}})
	send(t, s, Event{Type: "finish", RecordID: ws.ID, Snapshot: "message_original", State: "complete"})
	send(t, s, Event{Type: "complete", RecordID: ws.ID, Outcome: "complete"})
	s.Handle(Request{Action: "stop", SessionID: "session"})
	return s
}
func TestNativeRoundTripAndHARWebSocket(t *testing.T) {
	source := sampleArchive(t)
	var out bytes.Buffer
	if err := source.exportNative(&out); err != nil {
		t.Fatal(err)
	}
	dest := NewStore()
	status, err := dest.importNative(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if status.Records != 2 || !status.ReadOnly || status.State != "stopped" {
		t.Fatalf("%+v", status)
	}
	r := dest.Handle(Request{Action: "body", RecordID: "ws-1", Snapshot: "message_original"})
	if !bytes.Equal(r.Body.Data, []byte{0, 255, 128, 0}) || r.Body.State != "complete" {
		t.Fatalf("%+v", r)
	}
	if r := dest.Handle(Request{Action: "clear", SessionID: "session"}); r.Error != "capture_read_only" {
		t.Fatal(r.Error)
	}
	list := dest.Handle(Request{Action: "list", List: &ListOptions{Query: "original secret"}})
	if len(list.Records) != 1 {
		t.Fatal("body search")
	}
	out.Reset()
	if err = source.exportHAR(&out); err != nil {
		t.Fatal(err)
	}
	var har struct {
		Log struct {
			Entries   []json.RawMessage `json:"entries"`
			WebSocket []struct {
				Record Record                       `json:"_nagiRecord"`
				Bodies map[string]map[string]string `json:"_nagiBodies"`
			} `json:"_nagiWebSocketMessages"`
		} `json:"log"`
	}
	if err = json.Unmarshal(out.Bytes(), &har); err != nil {
		t.Fatal(err)
	}
	if len(har.Log.Entries) != 1 || len(har.Log.WebSocket) != 1 || har.Log.WebSocket[0].Record.ParentID != "handshake" || har.Log.WebSocket[0].Bodies["message_original"]["text"] != "AP+AAA==" {
		t.Fatalf("%s", out.Bytes())
	}
	if !bytes.Contains(out.Bytes(), []byte("Bearer fictional-credential")) {
		t.Fatal("HAR redacted raw values")
	}
}
func TestMalformedNativeImportIsAtomic(t *testing.T) {
	source := sampleArchive(t)
	var archive bytes.Buffer
	if err := source.exportNative(&archive); err != nil {
		t.Fatal(err)
	}
	raw := archive.String()
	cases := []string{"", raw[:len(raw)/2], raw + "{}\n", strings.Replace(raw, `"version":1`, `"version":999`, 1), strings.Replace(raw, `"type":"body"`, `"type":"body","invalid_offset":0`, 1), strings.Replace(raw, `"captured":15`, `"captured":999`, 1), strings.Replace(raw, `"parent_id":"handshake"`, `"parent_id":"missing"`, 1)}
	for i, bad := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dest := sampleArchive(t)
			before := dest.Handle(Request{Action: "status"}).Status
			_, err := dest.importNative(strings.NewReader(bad))
			if err == nil {
				t.Fatal("accepted malformed archive")
			}
			after := dest.Handle(Request{Action: "status"}).Status
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(after)
			if !bytes.Equal(a, b) {
				t.Fatal("changed state")
			}
		})
	}
}
func TestStreamingLargeBody(t *testing.T) {
	s := started(t, MaxBodyLimit)
	add(t, s, "large")
	chunk := bytes.Repeat([]byte("x"), MaxChunk)
	for i := 0; i < 20; i++ {
		send(t, s, Event{Type: "body", RecordID: "large", Snapshot: "request_original", Data: chunk})
	}
	send(t, s, Event{Type: "finish", RecordID: "large", Snapshot: "request_original", State: "complete"})
	s.Handle(Request{Action: "stop", SessionID: "session"})
	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go func() { result <- s.exportNative(writer); writer.Close() }()
	dest := NewStore()
	status, err := dest.importNative(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if status.Bytes < 20*MaxChunk {
		t.Fatal("lost body")
	}
	body := dest.Handle(Request{Action: "body", RecordID: "large", Snapshot: "request_original", Offset: 19 * MaxChunk})
	if !body.Body.EOF || len(body.Body.Data) != MaxChunk {
		t.Fatalf("%+v", body.Body)
	}
}
func TestEvictionCursorAndTruncationCounts(t *testing.T) {
	s := started(t, 3)
	add(t, s, "a")
	send(t, s, Event{Type: "body", RecordID: "a", Snapshot: "request_original", Data: []byte("abcdef")})
	send(t, s, Event{Type: "body", RecordID: "a", Snapshot: "request_original", Data: []byte("ghijk")})
	snap := s.Handle(Request{Action: "show", RecordID: "a"}).Record.Snapshots["request_original"]
	if snap.Captured != 3 || snap.BodySize != 11 {
		t.Fatalf("%+v", snap)
	}
	send(t, s, Event{Type: "complete", RecordID: "a", Outcome: "complete"})
	add(t, s, "b")
	page := s.Handle(Request{Action: "list", List: &ListOptions{Limit: 1}})
	s.mu.Lock()
	if !s.evict("") {
		t.Fatal("did not evict")
	}
	s.mu.Unlock()
	page = s.Handle(Request{Action: "list", List: &ListOptions{Cursor: page.NextCursor}})
	if len(page.Records) != 1 || page.Records[0].ID != "b" {
		t.Fatal("unstable cursor")
	}
}
func TestArchiveService(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	socket := filepath.Join(dir, "capture.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, socket, os.Getuid()) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	s := sampleArchive(t)
	var raw bytes.Buffer
	if err := s.exportNative(&raw); err != nil {
		t.Fatal(err)
	}
	status, err := Import(ctx, socket, &raw)
	if err != nil || status.Records != 2 {
		t.Fatalf("%+v %v", status, err)
	}
	raw.Reset()
	if err = Export(ctx, socket, &raw, "native"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw.Bytes(), []byte(`"type":"end"`)) {
		t.Fatal("missing archive end")
	}
}

func TestSharedNativeV1Fixture(t *testing.T) {
	file, err := os.Open("testdata/native-v1.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	s := NewStore()
	status, err := s.importNative(file)
	if err != nil || status.Records != 2 {
		t.Fatalf("%+v %v", status, err)
	}
	reply := s.Handle(Request{Action: "body", RecordID: "http-1", Snapshot: "request_original"})
	if string(reply.Body.Data) != "{}" {
		t.Fatal("fixture original request body")
	}
}

func TestMissingAsyncBodyCannotBecomeComplete(t *testing.T) {
	s := started(t, DefaultBodyLimit)
	add(t, s, "gap")
	send(t, s, Event{Type: "body", RecordID: "gap", Snapshot: "request_original", Data: []byte("abc")})
	finish := Event{Type: "finish", SessionID: "session", RecordID: "gap", Snapshot: "request_original", State: "complete", BodySize: 9}
	reply := s.Handle(Request{Action: "event", SessionID: "session", Event: &finish})
	if reply.Error != "" {
		t.Fatal(reply.Error)
	}
	snap := s.Handle(Request{Action: "show", RecordID: "gap"}).Record.Snapshots["request_original"]
	if snap.BodyState != "truncated" || snap.Captured != 3 || snap.BodySize != 9 {
		t.Fatalf("%+v", snap)
	}
}
func TestGapPreservesOnlyContiguousPrefix(t *testing.T) {
	s := started(t, DefaultBodyLimit)
	add(t, s, "gap")
	send(t, s, Event{Type: "body", RecordID: "gap", Snapshot: "request_original", Data: []byte("abc")})
	event := Event{Type: "body", SessionID: "session", RecordID: "gap", Snapshot: "request_original", Offset: 6, Data: []byte("ghi")}
	reply := s.Handle(Request{Action: "event", SessionID: "session", Event: &event})
	if reply.Error != "" {
		t.Fatal(reply.Error)
	}
	body := s.Handle(Request{Action: "body", RecordID: "gap", Snapshot: "request_original"})
	if string(body.Body.Data) != "abc" || body.Body.State != "truncated" {
		t.Fatalf("%+v", body.Body)
	}
}
