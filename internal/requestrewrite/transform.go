package requestrewrite

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type Result struct {
	Request     *http.Request
	RuleIDs     []string
	Outcome     string
	RuleVersion string
}
type target struct {
	scheme, host string
	port         int
}

func requestTarget(u *url.URL) (target, error) {
	if u == nil || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Scheme != "http" && u.Scheme != "https" {
		return target{}, invalid()
	}
	h, e := NormalizeDomain(u.Hostname())
	if e != nil {
		return target{}, e
	}
	p := 80
	if u.Scheme == "https" {
		p = 443
	}
	if u.Port() != "" {
		p, e = strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return target{}, invalid()
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return target{}, invalid()
	}
	if u.Host != u.Hostname() && u.Host != net.JoinHostPort(u.Hostname(), u.Port()) {
		return target{}, invalid()
	}
	return target{u.Scheme, h, p}, nil
}
func authority(t target) string {
	p := 80
	if t.scheme == "https" {
		p = 443
	}
	if t.port == p {
		return t.host
	}
	return net.JoinHostPort(t.host, strconv.Itoa(t.port))
}
func headersValid(r *http.Request) bool {
	n := 0
	seen := map[string]bool{}
	for k, vs := range r.Header {
		canonical := http.CanonicalHeaderKey(k)
		if seen[canonical] || canonical != k {
			return false
		}
		seen[canonical] = true
		if !token(k) {
			return false
		}
		for _, v := range vs {
			if !cleanValue(v) {
				return false
			}
			n += len(k) + len(v) + 4
		}
	}
	if r.ContentLength > 0 && (len(r.TransferEncoding) > 0 || r.Header.Get("Transfer-Encoding") != "") {
		return false
	}
	if len(r.TransferEncoding) > 1 || len(r.TransferEncoding) == 1 && r.TransferEncoding[0] != "chunked" {
		return false
	}
	if te := r.Header.Get("Transfer-Encoding"); te != "" && !strings.EqualFold(te, "chunked") {
		return false
	}
	if n > MaxHeadersBytes {
		return false
	}
	if len(r.Header.Values("Content-Length")) > 1 || len(r.Header.Values("Transfer-Encoding")) > 1 {
		return false
	}
	if r.Header.Get("Content-Length") != "" && (r.Header.Get("Transfer-Encoding") != "" || len(r.TransferEncoding) > 0) {
		return false
	}
	if v := r.Header.Get("Content-Length"); v != "" {
		for _, c := range v {
			if c < '0' || c > '9' {
				return false
			}
		}
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 0 || r.ContentLength >= 0 && n != r.ContentLength {
			return false
		}
	}
	if vs, ok := r.Header["Host"]; ok && (len(vs) != 1 || vs[0] == "") {
		return false
	}
	if vs, ok := r.Header["Content-Length"]; ok && (len(vs) != 1 || vs[0] == "") {
		return false
	}
	if vs, ok := r.Header["Transfer-Encoding"]; ok && (len(vs) != 1 || vs[0] == "") {
		return false
	}
	if h := r.Header.Get("Host"); h != "" && h != r.Host {
		return false
	}
	return true
}
func cloneRequest(ctx context.Context, r *http.Request) *http.Request {
	c := r.Clone(ctx)
	if c.Header == nil {
		c.Header = http.Header{}
	}
	c.Body = r.Body
	c.GetBody = nil
	return c
}
func matchRule(r Rule, t target, method, path string) bool {
	if !r.Enabled || !patternMatches(r.Match.Domain, t.host) || !strings.HasPrefix(path, r.Match.PathPrefix) {
		return false
	}
	for _, m := range r.Match.Methods {
		if method == m {
			return true
		}
	}
	return false
}
func consent(r Rule, t target) bool {
	for _, c := range r.RedirectConsent {
		if !patternMatches(c.Domain, t.host) {
			continue
		}
		scheme, port := false, false
		for _, s := range c.Schemes {
			scheme = scheme || s == t.scheme
		}
		for _, p := range c.Ports {
			port = port || p == t.port
		}
		if scheme && port {
			return true
		}
	}
	return false
}
func deleteHeader(h http.Header, name string) {
	for k := range h {
		if strings.EqualFold(k, name) {
			delete(h, k)
		}
	}
}
func setURL(r *http.Request, o Operation) error {
	v, _ := stringValue(o)
	switch o.Component {
	case "scheme":
		r.URL.Scheme = v
	case "host":
		v, _ = NormalizeDomain(v)
		if p := r.URL.Port(); p != "" {
			r.URL.Host = net.JoinHostPort(v, p)
		} else {
			r.URL.Host = v
		}
	case "port":
		host := r.URL.Hostname()
		if v == "" {
			r.URL.Host = host
		} else {
			n, _ := strconv.Atoi(v)
			r.URL.Host = net.JoinHostPort(host, strconv.Itoa(n))
		}
	case "path":
		p, e := url.PathUnescape(v)
		if e != nil {
			return invalid()
		}
		r.URL.Path = p
		r.URL.RawPath = v
	case "query":
		r.URL.RawQuery = v
		r.URL.ForceQuery = false
	}
	return nil
}

