package capture

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func started(t *testing.T, limit int) *Store {
	t.Helper()
	s := NewStore()
	r := s.Handle(Request{Action: "start", Start: &StartOptions{SessionID: "session", BodyLimit: limit}})
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	return s
}
func add(t *testing.T, s *Store, id string) {
	t.Helper()
	r := Record{ID: id, SessionID: "session", Kind: "http", Identity: Identity{SessionID: "session", Revision: 1}, Snapshots: map[string]Snapshot{"request_original": {Host: "fixture.example", Query: "token=fictional-credential", Headers: map[string][]string{"Authorization": {"Bearer fictional-credential"}}, BodyState: "collecting"}}}
	if reply := s.Handle(Request{Action: "event", SessionID: "session", Event: &Event{Type: "record", SessionID: "session", RecordID: id, Record: &r}}); reply.Error != "" {
		t.Fatal(reply.Error)
	}
}
func TestQuotaRawAndCursor(t *testing.T) {
	s := started(t, 3)
	add(t, s, "a")
	add(t, s, "b")
	q := Request{Action: "event", SessionID: "session", Event: &Event{Type: "body", SessionID: "session", RecordID: "a", Snapshot: "request_original", Data: []byte("secret")}}
	if r := s.Handle(q); r.Error != "" {
		t.Fatal(r.Error)
	}
	r := s.Handle(Request{Action: "body", RecordID: "a", Snapshot: "request_original"})
	if string(r.Body.Data) != "sec" || r.Body.State != "truncated" {
		t.Fatalf("%+v", r.Body)
	}
	show := s.Handle(Request{Action: "show", RecordID: "a"})
	b, _ := json.Marshal(show)
	if !strings.Contains(string(b), "Bearer fictional-credential") || !strings.Contains(string(b), "token=fictional-credential") {
		t.Fatal("raw values lost")
	}
	list := s.Handle(Request{Action: "list", List: &ListOptions{Limit: 1}})
	if list.NextCursor == "" {
		t.Fatal("cursor missing")
	}
	s.Handle(Request{Action: "clear", SessionID: "session"})
	if r := s.Handle(Request{Action: "list", List: &ListOptions{Cursor: list.NextCursor}}); r.Error != "capture_cursor_invalid" {
		t.Fatal(r.Error)
	}
}
func TestIsolationLifecycleAndBounds(t *testing.T) {
	s := started(t, DefaultBodyLimit)
	add(t, s, "a")
	if r := s.Handle(Request{Action: "stop", SessionID: "wrong"}); r.Error != "capture_session_mismatch" {
		t.Fatal(r.Error)
	}
	s.Handle(Request{Action: "pause", SessionID: "session"})
	if r := s.Handle(Request{Action: "event", SessionID: "session", Event: &Event{Type: "body", SessionID: "session", RecordID: "a", Data: []byte("x")}}); r.Error != "capture_not_recording" {
		t.Fatal(r.Error)
	}
	s.Handle(Request{Action: "stop", SessionID: "session"})
	r := s.Handle(Request{Action: "show", RecordID: "a"})
	if r.Record.Snapshots["request_original"].BodyState != "cutoff" {
		t.Fatal("missing collection cutoff")
	}
	if r := s.Handle(Request{Action: "body", RecordID: "a", Snapshot: "request_original", Offset: -1}); r.Error != "capture_range_invalid" {
		t.Fatal(r.Error)
	}
}
func TestRecordAndMetadataBounds(t *testing.T) {
	s := started(t, 1)
	for i := 0; i < MaxRecords; i++ {
		add(t, s, strconv.Itoa(i+1))
	}
	r := Record{ID: "overflow", SessionID: "session", Kind: "http", Identity: Identity{SessionID: "session", Revision: 1}}
	out := s.Handle(Request{Action: "event", SessionID: "session", Event: &Event{Type: "record", SessionID: "session", RecordID: r.ID, Record: &r}})
	if out.Error != "capture_record_limit" {
		t.Fatal(out.Error)
	}
	if s.Handle(Request{Action: "status"}).Status.Records != MaxRecords {
		t.Fatal("record limit")
	}
}
func TestPrivateService(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
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
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	reply, err := Call(ctx, socket, Request{Action: "start", Start: &StartOptions{}})
	if err != nil || reply.Status.SessionID == "" {
		t.Fatalf("%+v %v", reply, err)
	}
	info, _ := os.Stat(socket)
	if info.Mode().Perm() != 0600 {
		t.Fatal("socket permissions")
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}
func TestRejectSymlinkParentAndExistingSocket(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := Serve(context.Background(), filepath.Join(link, "capture.sock"), os.Getuid()); err == nil {
		t.Fatal("accepted symlink")
	}
	path := filepath.Join(dir, "capture.sock")
	if err := os.WriteFile(path, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Serve(context.Background(), path, os.Getuid()); err == nil {
		t.Fatal("replaced existing file")
	}
}
