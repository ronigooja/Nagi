package requestrewrite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ScriptExecutor executes a deterministic, isolated JavaScript invocation. It is
// injected explicitly; validation and final authority checks stay in this package.
type ScriptExecutor func(context.Context, string, json.RawMessage) (json.RawMessage, error)

var scriptExecutor struct {
	sync.RWMutex
	run ScriptExecutor
}

func SetScriptExecutor(run ScriptExecutor) {
	scriptExecutor.Lock()
	scriptExecutor.run = run
	scriptExecutor.Unlock()
}

type ResponseResult struct {
	Response    *http.Response `json:"-"`
	RuleIDs     []string       `json:"rule_ids"`
	Outcome     string         `json:"outcome"`
	RuleVersion string         `json:"rule_version"`
}
type Message struct {
	Type string `json:"type"`
	Body []byte `json:"-"`
}
type MessageResult struct {
	Message     Message  `json:"-"`
	RuleIDs     []string `json:"rule_ids"`
	Outcome     string   `json:"outcome"`
	RuleVersion string   `json:"rule_version"`
}

func hasString(vs []string, v string) bool {
	for _, s := range vs {
		if s == v {
			return true
		}
	}
	return false
}
func headerValues(h http.Header, name string) []string {
	vs := []string{}
	for k, v := range h {
		if strings.EqualFold(k, name) {
			vs = append(vs, v...)
		}
	}
	return vs
}
func matchPairs(want map[string][]string, values func(string) []string) bool {
	for key, candidates := range want {
		found := false
		for _, v := range values(key) {
			if hasString(candidates, v) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func (s *Snapshot) matchesRequest(r Rule, req *http.Request) bool {
	if !r.Enabled || req == nil || req.URL == nil || !patternMatches(r.Match.Domain, strings.TrimSuffix(strings.ToLower(req.URL.Hostname()), ".")) {
		return false
	}
	m := r.Match
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if len(m.Methods) > 0 && !hasString(m.Methods, req.Method) || !strings.HasPrefix(path, m.PathPrefix) {
		return false
	}
	if m.PathRegex != "" && !s.regex[m.PathRegex].MatchString(path) {
		return false
	}
	return matchPairs(m.Query, func(k string) []string { return req.URL.Query()[k] }) && matchPairs(m.Headers, func(k string) []string { return headerValues(req.Header, k) })
}
func (s *Snapshot) matchesResponse(r Rule, resp *http.Response) bool {
	m := r.Match
	if resp == nil {
		return len(m.StatusCodes) == 0 && len(m.ContentTypes) == 0 && len(m.ResponseHeaders) == 0
	}
	if len(m.StatusCodes) > 0 {
		found := false
		for _, v := range m.StatusCodes {
			found = found || v == resp.StatusCode
		}
		if !found {
			return false
		}
	}
	if len(m.ContentTypes) > 0 {
		media, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if e != nil || !hasString(m.ContentTypes, media) {
			return false
		}
	}
	return matchPairs(m.ResponseHeaders, func(k string) []string { return headerValues(resp.Header, k) })
}
func (s *Snapshot) script(id string) Script {
	for _, v := range s.doc.Scripts {
		if v.ID == id {
			return v
		}
	}
	return Script{}
}
func (s *Snapshot) needsBody(o Operation) bool {
	return bodyOperation(o) || o.Op == "script" && s.script(o.Script).BodyAccess == "buffer"
}
func (s *Snapshot) ruleBody(r Rule) bool {
	for _, o := range r.Operations {
		if s.needsBody(o) {
			return true
		}
	}
	return false
}

// RangeMode is resolved from original request conditions before any upstream
// send. Response predicates cannot force speculative/repeated requests.
func (s *Snapshot) RangeMode(req *http.Request) (string, error) {
	mode := ""
	for _, r := range s.doc.Rules {
		if (r.Stage != StageRequest && r.Stage != StageResponse) || !s.ruleBody(r) || !s.matchesRequest(r, req) {
			continue
		}
		if mode != "" && mode != r.RangeMode {
			return "", &Error{Code: "rewrite_range_conflict"}
		}
		mode = r.RangeMode
	}
	if mode == "" {
		mode = "fragment"
	}
	return mode, nil
}
func (s *Snapshot) Transform(ctx context.Context, r *http.Request) (Result, error) {
	return s.transform(ctx, r, false)
}
func (s *Snapshot) TransformUpgrade(ctx context.Context, r *http.Request) (Result, error) {
	return s.transform(ctx, r, true)
}

// A reservation accounts for the logical conversion workspace, including the
// retained original and working representation. It does not bound process RSS.
var bodyMemory = make(chan struct{}, 1)

type bodyTransaction struct {
	original                     []byte
	current                      []byte
	complete, consumed, reserved bool
	once                         sync.Once
}

func (b *bodyTransaction) release() {
	b.once.Do(func() {
		if b.reserved {
			<-bodyMemory
		}
	})
}
func (b *bodyTransaction) read(ctx context.Context, r io.ReadCloser) error {
	if b.reserved {
		return nil
	}
	select {
	case bodyMemory <- struct{}{}:
		b.reserved = true
	default:
		return &Error{Code: "rewrite_limit"}
	}
	if r == nil {
		b.complete = true
		b.original = []byte{}
		b.current = []byte{}
		return nil
	}
	b.consumed = true
	raw, e := ReadBodyBounded(ctx, r)
	if e != nil {
		return e
	}
	b.complete = true
	b.original = raw
	b.current = append([]byte{}, raw...)
	return nil
}
func (b *bodyTransaction) canRecover() bool { return !b.consumed || b.complete }
func (b *bodyTransaction) reader(original bool) io.ReadCloser {
	v := b.current
	if original {
		v = b.original
	}
	return io.NopCloser(bytes.NewReader(v))
}
func limitedRead(r io.Reader) ([]byte, error) {
	v, e := io.ReadAll(io.LimitReader(r, MaxBodyBytes+1))
	if e != nil {
		return nil, e
	}
	if len(v) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	return v, nil
}
func validHeaders(h http.Header, limit int) bool {
	n := 0
	seen := map[string]bool{}
	for k, vs := range h {
		key := strings.ToLower(k)
		if !token(k) || seen[key] {
			return false
		}
		seen[key] = true
		for _, v := range vs {
			if !cleanValue(v) {
				return false
			}
			n += len(k) + len(v) + 4
		}
	}
	return n <= limit
}
func applyHeader(h http.Header, o Operation) {
	v, _ := stringValue(o)
	switch o.Op {
	case "header_set":
		deleteHeader(h, o.Name)
		h.Set(o.Name, v)
	case "header_append":
		h.Add(o.Name, v)
	case "header_delete":
		deleteHeader(h, o.Name)
	}
}
func invalidate(h http.Header) {
	for _, k := range []string{"Content-Length", "Transfer-Encoding", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest", "ETag"} {
		deleteHeader(h, k)
	}
}
func transformFailure(e error, id string) error {
	var de *Error
	if errors.As(e, &de) {
		return &Error{Code: de.Code, RuleID: id}
	}
	return &Error{Code: "rewrite_transform", RuleID: id}
}
func stageContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}

func (s *Snapshot) transform(parent context.Context, original *http.Request, upgrade bool) (Result, error) {
	out := Result{RuleIDs: []string{}, Outcome: "unchanged", RuleVersion: s.version}
	ctx, cancel := stageContext(parent)
	defer cancel()
	if parent.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	if ctx.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	if original == nil || !token(original.Method) || !headersValid(original) {
		return out, invalid()
	}
	targetOriginal, e := requestTarget(original.URL)
	if e != nil || !s.Authorized(targetOriginal.host) {
		return out, &Error{Code: "rewrite_auth"}
	}
	if original.Host != "" {
		u := *original.URL
		u.Host = original.Host
		ht, e := requestTarget(&u)
		if e != nil || ht != targetOriginal {
			return out, invalid()
		}
	}
	path := original.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if validateURLValue("path", path) != nil || validateURLValue("query", original.URL.RawQuery) != nil {
		return out, invalid()
	}
	mode, e := s.RangeMode(original)
	if e != nil {
		return out, e
	}
	out.RangeMode = mode
	work := cloneRequest(parent, original)
	work.RequestURI = ""
	contributors := []Rule{}
	body := &bodyTransaction{}
	defer body.release()
	trailers := []Operation{}
	lastBody := Rule{}
	fallback := func(rule Rule, e error) (Result, error) {
		if rule.OnError == "forward_original" && body.canRecover() && ctx.Err() == nil {
			r := cloneRequest(parent, original)
			if body.consumed {
				r.Body = body.reader(true)
			}
			out.Request = r
			out.Outcome = "forward_original"
			return out, nil
		}
		return out, transformFailure(e, rule.ID)
	}
	for _, r := range s.doc.Rules {
		if r.Stage != StageRequest || !s.matchesRequest(r, original) {
			continue
		}
		out.RuleIDs = append(out.RuleIDs, r.ID)
		for _, o := range r.Operations {
			if ctx.Err() != nil || parent.Err() != nil {
				return out, &Error{Code: "rewrite_cancelled", RuleID: r.ID}
			}
			if upgrade && s.needsBody(o) {
				continue
			}
			e = nil
			switch o.Op {
			case "header_set", "header_append", "header_delete":
				applyHeader(work.Header, o)
			case "url_set":
				e = setURL(work, o)
				if o.Component == "scheme" || o.Component == "host" || o.Component == "port" {
					contributors = append(contributors, r)
				}
			case "method_set":
				work.Method, _ = stringValue(o)
			case "trailer_set", "trailer_delete":
				trailers = append(trailers, o)
			case "script":
				if s.needsBody(o) {
					lastBody = r
					e = body.read(ctx, original.Body)
				}
				if e == nil {
					var changedTarget bool
					changedTarget, e = s.runRequestScript(ctx, r, o, original, work, body)
					if changedTarget {
						contributors = append(contributors, r)
					}
				}
			default:
				if s.needsBody(o) {
					lastBody = r
					e = body.read(ctx, original.Body)
					if e == nil {
						body.current, e = BodyTransform(ctx, body.current, work.Header, []Operation{o})
					}
				}
			}
			if e == nil && !headersValid(work) {
				e = invalid()
			}
			if e != nil {
				return fallback(r, e)
			}
		}
	}
	final, e := requestTarget(work.URL)
	if e != nil || !s.TargetAuthorized(final.host) {
		return out, &Error{Code: "rewrite_auth"}
	}
	if final != targetOriginal {
		if len(contributors) == 0 {
			return out, &Error{Code: "rewrite_auth"}
		}
		for _, r := range contributors {
			if !consent(r, final) {
				return out, &Error{Code: "rewrite_auth", RuleID: r.ID}
			}
		}
	}
	work.URL.Host = authority(final)
	work.Host = work.URL.Host
	deleteHeader(work.Header, "Host")
	if body.reserved {
		if !body.complete {
			return fallback(lastBody, invalid())
		}
		work.Body = body.reader(false)
		work.ContentLength = int64(len(body.current))
		work.TransferEncoding = nil
		invalidate(work.Header)
	}
	if len(trailers) > 0 {
		if body.complete {
			work.Trailer, e = TransformTrailers(original.Trailer, trailers)
			if e != nil {
				return fallback(lastBody, e)
			}
		} else {
			work.Trailer = trailerKeys(original.Trailer, trailers)
			work.Body = TrailerBody(work.Body, original.Trailer, work.Trailer, trailers)
		}
		work.ContentLength = -1
		work.TransferEncoding = nil
		work.Header.Del("Content-Length")
	}
	if mode == "full" {
		work.Header.Del("Range")
		work.Header.Del("If-Range")
	}
	if ctx.Err() != nil || parent.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	out.Request = work
	if len(out.RuleIDs) > 0 {
		out.Outcome = "rewritten"
	}
	return out, nil
}
func trailerKeys(original http.Header, ops []Operation) http.Header {
	h := original.Clone()
	if h == nil {
		h = http.Header{}
	}
	for _, o := range ops {
		if o.Op == "trailer_set" {
			h[http.CanonicalHeaderKey(o.Name)] = nil
		} else if o.Op == "trailer_delete" {
			h.Del(o.Name)
		}
	}
	return h
}
func cloneResponse(r *http.Response) *http.Response {
	c := *r
	c.Header = r.Header.Clone()
	if c.Header == nil {
		c.Header = http.Header{}
	}
	c.Trailer = r.Trailer.Clone()
	c.TransferEncoding = append([]string{}, r.TransferEncoding...)
	return &c
}
func noResponseBody(method string, status int) bool {
	return method == "HEAD" || status == 204 || status == 205 || status == 304 || status == 101
}
func (s *Snapshot) TransformResponse(parent context.Context, originalRequest *http.Request, original *http.Response) (ResponseResult, error) {
	out := ResponseResult{RuleIDs: []string{}, Outcome: "unchanged", RuleVersion: s.version}
	ctx, cancel := stageContext(parent)
	defer cancel()
	if parent.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	if originalRequest == nil || original == nil || original.StatusCode < 100 || original.StatusCode > 599 || !validHeaders(original.Header, MaxHeadersBytes) {
		return out, invalid()
	}
	work := cloneResponse(original)
	body := &bodyTransaction{}
	defer body.release()
	trailers := []Operation{}
	lastRule := Rule{}
	mode, e := s.RangeMode(originalRequest)
	if e != nil {
		return out, e
	}
	fallback := func(rule Rule, e error) (ResponseResult, error) {
		if rule.OnError == "forward_original" && body.canRecover() && ctx.Err() == nil {
			r := cloneResponse(original)
			if body.consumed {
				r.Body = body.reader(true)
			}
			out.Response = r
			out.Outcome = "forward_original"
			return out, nil
		}
		return out, transformFailure(e, rule.ID)
	}
	for _, r := range s.doc.Rules {
		if r.Stage != StageResponse || !s.matchesRequest(r, originalRequest) || !s.matchesResponse(r, original) {
			continue
		}
		out.RuleIDs = append(out.RuleIDs, r.ID)
		lastRule = r
		for _, o := range r.Operations {
			if ctx.Err() != nil || parent.Err() != nil {
				return out, &Error{Code: "rewrite_cancelled", RuleID: r.ID}
			}
			e = nil
			if original.StatusCode == 101 && (s.needsBody(o) || o.Op == "status_set") {
				return fallback(r, invalid())
			}
			switch o.Op {
			case "header_set", "header_append", "header_delete":
				applyHeader(work.Header, o)
			case "status_set":
				var n int
				_ = json.Unmarshal(o.Value, &n)
				work.StatusCode = n
				work.Status = http.StatusText(n)
			case "trailer_set", "trailer_delete":
				trailers = append(trailers, o)
			case "script":
				if s.needsBody(o) {
					if noResponseBody(originalRequest.Method, original.StatusCode) {
						e = invalid()
					} else {
						e = body.read(ctx, original.Body)
					}
				}
				if e == nil {
					e = s.runResponseScript(ctx, r, o, originalRequest, original, work, body)
				}
			default:
				if s.needsBody(o) {
					if noResponseBody(originalRequest.Method, original.StatusCode) {
						e = invalid()
					} else {
						e = body.read(ctx, original.Body)
					}
					if e == nil {
						if mode == "full" && (original.StatusCode == 206 || original.Header.Get("Content-Range") != "") {
							e = invalid()
						} else if original.StatusCode == 206 {
							body.current, e = TransformFragments(ctx, body.current, work.Header, []Operation{o})
						} else {
							body.current, e = BodyTransform(ctx, body.current, work.Header, []Operation{o})
						}
					}
				}
			}
			if e == nil && !validHeaders(work.Header, MaxHeadersBytes) {
				e = invalid()
			}
			if e != nil {
				return fallback(r, e)
			}
		}
	}
	if noResponseBody(originalRequest.Method, work.StatusCode) {
		if body.complete && len(body.current) > 0 {
			return fallback(lastRule, invalid())
		}
		if work.StatusCode != 101 {
			if original.Body != nil {
				original.Body.Close()
			}
			work.Body = http.NoBody
			work.TransferEncoding = nil
			work.Trailer = nil
			if work.StatusCode == 204 || work.StatusCode == 205 {
				work.ContentLength = 0
				work.Header.Del("Content-Length")
			} else {
				work.ContentLength = original.ContentLength
			}
		}
	} else if body.reserved {
		work.Body = body.reader(false)
		work.ContentLength = int64(len(body.current))
		work.TransferEncoding = nil
		invalidate(work.Header)
	}
	if len(trailers) > 0 {
		if noResponseBody(originalRequest.Method, work.StatusCode) {
			return fallback(lastRule, invalid())
		}
		if body.complete {
			work.Trailer, e = TransformTrailers(original.Trailer, trailers)
			if e != nil {
				return fallback(lastRule, e)
			}
		} else {
			work.Trailer = trailerKeys(original.Trailer, trailers)
			work.Body = TrailerBody(work.Body, original.Trailer, work.Trailer, trailers)
		}
		work.ContentLength = -1
		work.TransferEncoding = nil
		work.Header.Del("Content-Length")
	}
	if ctx.Err() != nil || parent.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	out.Response = work
	if len(out.RuleIDs) > 0 {
		out.Outcome = "rewritten"
	}
	return out, nil
}
func (s *Snapshot) NeedsMessageEditing(stage string, req *http.Request, handshake *http.Response) bool {
	for _, r := range s.doc.Rules {
		if r.Stage == stage && s.matchesRequest(r, req) && s.matchesResponse(r, handshake) {
			for _, o := range r.Operations {
				if bodyOperation(o) || o.Op == "script" {
					return true
				}
			}
		}
	}
	return false
}
func (s *Snapshot) TransformMessage(parent context.Context, stage string, req *http.Request, handshake *http.Response, original Message) (MessageResult, error) {
	out := MessageResult{Message: original, RuleIDs: []string{}, Outcome: "unchanged", RuleVersion: s.version}
	if stage != StageWebSocketClient && stage != StageWebSocketServer || original.Type != "text" && original.Type != "binary" {
		return out, invalid()
	}
	ctx, cancel := stageContext(parent)
	defer cancel()
	if parent.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	if len(original.Body) > MaxBodyBytes {
		return out, &Error{Code: "rewrite_limit"}
	}
	work := Message{Type: original.Type, Body: append([]byte{}, original.Body...)}
	for _, r := range s.doc.Rules {
		if r.Stage != stage || !s.matchesRequest(r, req) || !s.matchesResponse(r, handshake) || len(r.Match.MessageTypes) > 0 && !hasString(r.Match.MessageTypes, original.Type) {
			continue
		}
		out.RuleIDs = append(out.RuleIDs, r.ID)
		for _, o := range r.Operations {
			var e error
			if bodyOperation(o) {
				h := http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}}
				if o.Op == "json_set" || o.Op == "json_delete" {
					h.Set("Content-Type", "application/json")
				}
				work.Body, e = BodyTransform(ctx, work.Body, h, []Operation{o})
			} else if o.Op == "script" {
				work, e = s.runMessageScript(ctx, r, o, stage, req, handshake, original, work)
			}
			if o.MessageType != "" {
				work.Type = o.MessageType
			}
			if e == nil && (len(work.Body) > MaxBodyBytes || work.Type == "text" && !utf8.Valid(work.Body)) {
				e = invalid()
			}
			if e != nil {
				if r.OnError == "forward_original" && ctx.Err() == nil {
					out.Outcome = "forward_original"
					return out, nil
				}
				return out, transformFailure(e, r.ID)
			}
		}
	}
	if ctx.Err() != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	out.Message = work
	if len(out.RuleIDs) > 0 {
		out.Outcome = "rewritten"
	}
	return out, nil
}

