package requestrewrite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/rewritescript"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

func requestMetadata(r *http.Request) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{"method": r.Method, "url": r.URL.String(), "headers": r.Header.Clone()}
}
func responseMetadata(r *http.Response) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{"status": r.StatusCode, "headers": r.Header.Clone()}
}
func (s *Snapshot) executeScript(ctx context.Context, o Operation, stage string, original any, message map[string]any, body []byte) (map[string]json.RawMessage, error) {
	script := s.script(o.Script)
	if script.ID == "" {
		return nil, invalid()
	}
	if script.BodyAccess == "buffer" {
		message["body_base64"] = base64.StdEncoding.EncodeToString(body)
	}
	input, e := json.Marshal(map[string]any{"stage": stage, "original": original, "message": message})
	if e != nil {
		return nil, invalid()
	}
	scriptExecutor.RLock()
	run := scriptExecutor.run
	scriptExecutor.RUnlock()
	if run == nil {
		run = isolatedScript
	}
	raw, e := run(ctx, script.Source, input)
	if e != nil {
		return nil, e
	}
	if len(raw) > MaxPreviewBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	v, e := decodeJSONBudget(raw, 32<<20)
	if e != nil {
		return nil, &Error{Code: "rewrite_script_output"}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, &Error{Code: "rewrite_script_output"}
	}
	action, ok := m["action"].(string)
	if !ok {
		return nil, &Error{Code: "rewrite_script_output"}
	}
	switch action {
	case "unchanged":
		if len(m) != 1 {
			return nil, &Error{Code: "rewrite_script_output"}
		}
		return nil, nil
	case "block":
		if len(m) != 1 {
			return nil, &Error{Code: "rewrite_script_output"}
		}
		return nil, &Error{Code: "rewrite_script_block"}
	case "replace":
		if len(m) != 2 || m["message"] == nil {
			return nil, &Error{Code: "rewrite_script_output"}
		}
	default:
		return nil, &Error{Code: "rewrite_script_output"}
	}
	var out struct {
		Action  string                     `json:"action"`
		Message map[string]json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Message == nil {
		return nil, &Error{Code: "rewrite_script_output"}
	}
	if script.BodyAccess == "none" {
		if _, ok := out.Message["body_base64"]; ok {
			return nil, &Error{Code: "rewrite_script_output"}
		}
	}
	return out.Message, nil
}
func scriptHeaders(raw json.RawMessage, old http.Header, trailers bool) (http.Header, error) {
	var h http.Header
	if json.Unmarshal(raw, &h) != nil || h == nil {
		return nil, invalid()
	}
	normalized := http.Header{}
	seen := map[string]bool{}
	for k, vs := range h {
		key := http.CanonicalHeaderKey(k)
		if seen[key] || vs == nil {
			return nil, invalid()
		}
		seen[key] = true
		normalized[key] = vs
	}
	max := MaxHeadersBytes
	if trailers {
		max = MaxTrailersBytes
	}
	if !validHeaders(normalized, max) {
		return nil, invalid()
	}
	names := map[string]bool{}
	for k := range old {
		names[k] = true
	}
	for k := range normalized {
		names[k] = true
	}
	for k := range names {
		allowed := allowedHeader(k)
		if trailers {
			allowed = allowedTrailer(k)
		}
		if !allowed && !reflect.DeepEqual(headerValues(old, k), headerValues(normalized, k)) {
			return nil, invalid()
		}
	}
	return normalized, nil
}
func scriptBody(m map[string]json.RawMessage, current []byte) ([]byte, error) {
	raw, ok := m["body_base64"]
	if !ok {
		return current, nil
	}
	var v string
	if json.Unmarshal(raw, &v) != nil || strings.ContainsAny(v, "\r\n") || base64.StdEncoding.DecodedLen(len(v)) > MaxBodyBytes+2 {
		return nil, invalid()
	}
	b, e := base64.StdEncoding.Strict().DecodeString(v)
	if e != nil || len(b) > MaxBodyBytes {
		return nil, invalid()
	}
	return b, nil
}
func scriptKeys(m map[string]json.RawMessage, keys ...string) bool {
	for k := range m {
		if !hasString(keys, k) {
			return false
		}
	}
	return true
}
func (s *Snapshot) runRequestScript(ctx context.Context, r Rule, o Operation, original, work *http.Request, body *bodyTransaction) (bool, error) {
	changesTarget := false
	apply := func(ctx context.Context, decoded []byte, _ http.Header, _ Operation) ([]byte, error) {
		m, e := s.executeScript(ctx, o, StageRequest, map[string]any{"request": requestMetadata(original)}, map[string]any{"method": work.Method, "url": work.URL.String(), "headers": work.Header.Clone(), "trailers": work.Trailer.Clone()}, decoded)
		if e != nil || m == nil {
			return decoded, e
		}
		if !scriptKeys(m, "method", "url", "headers", "trailers", "body_base64") {
			return nil, invalid()
		}
		if raw, ok := m["method"]; ok {
			var v string
			if json.Unmarshal(raw, &v) != nil || !token(v) || v != strings.ToUpper(v) || v == "CONNECT" {
				return nil, invalid()
			}
			work.Method = v
		}
		if raw, ok := m["url"]; ok {
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, invalid()
			}
			u, e := url.Parse(v)
			if e != nil {
				return nil, invalid()
			}
			before, e := requestTarget(work.URL)
			if e != nil {
				return nil, e
			}
			after, e := requestTarget(u)
			if e != nil || validateURLValue("path", u.EscapedPath()) != nil || validateURLValue("query", u.RawQuery) != nil {
				return nil, invalid()
			}
			changesTarget = before != after
			work.URL = u
		}
		if raw, ok := m["headers"]; ok {
			work.Header, e = scriptHeaders(raw, work.Header, false)
			if e != nil {
				return nil, e
			}
		}
		if raw, ok := m["trailers"]; ok {
			if !body.complete {
				return nil, invalid()
			}
			work.Trailer, e = scriptHeaders(raw, work.Trailer, true)
			if e != nil {
				return nil, e
			}
		}
		return scriptBody(m, decoded)
	}
	if s.needsBody(o) {
		raw, e := BodyTransformWithHook(ctx, body.current, work.Header, []Operation{o}, apply)
		if e != nil {
			return changesTarget, e
		}
		body.current = raw
		return changesTarget, nil
	}
	_, e := apply(ctx, nil, work.Header, o)
	return changesTarget, e
}
func (s *Snapshot) runResponseScript(ctx context.Context, r Rule, o Operation, request *http.Request, original, work *http.Response, body *bodyTransaction) error {
	apply := func(ctx context.Context, decoded []byte, _ http.Header, _ Operation) ([]byte, error) {
		m, e := s.executeScript(ctx, o, StageResponse, map[string]any{"request": requestMetadata(request), "response": responseMetadata(original)}, map[string]any{"status": work.StatusCode, "headers": work.Header.Clone(), "trailers": work.Trailer.Clone()}, decoded)
		if e != nil || m == nil {
			return decoded, e
		}
		if !scriptKeys(m, "status", "headers", "trailers", "body_base64") {
			return nil, invalid()
		}
		if raw, ok := m["status"]; ok {
			var n int
			if json.Unmarshal(raw, &n) != nil || n < 200 || n > 599 || original.StatusCode == 101 {
				return nil, invalid()
			}
			work.StatusCode = n
			work.Status = http.StatusText(n)
		}
		if raw, ok := m["headers"]; ok {
			work.Header, e = scriptHeaders(raw, work.Header, false)
			if e != nil {
				return nil, e
			}
		}
		if raw, ok := m["trailers"]; ok {
			if !body.complete {
				return nil, invalid()
			}
			work.Trailer, e = scriptHeaders(raw, work.Trailer, true)
			if e != nil {
				return nil, e
			}
		}
		return scriptBody(m, decoded)
	}
	if s.needsBody(o) {
		mode, e := s.RangeMode(request)
		if e != nil {
			return e
		}
		if mode == "full" && (original.StatusCode == 206 || original.Header.Get("Content-Range") != "") {
			return invalid()
		}
		var raw []byte
		if original.StatusCode == 206 {
			baseline := cloneResponse(work)
			var result *http.Response
			consistent := func(ctx context.Context, decoded []byte, h http.Header, op Operation) ([]byte, error) {
				*work = *cloneResponse(baseline)
				out, err := apply(ctx, decoded, h, op)
				if err != nil {
					return nil, err
				}
				if result != nil && (result.StatusCode != work.StatusCode || !reflect.DeepEqual(result.Header, work.Header) || !reflect.DeepEqual(result.Trailer, work.Trailer)) {
					return nil, invalid()
				}
				result = cloneResponse(work)
				return out, nil
			}
			e = body.transformFragments(ctx, original.Header, baseline.Header, []Operation{o}, consistent)
			if e == nil {
				*work = *result
			}
			return e
		} else {
			raw, e = BodyTransformWithHook(ctx, body.current, work.Header, []Operation{o}, apply)
		}
		if e != nil {
			return e
		}
		body.current = raw
		return nil
	}
	_, e := apply(ctx, nil, work.Header, o)
	return e
}
func (s *Snapshot) runMessageScript(ctx context.Context, r Rule, o Operation, stage string, req *http.Request, handshake *http.Response, original, current Message) (Message, error) {
	m, e := s.executeScript(ctx, o, stage, map[string]any{"request": requestMetadata(req), "response": responseMetadata(handshake), "message": map[string]any{"type": original.Type}}, map[string]any{"type": current.Type}, current.Body)
	if e != nil || m == nil {
		return current, e
	}
	if !scriptKeys(m, "type", "body_base64") {
		return current, invalid()
	}
	if raw, ok := m["type"]; ok {
		var v string
		if json.Unmarshal(raw, &v) != nil || v != "text" && v != "binary" {
			return current, invalid()
		}
		current.Type = v
	}
	current.Body, e = scriptBody(m, current.Body)
	return current, e
}

func isolatedScript(ctx context.Context, source string, input json.RawMessage) (json.RawMessage, error) {
	out, e := rewritescript.Execute(ctx, source, input)
	if e == nil {
		return out, nil
	}
	code := "rewrite_script_execution"
	switch {
	case errors.Is(e, rewritescript.ErrBusy), errors.Is(e, rewritescript.ErrLimit):
		code = "rewrite_limit"
	case errors.Is(e, rewritescript.ErrTimeout):
		code = "rewrite_script_timeout"
	case errors.Is(e, rewritescript.ErrCanceled):
		code = "rewrite_cancelled"
	case errors.Is(e, rewritescript.ErrOutput):
		code = "rewrite_script_output"
	}
	return nil, &Error{Code: code}
}

// PrepareScripts warms the fixed guest before traffic admission. No script is
// run during preparation, and no external capabilities are instantiated.
func (s *Snapshot) PrepareScripts(ctx context.Context) error {
	for _, r := range s.doc.Rules {
		if r.Enabled {
			for _, o := range r.Operations {
				if o.Op == "script" {
					if e := rewritescript.Prepare(ctx); e != nil {
						return &Error{Code: "rewrite_script_execution"}
					}
					return nil
				}
			}
		}
	}
	return nil
}
