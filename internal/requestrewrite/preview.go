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

// Two maximum bodies, their base64 expansion and header/trailer overhead fit.
const MaxPreviewBytes = 24 << 20
const MaxPreviewResultBytes = 3 * MaxPreviewBytes

// Fixtures and preview output are explicit sensitive data, never diagnostics.
type Fixture struct {
	Scheme     string              `json:"scheme"`
	Host       string              `json:"host"`
	Port       int                 `json:"port"`
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Query      string              `json:"query"`
	Headers    map[string][]string `json:"headers"`
	Trailers   map[string][]string `json:"trailers,omitempty"`
	BodyBase64 string              `json:"body_base64"`
}
type ResponseFixture struct {
	Status     int                 `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Trailers   map[string][]string `json:"trailers,omitempty"`
	BodyBase64 string              `json:"body_base64"`
}
type MessageFixture struct {
	Type       string `json:"type"`
	BodyBase64 string `json:"body_base64"`
}
type PreviewInput struct {
	Stage    string           `json:"stage"`
	Request  Fixture          `json:"request"`
	Response *ResponseFixture `json:"response,omitempty"`
	Message  *MessageFixture  `json:"message,omitempty"`
}
type StageResult struct {
	Stage   string   `json:"stage"`
	RuleIDs []string `json:"rule_ids"`
	Outcome string   `json:"outcome"`
}
type PreviewResult struct {
	Sensitive   bool             `json:"sensitive"`
	RuleVersion string           `json:"rule_version"`
	RuleIDs     []string         `json:"rule_ids"`
	Outcome     string           `json:"outcome"`
	Stage       string           `json:"stage"`
	Stages      []StageResult    `json:"stages"`
	Request     Fixture          `json:"request"`
	Response    *ResponseFixture `json:"response,omitempty"`
	Message     *MessageFixture  `json:"message,omitempty"`
}

func fixtureBody(encoded string) ([]byte, error) {
	if base64.StdEncoding.DecodedLen(len(encoded)) > MaxBodyBytes+2 {
		return nil, &Error{Code: "rewrite_limit"}
	}
	body, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || len(body) > MaxBodyBytes || strings.ContainsAny(encoded, "\r\n") {
		return nil, invalid()
	}
	return body, nil
}
func fixtureHeaders(h map[string][]string, limit int) (http.Header, error) {
	if h == nil {
		return nil, invalid()
	}
	out := http.Header{}
	seen := map[string]bool{}
	for k, vs := range h {
		key := http.CanonicalHeaderKey(k)
		if seen[key] || vs == nil {
			return nil, invalid()
		}
		seen[key] = true
		out[key] = append([]string{}, vs...)
	}
	if !validHeaders(out, limit) {
		return nil, invalid()
	}
	return out, nil
}
func (f Fixture) request() (*http.Request, error) {
	host, e := NormalizeDomain(f.Host)
	if e != nil || f.Port < 1 || f.Port > 65535 || validateURLValue("path", f.Path) != nil || validateURLValue("query", f.Query) != nil {
		return nil, invalid()
	}
	body, e := fixtureBody(f.BodyBase64)
	if e != nil {
		return nil, e
	}
	h, e := fixtureHeaders(f.Headers, MaxHeadersBytes)
	if e != nil {
		return nil, e
	}
	var trailers http.Header
	if f.Trailers != nil {
		trailers, e = fixtureHeaders(f.Trailers, MaxTrailersBytes)
		if e != nil {
			return nil, e
		}
		if _, e = TransformTrailers(trailers, nil); e != nil {
			return nil, e
		}
	}
	path, _ := url.PathUnescape(f.Path)
	u := &url.URL{Scheme: f.Scheme, Host: net.JoinHostPort(host, strconv.Itoa(f.Port)), Path: path, RawPath: f.Path, RawQuery: f.Query}
	if _, e := requestTarget(u); e != nil {
		return nil, e
	}
	return &http.Request{Method: f.Method, URL: u, Host: u.Host, Header: h, Trailer: trailers, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
}
func requestFixture(r *http.Request) (Fixture, error) {
	body, e := limitedRead(r.Body)
	if e != nil {
		return Fixture{}, e
	}
	t, e := requestTarget(r.URL)
	if e != nil {
		return Fixture{}, e
	}
	return Fixture{Scheme: t.scheme, Host: t.host, Port: t.port, Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Headers: r.Header, Trailers: r.Trailer, BodyBase64: base64.StdEncoding.EncodeToString(body)}, nil
}
func (f ResponseFixture) response(req *http.Request) (*http.Response, error) {
	body, e := fixtureBody(f.BodyBase64)
	if e != nil {
		return nil, e
	}
	h, e := fixtureHeaders(f.Headers, MaxHeadersBytes)
	if e != nil {
		return nil, e
	}
	var trailers http.Header
	if f.Trailers != nil {
		trailers, e = fixtureHeaders(f.Trailers, MaxTrailersBytes)
		if e != nil {
			return nil, e
		}
	}
	if f.Status < 100 || f.Status > 599 {
		return nil, invalid()
	}
	return &http.Response{StatusCode: f.Status, Header: h, Trailer: trailers, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
}
func responseFixture(r *http.Response) (*ResponseFixture, error) {
	body, e := limitedRead(r.Body)
	if e != nil {
		return nil, e
	}
	return &ResponseFixture{Status: r.StatusCode, Headers: r.Header, Trailers: r.Trailer, BodyBase64: base64.StdEncoding.EncodeToString(body)}, nil
}
func (s *Snapshot) PreviewSensitive(ctx context.Context, input []byte) (PreviewResult, error) {
	out := PreviewResult{Sensitive: true, RuleVersion: s.version, RuleIDs: []string{}, Stages: []StageResult{}, Outcome: "unchanged"}
	if len(input) == 0 || len(input) > MaxPreviewBytes {
		return out, &Error{Code: "rewrite_limit"}
	}
	v, e := decodeJSONBudget(input, 32<<20)
	if e != nil {
		return out, e
	}
	m, ok := v.(map[string]any)
	if !ok || m["stage"] == nil || m["request"] == nil {
		return out, invalid()
	}
	// Required body fields distinguish an empty body from incomplete input.
	for _, k := range []string{"request", "response", "message"} {
		if part, ok := m[k]; ok {
			pm, ok := part.(map[string]any)
			if !ok || pm["body_base64"] == nil {
				return out, invalid()
			}
		}
	}
	var in PreviewInput
	d := json.NewDecoder(bytes.NewReader(input))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return out, invalid()
	}
	if e := s.PrepareScripts(ctx); e != nil {
		return out, e
	}
	req, e := in.Request.request()
	if e != nil {
		return out, e
	}
	defer req.Body.Close()
	out.Stage = in.Stage
	out.Request = in.Request
	add := func(stage string, ids []string, outcome string) {
		out.Stages = append(out.Stages, StageResult{stage, ids, outcome})
		out.RuleIDs = append(out.RuleIDs, ids...)
		if outcome != "unchanged" {
			out.Outcome = outcome
		}
	}
	switch in.Stage {
	case StageRequest, "exchange":
		if in.Message != nil || in.Stage == StageRequest && in.Response != nil || in.Stage == "exchange" && in.Response == nil {
			return out, invalid()
		}
		var result Result
		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			result, e = s.TransformUpgrade(ctx, req)
		} else {
			result, e = s.Transform(ctx, req)
		}
		if e != nil {
			return out, e
		}
		if result.Request.Body != nil {
			defer result.Request.Body.Close()
		}
		out.Request, e = requestFixture(result.Request)
		if result.Request.Body != nil {
			_ = result.Request.Body.Close()
		}
		if e != nil {
			return out, e
		}
		add(StageRequest, result.RuleIDs, result.Outcome)
		if in.Stage == StageRequest {
			return out, nil
		}
	case StageResponse:
		if in.Response == nil || in.Message != nil {
			return out, invalid()
		}
	case StageWebSocketClient, StageWebSocketServer:
		if in.Message == nil || in.Response == nil {
			return out, invalid()
		}
		handshake, e := in.Response.response(req)
		if e != nil {
			return out, e
		}
		defer handshake.Body.Close()
		if handshake.StatusCode != 101 {
			return out, invalid()
		}
		body, e := fixtureBody(in.Message.BodyBase64)
		if e != nil {
			return out, e
		}
		result, e := s.TransformMessage(ctx, in.Stage, req, handshake, Message{Type: in.Message.Type, Body: body})
		if e != nil {
			return out, e
		}
		out.Message = &MessageFixture{Type: result.Message.Type, BodyBase64: base64.StdEncoding.EncodeToString(result.Message.Body)}
		out.Response = in.Response
		add(in.Stage, result.RuleIDs, result.Outcome)
		return out, nil
	default:
		return out, invalid()
	}
	resp, e := in.Response.response(req)
	if e != nil {
		return out, e
	}
	defer resp.Body.Close()
	result, e := s.TransformResponse(ctx, req, resp)
	if e != nil {
		return out, e
	}
	if result.Response.Body != nil {
		defer result.Response.Body.Close()
	}
	out.Response, e = responseFixture(result.Response)
	if e != nil {
		return out, e
	}
	add(StageResponse, result.RuleIDs, result.Outcome)
	return out, nil
}