// TransformHandshake applies ordinary header operations from matching WebSocket
// rules to a valid upgrade response. Protocol-owned fields remain immutable.
func (s *Snapshot) TransformHandshake(req *http.Request, resp *http.Response) (*http.Response, error) {
	work := cloneResponse(resp)
	for _, r := range s.doc.Rules {
		if (r.Stage == StageWebSocketClient || r.Stage == StageWebSocketServer) && s.matchesRequest(r, req) && s.matchesResponse(r, resp) {
			for _, o := range r.Operations {
				if strings.HasPrefix(o.Op, "header_") {
					applyHeader(work.Header, o)
				}
			}
			if !validHeaders(work.Header, MaxHeadersBytes) {
				if r.OnError == "forward_original" {
					return cloneResponse(resp), nil
				}
				return nil, transformFailure(invalid(), r.ID)
			}
		}
	}
	return work, nil
}

func jsonMemory(v any) int {
	switch x := v.(type) {
	case map[string]any:
		n := 256
		for k, v := range x {
			n += len(k) + 128 + jsonMemory(v)
		}
		return n
	case []any:
		n := 24 + len(x)*32
		for _, v := range x {
			n += jsonMemory(v)
		}
		return n
	case string:
		return 32 + len(x)
	default:
		return 32
	}
}
