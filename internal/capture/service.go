package capture

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var ErrUnavailable = errors.New("capture_unavailable")

// Call performs one private local operation. Errors contain fixed codes only.
func Call(ctx context.Context, socket string, q Request) (Reply, error) {
	var out Reply
	pr, pw := io.Pipe()
	go func() { err := json.NewEncoder(pw).Encode(q); _ = pw.CloseWithError(err) }()
	client := httpClient(socket)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://capture/call", pr)
	if err != nil {
		pr.Close()
		return out, ErrUnavailable
	}
	resp, err := client.Do(req)
	if err != nil {
		pr.Close()
		return out, ErrUnavailable
	}
	defer resp.Body.Close()
	if err = json.NewDecoder(io.LimitReader(resp.Body, MaxBytes*2)).Decode(&out); err != nil {
		return Reply{}, ErrUnavailable
	}
	if out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}
func httpClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}, Timeout: 10 * time.Second}
}

type peerKey struct{}

func secureParent(path string, uid int) error {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("capture_directory_invalid")
	}
	if !ownedBy(info, uid) {
		return errors.New("capture_owner_invalid")
	}
	for p := dir; ; p = filepath.Dir(p) {
		info, err = os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) || (!ownedBy(info, uid) && !ownedBy(info, 0)) {
			return errors.New("capture_directory_invalid")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

// Serve runs an owner-authenticated private Unix service until ctx is canceled.
func Serve(ctx context.Context, socket string, ownerUID int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := secureParent(socket, ownerUID); err != nil {
		return err
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		return errors.New("capture_socket_exists")
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return errors.New("capture_bind_failed")
	}
	defer ln.Close()
	if err = os.Chmod(socket, 0600); err != nil {
		return errors.New("capture_permissions_failed")
	}
	socketInfo, err := os.Lstat(socket)
	if err != nil {
		return errors.New("capture_bind_failed")
	}
	defer func() {
		current, e := os.Lstat(socket)
		if e == nil && os.SameFile(socketInfo, current) {
			_ = os.Remove(socket)
		}
	}()
	store := NewStore()
	mux := http.NewServeMux()
	slots := make(chan struct{}, 2)
	authorize := func(w http.ResponseWriter, r *http.Request) bool {
		uid, ok := r.Context().Value(peerKey{}).(int)
		if !ok || uid != ownerUID {
			http.Error(w, "capture_unauthorized", http.StatusForbidden)
			return false
		}
		select {
		case slots <- struct{}{}:
			return true
		default:
			http.Error(w, "capture_busy", http.StatusServiceUnavailable)
			return false
		}
	}
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		defer func() { <-slots }()
		if r.Method != http.MethodPost || r.ContentLength > 0 || len(r.TransferEncoding) > 0 || r.URL.RawQuery != "" {
			http.Error(w, "capture_request_invalid", 400)
			return
		}
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		cancel()
	})
	mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		defer func() { <-slots }()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			_ = json.NewEncoder(w).Encode(fail("capture_method_invalid"))
			return
		}
		var q Request
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, (3*DefaultBodyLimit)/2))
		dec.DisallowUnknownFields()
		if dec.Decode(&q) != nil {
			_ = json.NewEncoder(w).Encode(fail("capture_request_invalid"))
			return
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			_ = json.NewEncoder(w).Encode(fail("capture_request_invalid"))
			return
		}
		_ = json.NewEncoder(w).Encode(store.Handle(q))
	})
	registerArchive(mux, store, authorize, slots)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, MaxHeaderBytes: 4096, ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		uid, e := peerUID(c)
		if e != nil {
			uid = -1
		}
		return context.WithValue(ctx, peerKey{}, uid)
	}}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
			_ = srv.Close()
		case <-done:
		}
	}()
	err = srv.Serve(ln)
	close(done)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return errors.New("capture_service_failed")
}

// Sink never blocks traffic producers; its one MiB queue has one delivery worker.
// Rejected events mean collection cutoff; callers keep forwarding the traffic.
type Sink struct {
	mu              sync.Mutex
	socket, session string
	queue           chan sinkEvent
	bytes           int
	closed          bool
	done            chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
}

var sinkQueued atomic.Int64

type sinkEvent struct {
	event Event
	bytes int
}

func NewSink(socket, sessionID string) *Sink {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sink{socket: socket, session: sessionID, queue: make(chan sinkEvent, 64), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	go s.run()
	return s
}
func (s *Sink) Offer(e Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if e.SessionID == "" {
		e.SessionID = s.session
	}
	if e.SessionID != s.session || len(e.Data) > MaxChunk {
		return false
	}
	n := int(size(e))
	if n > DefaultBodyLimit || s.bytes+n > DefaultBodyLimit {
		return false
	}
	for {
		old := sinkQueued.Load()
		if old+int64(n) > DefaultBodyLimit {
			return false
		}
		if sinkQueued.CompareAndSwap(old, old+int64(n)) {
			break
		}
	}
	e = clone(e)
	select {
	case s.queue <- sinkEvent{e, n}:
		s.bytes += n
		return true
	default:
		sinkQueued.Add(-int64(n))
		return false
	}
}
func (s *Sink) run() {
	defer close(s.done)
	defer func() {
		for e := range s.queue {
			sinkQueued.Add(-int64(e.bytes))
		}
	}()
	for e := range s.queue {
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
		_, _ = Call(ctx, s.socket, Request{Action: "event", SessionID: s.session, Event: &e.event})
		cancel()
		s.mu.Lock()
		s.bytes -= e.bytes
		sinkQueued.Add(-int64(e.bytes))
		s.mu.Unlock()
		if s.ctx.Err() != nil {
			return
		}
	}
}
func (s *Sink) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		s.cancel()
		<-s.done
	}
	s.cancel()
}

// Shutdown explicitly ends the private memory-only recorder.
func Shutdown(ctx context.Context, socket string) error {
	c := httpClient(socket)
	defer c.CloseIdleConnections()
	q, e := http.NewRequestWithContext(ctx, http.MethodPost, "http://capture/shutdown", nil)
	if e != nil {
		return ErrUnavailable
	}
	r, e := c.Do(q)
	if e != nil {
		return ErrUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	return nil
}
