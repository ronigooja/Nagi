package requestrewriteservice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/ronigooja/Nagi/internal/capture"
)

type captureKey struct{}

func validateCaptureConfiguration(c Configuration) error {
	if c.Capture == nil {
		if c.RecorderSocket != "" || c.BodyLimit != 0 {
			return errInvalid
		}
		return nil
	}
	if !validCapturePolicy(*c.Capture) || !filepath.IsAbs(c.RecorderSocket) || c.BodyLimit < 0 || c.BodyLimit > capture.MaxBodyLimit {
		return errInvalid
	}
	return nil
}
func validCapturePolicy(p capture.Policy) bool {
	return p.SessionID != "" && len(p.SessionID) <= 128 && p.Revision > 0 && (p.State == "recording" || p.State == "paused" || p.State == "stopped")
}

// SetCapture updates collection independently of the immutable rule generation.
// Pausing never cancels application streams or their upstream operations.
func (s *Service) SetCapture(p capture.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validCapturePolicy(p) || !s.configured || s.captureSink == nil || s.capturePolicy == nil || p.SessionID != s.capturePolicy.SessionID {
		return errInvalid
	}
	old := s.capturePolicy
	if reflect.DeepEqual(*old, p) {
		return nil
	}
	if p.Revision != old.Revision+1 {
		return errInvalid
	}
	cp := p
	s.capturePolicy = &cp
	return nil
}
func (s *Service) captureIdentity(value string) (*capture.Identity, error) {
	if value == "" {
		return nil, nil
	}
	s.mu.Lock()
	configured := s.capturePolicy != nil
	s.mu.Unlock()
	if !configured || len(value) > 12<<10 {
		return nil, errAuth
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(b) != value || !uniqueJSON(b) || !exactKeys(b, reflect.TypeOf(capture.Identity{})) {
		return nil, errAuth
	}
	var identity capture.Identity
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&identity) != nil || identity.SessionID == "" || identity.Revision == 0 || identity.Source == "" || identity.Destination == "" {
		return nil, errAuth
	}
	s.mu.Lock()
	p := *s.capturePolicy
	s.mu.Unlock()
	if identity.SessionID != p.SessionID || identity.Revision > p.Revision {
		return nil, errAuth
	}
	return &identity, nil
}
func (c *connectionContext) authorizedRequest(req *http.Request, scheme string) bool {
	if c.service.snapshot.Authorized(req.URL.Hostname()) {
		return true
	}
	return c.identity != nil && sameAuthority(req.URL.Host, c.authority, scheme) && sameAuthority(req.Host, c.authority, scheme) && (c.protocol == "tls") == (scheme == "https")
}

type captureRecord struct {
	mu      sync.Mutex
	service *Service
	sink    *capture.Sink
	policy  capture.Policy
	record  capture.Record
	limit   int64
}

func (s *Service) beginCapture(cc *connectionContext, req *http.Request, kind, parent, stage string, sequence uint64) *captureRecord {
	if cc.identity == nil {
		return nil
	}
	s.mu.Lock()
	if s.capturePolicy == nil || s.capturePolicy.State != "recording" || s.captureSink == nil {
		s.mu.Unlock()
		return nil
	}
	p := *s.capturePolicy
	sink := s.captureSink
	limit := s.config.BodyLimit
	s.mu.Unlock()
	if p.SessionID != cc.identity.SessionID {
		return nil
	}
	if limit == 0 {
		limit = capture.DefaultBodyLimit
	}
	identity := *cc.identity
	identity.Revision = p.Revision
	rid, err := randomValue(18)
	if err != nil {
		return nil
	}
	c := &captureRecord{service: s, sink: sink, policy: p, limit: int64(limit), record: capture.Record{ID: rid, SessionID: p.SessionID, ParentID: parent, Kind: kind, Stage: stage, Sequence: sequence, Generation: s.config.Generation, RuleVersion: s.snapshot.Version(), Identity: identity, Started: time.Now(), RuleIDs: []string{}, Snapshots: map[string]capture.Snapshot{}}}
	if kind == "http" {
		c.record.Snapshots["request_original"] = requestSnapshot(req)
	}
	c.upsert()
	return c
}
func requestSnapshot(r *http.Request) capture.Snapshot {
	port, _ := strconv.Atoi(r.URL.Port())
	if port == 0 {
		port = 80
		if r.URL.Scheme == "https" {
			port = 443
		}
	}
	h := r.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	h["Host"] = []string{r.Host}
	return capture.Snapshot{Scheme: r.URL.Scheme, Host: r.URL.Hostname(), Port: port, Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Headers: h, BodyState: "collecting"}
}
func (c *captureRecord) active() bool {
	c.service.mu.Lock()
	defer c.service.mu.Unlock()
	p := c.service.capturePolicy
	return p != nil && p.SessionID == c.policy.SessionID && p.Revision == c.policy.Revision && p.State == "recording"
}
func (c *captureRecord) offer(e capture.Event) bool {
	if c == nil || !c.active() {
		return false
	}
	e.SessionID = c.policy.SessionID
	e.RecordID = c.record.ID
	return c.sink.Offer(e)
}
func (c *captureRecord) upsert() { r := c.record; c.offer(capture.Event{Type: "record", Record: &r}) }
func (c *captureRecord) result(ids []string, outcome string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record.RuleIDs = append(c.record.RuleIDs, ids...)
	if outcome != "" {
		c.record.Outcome = outcome
	}
	c.upsert()
}
func (c *captureRecord) requestActual(r *http.Request, ids []string, outcome string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record.Snapshots["request_actual"] = requestSnapshot(r)
	c.record.RuleIDs = append(c.record.RuleIDs, ids...)
	c.record.Outcome = outcome
	c.upsert()
}
func (c *captureRecord) response(name string, r *http.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record.Snapshots[name] = capture.Snapshot{Status: r.StatusCode, Headers: r.Header.Clone(), BodyState: "collecting"}
	c.upsert()
}
func (c *captureRecord) complete(outcome, code string, closeCode int) {
	if c == nil {
		return
	}
	c.offer(capture.Event{Type: "complete", Outcome: outcome, Error: code, CloseCode: closeCode, DurationMS: time.Since(c.record.Started).Milliseconds()})
}

