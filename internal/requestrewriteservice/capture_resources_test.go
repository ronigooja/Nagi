package requestrewriteservice

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ronigooja/Nagi/internal/capture"
)

// Opt in: NAGI_CAPTURE_RESOURCE_DURATION=3m go test ./internal/requestrewriteservice -run '^TestCaptureSustainedResources$' -count=1 -v
// This exercises local service/recorder sockets, never TUN or host routes.
func TestCaptureSustainedResources(t *testing.T) {
	raw := os.Getenv("NAGI_CAPTURE_RESOURCE_DURATION")
	if raw == "" {
		t.Skip("set NAGI_CAPTURE_RESOURCE_DURATION (at least 3m) for sustained acceptance")
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration < 3*time.Minute {
		t.Fatal("resource duration must be at least 3m")
	}
	payload := bytes.Repeat([]byte("original-content-"), 16384) // exactly one MaxChunk
	var httpCount, wsCount, streamBytes atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws":
			c, e := websocket.Accept(w, r, nil)
			if e != nil {
				return
			}
			defer c.CloseNow()
			c.SetReadLimit(int64(len(payload) * 2))
			for {
				k, b, e := c.Read(r.Context())
				if e != nil {
					return
				}
				if c.Write(r.Context(), k, b) != nil {
					return
				}
			}
		case "/stream":
			w.Header().Set("Content-Type", "application/octet-stream")
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if _, e := w.Write(payload[:4096]); e != nil {
						return
					}
					w.(http.Flusher).Flush()
				}
			}
		default:
			b, e := io.ReadAll(r.Body)
			if e != nil || !bytes.Equal(b, []byte("unchanged-request")) {
				t.Error("original request changed", e)
				return
			}
			_, _ = w.Write(payload)
		}
	}))
	defer origin.Close()
	f := enableCapture(t, newFixtureWithTimeout(t, strings.TrimPrefix(origin.URL, "http://"), duration+10*time.Second, []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), capture.DefaultBodyLimit)
	tr := &http.Transport{MaxIdleConnsPerHost: 4, DialContext: func(context.Context, string, string) (net.Conn, error) {
		c := f.handoff(t, "http")
		_ = c.SetDeadline(time.Time{})
		return c, nil
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	var wg sync.WaitGroup
	fail := func(e error) {
		if ctx.Err() == nil {
			t.Error(e)
			cancel()
		}
	}
	// One HTTP response stays open throughout the run, independent of list clients.
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://api.service.example/stream", nil)
		resp, e := client.Do(req)
		if e != nil {
			fail(fmt.Errorf("long HTTP stream: %w", e))
			return
		}
		defer resp.Body.Close()
		b := make([]byte, 4096)
		var offset int
		for {
			n, e := resp.Body.Read(b)
			for i := 0; i < n; i++ {
				if b[i] != payload[(offset+i)%4096] {
					fail(fmt.Errorf("long stream changed at %d", offset+i))
					return
				}
			}
			offset += n
			streamBytes.Add(int64(n))
			if e != nil {
				if e != io.EOF {
					fail(fmt.Errorf("long HTTP stream: %w", e))
				}
				return
			}
		}
	}()
	// Keep one parent pinned while messages and HTTP records rotate around it.
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, _, e := websocket.Dial(ctx, "ws://api.service.example/ws", &websocket.DialOptions{HTTPClient: client})
		if e != nil {
			fail(fmt.Errorf("WebSocket: %w", e))
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(int64(len(payload) * 2))
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				b := []byte("bidirectional-original")
				if wsCount.Load()%10 == 0 {
					b = payload
				}
				if e = c.Write(ctx, websocket.MessageBinary, b); e != nil {
					fail(fmt.Errorf("WebSocket: %w", e))
					return
				}
				_, got, e := c.Read(ctx)
				if e != nil {
					fail(fmt.Errorf("WebSocket: %w", e))
					return
				}
				if !bytes.Equal(got, b) {
					fail(fmt.Errorf("WebSocket forwarding changed"))
					return
				}
				wsCount.Add(1)
			}
		}
	}()
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(80 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					req, _ := http.NewRequestWithContext(ctx, "POST", "http://api.service.example/rotate", strings.NewReader("unchanged-request"))
					resp, e := client.Do(req)
					if e != nil {
						fail(fmt.Errorf("rotating HTTP: %w", e))
						return
					}
					got, e := io.ReadAll(resp.Body)
					resp.Body.Close()
					if e != nil {
						fail(fmt.Errorf("rotating HTTP: %w", e))
						return
					}
					if !bytes.Equal(got, payload) {
						fail(fmt.Errorf("HTTP forwarding changed"))
						return
					}
					httpCount.Add(1)
				}
			}
		}()
	}
	// First minute has no UI polling; later a slow reader repeatedly searches and
	// follows cursors while writes and evictions continue. Busy slots are expected.
	var searches atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		timer := time.NewTimer(duration / 3)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		cursor := ""
		tick := time.NewTicker(700 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				r, e := capture.Call(ctx, f.socket, capture.Request{Action: "list", List: &capture.ListOptions{Limit: 7, Query: "original", Cursor: cursor}})
				if e != nil {
					continue
				}
				cursor = r.NextCursor
				searches.Add(1)
				for _, rec := range r.Records {
					for _, s := range rec.Snapshots {
						if s.BodyState == "complete" && s.Captured != s.BodySize {
							fail(fmt.Errorf("discarded bytes claimed complete"))
							return
						}
					}
				}
			}
		}
	}()
	type sample struct {
		heap, rss, logical uint64
		goroutines, fd     int
	}
	var samples []sample
	started := time.Now()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	collect := func() {
		var st *capture.Status
		for attempt := 0; attempt < 20; attempt++ {
			r, e := capture.Call(context.Background(), f.socket, capture.Request{Action: "status"})
			if e == nil {
				st = r.Status
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if st == nil {
			t.Error("status unavailable")
			return
		}
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		var usage syscall.Rusage
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
		cpu := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
		s := sample{heap: m.HeapAlloc, logical: uint64(st.Bytes), goroutines: runtime.NumGoroutine(), fd: -1}
		if data, e := os.ReadFile("/proc/self/statm"); e == nil {
			fields := strings.Fields(string(data))
			if len(fields) > 1 {
				pages, _ := strconv.ParseUint(fields[1], 10, 64)
				s.rss = pages * uint64(os.Getpagesize())
			}
		}
		if entries, e := os.ReadDir("/proc/self/fd"); e == nil {
			s.fd = len(entries)
		}
		samples = append(samples, s)
		t.Logf("sample elapsed=%s records=%d logical=%d heap_after_gc=%d rss=%d goroutines=%d fd=%d dropped=%d truncated=%d http=%d ws=%d stream_bytes=%d cpu_seconds=%.2f", time.Since(started).Round(time.Second), st.Records, st.Bytes, s.heap, s.rss, s.goroutines, s.fd, st.Dropped, st.Truncated, httpCount.Load(), wsCount.Load(), streamBytes.Load(), cpu)
		if st.Records > capture.MaxRecords || st.Bytes > capture.MaxBytes {
			t.Error("recorder budget exceeded")
		}
	}
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-tick.C:
			collect()
		}
	}
	wg.Wait()
	collect()
	if httpCount.Load() < 1000 || wsCount.Load() < 100 || streamBytes.Load() < capture.DefaultBodyLimit || searches.Load() < 10 {
		t.Fatalf("insufficient sustained workload: http=%d ws=%d stream=%d searches=%d", httpCount.Load(), wsCount.Load(), streamBytes.Load(), searches.Load())
	}
	// Compare equal saturated windows, after GC. This is a duration-growth check,
	// not a physical/RSS ceiling: allocator caches, TLS and test fixtures add memory.
	if len(samples) < 10 {
		t.Fatal("insufficient samples")
	}
	peak := func(window []sample) sample {
		var out sample
		for _, s := range window {
			if s.heap > out.heap {
				out.heap = s.heap
			}
			if s.goroutines > out.goroutines {
				out.goroutines = s.goroutines
			}
			if s.fd > out.fd {
				out.fd = s.fd
			}
		}
		return out
	}
	middle := peak(samples[len(samples)/3 : 2*len(samples)/3])
	late := peak(samples[2*len(samples)/3:])
	allowance := uint64(capture.MaxBytes / 2)
	if late.heap > middle.heap+allowance {
		t.Errorf("heap keeps growing after saturation: middle_peak=%d late_peak=%d", middle.heap, late.heap)
	}
	if late.goroutines > middle.goroutines+32 {
		t.Errorf("goroutines accumulate: middle_peak=%d late_peak=%d", middle.goroutines, late.goroutines)
	}
	if late.fd > 0 && middle.fd > 0 && late.fd > middle.fd+16 {
		t.Errorf("descriptors accumulate: middle_peak=%d late_peak=%d", middle.fd, late.fd)
	}
	t.Logf("summary http=%d websocket_roundtrips=%d long_stream_bytes=%d searches=%d samples=%d", httpCount.Load(), wsCount.Load(), streamBytes.Load(), searches.Load(), len(samples))
}

