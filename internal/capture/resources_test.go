package capture

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// Opt in alongside TestCaptureSustainedResources. These finite adversarial
// phases isolate allocation and admission limits from network coalescing.
func TestCaptureResourceAdversaries(t *testing.T) {
	if os.Getenv("NAGI_CAPTURE_RESOURCE_DURATION") == "" {
		t.Skip("opt-in resource acceptance")
	}
	event := func(s *Store, e Event) Reply {
		e.SessionID = "session"
		return s.Handle(Request{Action: "event", SessionID: "session", Event: &e})
	}
	t.Run("tiny-and-max-chunks", func(t *testing.T) {
		for _, chunk := range []int{1, MaxChunk} {
			s := started(t, MaxBodyLimit)
			add(t, s, "blocks")
			n := MaxChunk
			if chunk == 1 {
				n = 65536
			}
			data := bytes.Repeat([]byte("x"), chunk)
			for offset := 0; offset < n; offset += chunk {
				if r := event(s, Event{Type: "body", RecordID: "blocks", Snapshot: "request_original", Data: data, Offset: int64(offset)}); r.Error != "" {
					t.Fatal(r.Error)
				}
			}
			b := s.bodies["blocks"]["request_original"]
			// Inspect retained allocations, not just byte-length: tiny events must not
			// leave one heap object per event. Allow one final block of slack.
			if b.allocated() > int64(n)+bodyBlockCost+int64(n/16384)*32 {
				t.Fatalf("tiny chunks retain excess capacity: input=%d allocated=%d", n, b.allocated())
			}
			if got := s.Handle(Request{Action: "body", RecordID: "blocks", Snapshot: "request_original", Limit: n}).Body.Data; !bytes.Equal(got, bytes.Repeat([]byte("x"), n)) {
				t.Fatal("coalescing changed body")
			}
			t.Logf("chunk=%d retained_body_bytes=%d allocation_bytes=%d blocks=%d", chunk, n, b.allocated(), b.blocks)
		}
	})
	t.Run("byte-budget-rotation", func(t *testing.T) {
		s := started(t, MaxBodyLimit)
		data := bytes.Repeat([]byte("b"), MaxChunk)
		for i := 0; i < 100; i++ {
			id := "budget" + strconv.Itoa(i)
			add(t, s, id)
			for j := 0; j < 4; j++ {
				if r := event(s, Event{Type: "body", RecordID: id, Snapshot: "request_original", Data: data, Offset: int64(j * MaxChunk)}); r.Error != "" {
					t.Fatal(r.Error)
				}
			}
			if r := event(s, Event{Type: "finish", RecordID: id, Snapshot: "request_original", State: "complete", BodySize: DefaultBodyLimit}); r.Error != "" {
				t.Fatal(r.Error)
			}
			if s.bytes > MaxBytes-8*DefaultBodyLimit {
				t.Fatal("reserved ingress budget consumed")
			}
		}
		if s.status.Dropped < 40 || len(s.records) >= 100 {
			t.Fatal("byte budget did not rotate history")
		}
		t.Logf("logical_bytes=%d retained_records=%d evictions=%d", s.bytes, len(s.records), s.status.Dropped)
	})
	t.Run("pinned-parents-release", func(t *testing.T) {
		s := started(t, 1)
		for i := 0; i < MaxRecords; i++ {
			id := "parent" + strconv.Itoa(i)
			r := Record{ID: id, SessionID: "session", Kind: "http", Outcome: "upgraded", Identity: Identity{SessionID: "session", Revision: 1}, Snapshots: map[string]Snapshot{}}
			if out := event(s, Event{Type: "record", RecordID: id, Record: &r}); out.Error != "" {
				t.Fatal(out.Error)
			}
		}
		incoming := Record{ID: "overflow", SessionID: "session", Kind: "http", Outcome: "complete", Identity: Identity{SessionID: "session", Revision: 1}, Snapshots: map[string]Snapshot{}}
		for i := 0; i < 1000; i++ {
			if r := event(s, Event{Type: "record", RecordID: incoming.ID, Record: &incoming}); r.Error != "capture_record_limit" {
				t.Fatal("active parents not retained", r.Error)
			}
		}
		if len(s.records) != MaxRecords {
			t.Fatal("active parent count escaped bound")
		}
		event(s, Event{Type: "complete", RecordID: "parent0", Outcome: "closed"})
		if r := event(s, Event{Type: "record", RecordID: incoming.ID, Record: &incoming}); r.Error != "" {
			t.Fatal("closed parent failed to release", r.Error)
		}
		if _, ok := s.records["parent0"]; ok {
			t.Fatal("closed parent still pinned")
		}
		t.Logf("active_parent_limit=%d rejected=%d closed_parent_released=true", len(s.records), s.status.Dropped)
	})
	t.Run("slow-recorder-queue", func(t *testing.T) {
		dir := t.TempDir()
		socket := filepath.Join(dir, "slow.sock")
		ln, e := net.Listen("unix", socket)
		if e != nil {
			t.Fatal(e)
		}
		release := make(chan struct{})
		entered := make(chan struct{}, 1)
		var calls atomic.Int64
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			_, _ = io.WriteString(w, `{"sensitive":true}`)
		})}
		go srv.Serve(ln)
		defer srv.Close()
		defer close(release)
		sink := NewSink(socket, "session")
		defer sink.Close()
		before := runtime.NumGoroutine()
		first := Event{Type: "body", RecordID: "queue", Snapshot: "request_original", Data: bytes.Repeat([]byte("q"), MaxChunk)}
		if !sink.Offer(first) {
			t.Fatal("first offer rejected")
		}
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("worker not connected")
		}
		rejected := 0
		for i := 0; i < 128; i++ {
			if !sink.Offer(first) {
				rejected++
			}
		}
		tiny := first
		tiny.Data = []byte("q")
		for i := 0; i < 10000; i++ {
			if !sink.Offer(tiny) {
				rejected++
			}
		}
		sink.mu.Lock()
		queued, n := len(sink.queue), sink.bytes
		sink.mu.Unlock()
		global := sinkQueued.Load()
		if rejected == 0 || queued > 64 || n > DefaultBodyLimit || global > DefaultBodyLimit {
			t.Fatalf("queue failed bounds: reject=%d queue=%d bytes=%d global=%d", rejected, queued, n, global)
		}
		if delta := runtime.NumGoroutine() - before; delta > 12 {
			t.Fatalf("offers spawn waiting goroutines: delta=%d", delta)
		}
		t.Logf("rejected=%d queue_events=%d queue_serialized_bytes=%d global_serialized_bytes=%d worker_calls=%d goroutine_delta=%d", rejected, queued, n, global, calls.Load(), runtime.NumGoroutine()-before)
		// Cancellation must also return the process-wide queue accounting to zero.
		sink.cancel()
		sink.Close()
		if sinkQueued.Load() != 0 {
			t.Fatal("queue budget leaked after cancellation")
		}
	})
}