// Each boundary has its own reader. When a transform leaves a body untouched,
// nesting these readers records each consumed byte once at each boundary.
func (c *captureRecord) body(name string, body io.ReadCloser, trailers func() http.Header) io.ReadCloser {
	if body == nil || body == http.NoBody {
		c.offer(capture.Event{Type: "finish", Snapshot: name, State: "complete", Trailers: trailers()})
		return body
	}
	return &captureBody{ReadCloser: body, record: c, name: name, trailers: trailers}
}

type captureBody struct {
	io.ReadCloser
	record   *captureRecord
	name     string
	trailers func() http.Header
	once     sync.Once
	bytes    int64
	cutoff   bool
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.append(p[:n])
	}
	if err != nil {
		state := "complete"
		if err != io.EOF {
			state = "failed"
		}
		b.finish(state)
	}
	return n, err
}
func (b *captureBody) append(p []byte) {
	for len(p) > 0 {
		n := len(p)
		if n > capture.MaxChunk {
			n = capture.MaxChunk
		}
		// Send only the bounded prefix and one overflow byte to mark truncation.
		remaining := b.record.limit + 1 - b.bytes
		if remaining <= 0 {
			return
		}
		if int64(n) > remaining {
			n = int(remaining)
		}
		if !b.cutoff && !b.record.offer(capture.Event{Type: "body", Snapshot: b.name, Data: p[:n]}) {
			b.cutoff = true
		}
		b.bytes += int64(n)
		p = p[n:]
	}
}
func (b *captureBody) finish(state string) {
	b.once.Do(func() {
		if b.cutoff {
			state = "cutoff"
		}
		if b.bytes > b.record.limit {
			state = "truncated"
		}
		b.record.offer(capture.Event{Type: "finish", Snapshot: b.name, State: state, Trailers: b.trailers()})
	})
}
func (b *captureBody) Close() error { err := b.ReadCloser.Close(); b.finish("cutoff"); return err }

type captureWriter struct {
	io.Writer
	body *captureBody
}

func (w captureWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n > 0 {
		w.body.append(p[:n])
	}
	return n, err
}

func (c *captureRecord) messageSnapshot(name, kind string) *captureBody {
	c.mu.Lock()
	c.record.Snapshots[name] = capture.Snapshot{MessageType: kind, Headers: map[string][]string{}, BodyState: "collecting"}
	c.upsert()
	c.mu.Unlock()
	return &captureBody{record: c, name: name, trailers: func() http.Header { return nil }}
}

// Capture metadata never comes from application HTTP headers. Only the outer
// authenticated engine handoff populates connectionContext.identity.
type websocketCaptureKey struct{}
type websocketCapture struct {
	cc     *connectionContext
	parent string
}
type captureReader struct {
	io.Reader
	body *captureBody
}

func (r *captureReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.body.append(p[:n])
	}
	if err != nil {
		state := "complete"
		if err != io.EOF {
			state = "failed"
		}
		r.body.finish(state)
	}
	return n, err
}
func captureFinish(name string) capture.Event {
	return capture.Event{Type: "finish", Snapshot: name, State: "complete"}
}