type oneByteCaptureReader struct{ io.Reader }

func (r oneByteCaptureReader) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return r.Reader.Read(b)
}

func TestCaptureResourceDiscardedBytes(t *testing.T) {
	if os.Getenv("NAGI_CAPTURE_RESOURCE_DURATION") == "" {
		t.Skip("opt-in resource acceptance")
	}
	f := enableCapture(t, newFixture(t, "127.0.0.1:1", []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), capture.DefaultBodyLimit)
	req, _ := http.NewRequest("POST", "http://api.service.example/tiny", nil)
	identity := capture.Identity{SessionID: f.policy.SessionID, Revision: f.policy.Revision}
	record := f.s.beginCapture(&connectionContext{service: f.s, identity: &identity}, req, "http", "", "", 0)
	for deadline := time.Now().Add(time.Second); ; {
		if _, e := capture.Call(context.Background(), f.socket, capture.Request{Action: "show", RecordID: record.record.ID}); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("record not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	payload := bytes.Repeat([]byte("x"), 65536)
	body := record.body("request_original", io.NopCloser(oneByteCaptureReader{bytes.NewReader(payload)}), int64(len(payload)), func() http.Header { return nil })
	got, e := io.ReadAll(body)
	_ = body.Close()
	if e != nil || !bytes.Equal(got, payload) {
		t.Fatal("discarded recorder events changed forwarded bytes", e)
	}
	f.s.captureSink.Close()
	snap := f.call(t, capture.Request{Action: "show", RecordID: record.record.ID}).Record.Snapshots["request_original"]
	if snap.Captured >= int64(len(payload)) {
		t.Fatal("workload did not overload queue")
	}
	if snap.BodyState == "complete" {
		t.Fatalf("discarded chunks falsely complete: %+v", snap)
	}
	t.Logf("one_byte_reads=%d retained=%d body_state=%s forwarded_unchanged=true", len(payload), snap.Captured, snap.BodyState)
}