// Transform performs no forwarding/DNS. The caller owns the returned body, and
// must send it once only after success. Non-JSON bodies keep their original stream.
func (s *Snapshot) Transform(ctx context.Context, original *http.Request) (Result, error) {
	return s.transform(ctx, original, false)
}

// TransformUpgrade applies header/URL operations to an upgrade handshake.
// JSON operations are skipped; upgraded frames are never body transformations.
func (s *Snapshot) TransformUpgrade(ctx context.Context, original *http.Request) (Result, error) {
	return s.transform(ctx, original, true)
}
func (s *Snapshot) transform(ctx context.Context, original *http.Request, upgrade bool) (Result, error) {
	result := Result{RuleIDs: []string{}, Outcome: "unchanged", RuleVersion: s.version}
	if ctx.Err() != nil {
		return result, &Error{Code: "rewrite_cancelled"}
	}
	if original == nil || !token(original.Method) || !headersValid(original) {
		return result, invalid()
	}
	t, e := requestTarget(original.URL)
	if e != nil || !s.Authorized(t.host) {
		return result, &Error{Code: "rewrite_auth"}
	}
	if original.Host != "" {
		u := *original.URL
		u.Host = original.Host
		ht, e := requestTarget(&u)
		if e != nil || ht != t {
			return result, invalid()
		}
	}
	path := original.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if validateURLValue("path", path) != nil || validateURLValue("query", original.URL.RawQuery) != nil {
		return result, invalid()
	}
	working := cloneRequest(ctx, original)
	working.RequestURI = ""
	contributors := []Rule{}
	var body *bodyEdit
	var lastJSONRule Rule
	defer func() {
		if body != nil && !body.transferred {
			body.release()
		}
	}()
	for _, rule := range s.doc.Rules {
		if !matchRule(rule, t, original.Method, path) {
			continue
		}
		result.RuleIDs = append(result.RuleIDs, rule.ID)
		changesTarget := false
		for _, op := range rule.Operations {
			if e = ctx.Err(); e != nil {
				return result, &Error{Code: "rewrite_cancelled", RuleID: rule.ID}
			}
			switch op.Op {
			case "header_set":
				v, _ := stringValue(op)
				deleteHeader(working.Header, op.Name)
				working.Header.Set(op.Name, v)
			case "header_delete":
				deleteHeader(working.Header, op.Name)
			case "url_set":
				e = setURL(working, op)
				if op.Component == "scheme" || op.Component == "host" || op.Component == "port" {
					changesTarget = true
				}
			case "json_set", "json_delete":
				if upgrade {
					continue
				}
				lastJSONRule = rule
				if body == nil {
					body, e = newBodyEdit(ctx, original)
				}
				if e == nil {
					parts, _ := pointerParts(op.Pointer)
					var value any
					if op.Op == "json_set" {
						value, e = decodeJSON(op.Value)
					}
					if e == nil {
						body.value, e = editJSON(body.value, parts, value, op.Op == "json_delete")
					}
				}
			}
			if e == nil && !headersValid(working) {
				e = invalid()
			}
			if e == nil && body != nil && jsonMemory(body.value) > 12<<20 {
				return result, &Error{Code: "rewrite_limit", RuleID: rule.ID}
			}
			if e != nil {
				if rule.OnError == "forward_original" && (body == nil || body.complete || body.reserved && !body.consumed) && ctx.Err() == nil {
					r := cloneRequest(ctx, original)
					if body != nil && body.consumed {
						body.transferred = true
						r.Body = &reservedBody{Reader: bytes.NewReader(body.original), release: body.release}
						body.value = nil
						r.GetBody = nil
					}
					if ctx.Err() != nil {
						if body != nil {
							body.transferred = false
						}
						return result, &Error{Code: "rewrite_cancelled"}
					}
					result.Request = r
					result.Outcome = "forward_original"
					return result, nil
				}
				if body != nil && !body.reserved {
					return result, &Error{Code: "rewrite_limit", RuleID: rule.ID}
				}
				return result, &Error{Code: "rewrite_transform", RuleID: rule.ID}
			}
		}
		if changesTarget {
			contributors = append(contributors, rule)
		}
	}
	final, e := requestTarget(working.URL)
	if e != nil || !s.Authorized(final.host) {
		return result, &Error{Code: "rewrite_auth"}
	}
	if final != t {
		for _, r := range contributors {
			if !consent(r, final) {
				return result, &Error{Code: "rewrite_auth", RuleID: r.ID}
			}
		}
	}
	working.URL.Host = authority(final)
	working.Host = working.URL.Host
	deleteHeader(working.Header, "Host")
	if body != nil {
		encoded, e := body.output()
		if e != nil {
			if lastJSONRule.OnError == "forward_original" && body.complete && ctx.Err() == nil {
				r := cloneRequest(ctx, original)
				body.transferred = true
				body.value = nil
				r.Body = &reservedBody{Reader: bytes.NewReader(body.original), release: body.release}
				if ctx.Err() != nil {
					if body != nil {
						body.transferred = false
					}
					return result, &Error{Code: "rewrite_cancelled"}
				}
				result.Request = r
				result.Outcome = "forward_original"
				return result, nil
			}
			return result, &Error{Code: "rewrite_transform", RuleID: lastJSONRule.ID}
		}
		body.original = nil
		body.value = nil
		working.Body = &reservedBody{Reader: bytes.NewReader(encoded), release: body.release}
		working.ContentLength = int64(len(encoded))
		working.TransferEncoding = nil
		working.Trailer = nil
		deleteHeader(working.Header, "Transfer-Encoding")
		deleteHeader(working.Header, "Content-Length")
		for _, name := range []string{"Content-MD5", "Digest", "Content-Digest", "Repr-Digest"} {
			deleteHeader(working.Header, name)
		}
		working.GetBody = nil
	}
	if !headersValid(working) {
		return result, invalid()
	}
	if ctx.Err() != nil {
		return result, &Error{Code: "rewrite_cancelled"}
	}
	if body != nil {
		body.transferred = true
	}
	result.Request = working
	if len(result.RuleIDs) > 0 {
		result.Outcome = "rewritten"
	}
	return result, nil
}

