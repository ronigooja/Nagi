package requestrewrite

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const MaxPreviewBytes = 12 << 20

// Fixture and PreviewResult are explicitly sensitive request data. They must
// never be sent to ordinary logs, status, diagnostics or implicit exports.
type Fixture struct {
	Scheme     string              `json:"scheme"`
	Host       string              `json:"host"`
	Port       int                 `json:"port"`
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Query      string              `json:"query"`
	Headers    map[string][]string `json:"headers"`
	BodyBase64 string              `json:"body_base64"`
}
type PreviewResult struct {
	Sensitive   bool     `json:"sensitive"`
	RuleVersion string   `json:"rule_version"`
	RuleIDs     []string `json:"rule_ids"`
	Outcome     string   `json:"outcome"`
	Request     Fixture  `json:"request"`
}

func (s *Snapshot) PreviewSensitive(ctx context.Context, input []byte) (PreviewResult, error) {
	out := PreviewResult{Sensitive: true}
	if len(input) > MaxPreviewBytes {
		return out, &Error{Code: "rewrite_limit"}
	}
	v, e := decodeJSONBudget(input, 16<<20)
	if e != nil {
		return out, e
	}
	m, e := fields(v, "scheme", "host", "port", "method", "path", "query", "headers", "body_base64")
	if e != nil {
		return out, e
	}
	for _, name := range []string{"scheme", "host", "method", "path", "query", "body_base64"} {
		if _, ok := m[name].(string); !ok {
			return out, invalid()
		}
	}
	if _, ok := m["headers"].(map[string]any); !ok {
		return out, invalid()
	}
	var fixture Fixture
	if json.Unmarshal(input, &fixture) != nil || fixture.Headers == nil || fixture.Port < 1 || fixture.Port > 65535 || validateURLValue("path", fixture.Path) != nil || validateURLValue("query", fixture.Query) != nil {
		return out, invalid()
	}
	host, e := NormalizeDomain(fixture.Host)
	if e != nil {
		return out, e
	}
	if base64.StdEncoding.DecodedLen(len(fixture.BodyBase64)) > MaxBodyBytes+2 {
		return out, &Error{Code: "rewrite_limit"}
	}
	body, e := base64.StdEncoding.Strict().DecodeString(fixture.BodyBase64)
	if e != nil || len(body) > MaxBodyBytes || strings.ContainsAny(fixture.BodyBase64, "\r\n") {
		return out, invalid()
	}
	path, _ := url.PathUnescape(fixture.Path)
	u := &url.URL{Scheme: fixture.Scheme, Host: net.JoinHostPort(host, strconv.Itoa(fixture.Port)), Path: path, RawPath: fixture.Path, RawQuery: fixture.Query}
	r := &http.Request{Method: fixture.Method, URL: u, Host: u.Host, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
	seen := map[string]bool{}
	for k, vs := range fixture.Headers {
		key := http.CanonicalHeaderKey(k)
		if seen[key] || vs == nil {
			return out, invalid()
		}
		seen[key] = true
		r.Header[key] = vs
	}
	defer r.Body.Close()
	result, e := s.Transform(ctx, r)
	if e != nil {
		return out, e
	}
	defer result.Request.Body.Close()
	transformed, e := limitedRead(result.Request.Body)
	if e != nil {
		return out, e
	}
	t, e := requestTarget(result.Request.URL)
	if e != nil {
		return out, e
	}
	out.RuleVersion = result.RuleVersion
	out.RuleIDs = result.RuleIDs
	out.Outcome = result.Outcome
	out.Request = Fixture{Scheme: t.scheme, Host: t.host, Port: t.port, Method: result.Request.Method, Path: result.Request.URL.EscapedPath(), Query: result.Request.URL.RawQuery, Headers: map[string][]string(result.Request.Header), BodyBase64: base64.StdEncoding.EncodeToString(transformed)}
	return out, nil
}