// A body transaction reserves the full bounded workspace before consuming input.
// This conservative admission permits at most one maximum-size transform at once.
var bodyMemory = make(chan struct{}, 1)

type bodyEdit struct {
	original                                              []byte
	value                                                 any
	compressed, complete, reserved, consumed, transferred bool
	once                                                  sync.Once
}

func (b *bodyEdit) release() {
	b.once.Do(func() {
		if b.reserved {
			<-bodyMemory
		}
	})
}
func limitedRead(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxBodyBytes+1))
	if e != nil || len(b) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	return b, nil
}
func newBodyEdit(ctx context.Context, r *http.Request) (b *bodyEdit, err error) {
	b = &bodyEdit{}
	select {
	case bodyMemory <- struct{}{}:
		b.reserved = true
	default:
		return b, &Error{Code: "rewrite_limit"}
	}
	if len(r.Trailer) > 0 || r.Header.Get("Trailer") != "" {
		return b, invalid()
	}
	media, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !(media == "application/json" || strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")) {
		return b, invalid()
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			return b, invalid()
		}
	}
	enc := r.Header.Values("Content-Encoding")
	if len(enc) > 1 {
		return b, invalid()
	}
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	if encoding != "" && encoding != "identity" && encoding != "gzip" {
		return b, invalid()
	}
	b.compressed = encoding == "gzip"
	if r.Body == nil {
		b.complete = true
		return b, invalid()
	}
	done := make(chan struct{})
	var closeOnce sync.Once
	closeOriginal := func() { closeOnce.Do(func() { _ = r.Body.Close() }) }
	go func() {
		select {
		case <-ctx.Done():
			closeOriginal()
		case <-done:
		}
	}()
	b.consumed = true
	b.original, e = limitedRead(r.Body)
	close(done)
	closeOriginal()
	if e != nil || ctx.Err() != nil {
		return b, &Error{Code: "rewrite_transform"}
	}
	b.complete = true
	raw := b.original
	if b.compressed {
		reader := bytes.NewReader(raw)
		gz, e := gzip.NewReader(reader)
		if e != nil {
			return b, invalid()
		}
		gz.Multistream(false)
		raw, e = limitedRead(gz)
		ce := gz.Close()
		if e != nil || ce != nil || reader.Len() != 0 {
			return b, invalid()
		}
	}
	b.value, e = decodeJSONBudget(raw, 12<<20)
	if e != nil {
		return b, e
	}
	if jsonMemory(b.value) > 12<<20 {
		return b, &Error{Code: "rewrite_limit"}
	}
	return b, nil
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
func (b *bodyEdit) output() ([]byte, error) {
	raw := canonicalJSON(b.value)
	if len(raw) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	if !b.compressed {
		return raw, nil
	}
	var dst bytes.Buffer
	gz := gzip.NewWriter(&dst)
	if _, e := gz.Write(raw); e != nil {
		return nil, invalid()
	}
	if gz.Close() != nil || dst.Len() > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	return dst.Bytes(), nil
}

type reservedBody struct {
	*bytes.Reader
	release func()
	once    sync.Once
}

func (b *reservedBody) Close() error { b.once.Do(b.release); return nil }
