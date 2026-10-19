// Package requestrewrite owns validated, profile-local request transformations.
// It performs no network operations. Runtime activation belongs to its caller.
package requestrewrite

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxDocumentBytes = 1 << 20
const MaxScriptBytes = 64 << 10
const MaxTrailersBytes = 16 << 10
const MaxWorkspaceBytes = 64 << 20
const MaxBodyBytes = 8 << 20
const MaxHeadersBytes = 64 << 10

type Error struct {
	Code   string
	RuleID string
}

func (e *Error) Error() string {
	if e.RuleID != "" {
		return e.Code + ": rule " + e.RuleID
	}
	return e.Code
}
func invalid() error { return &Error{Code: "rewrite_invalid"} }

const (
	StageRequest         = "request"
	StageResponse        = "response"
	StageWebSocketClient = "websocket_client"
	StageWebSocketServer = "websocket_server"
)

type Document struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	Rules   []Rule   `json:"rules"`
	Scripts []Script `json:"scripts"`
}
type Script struct {
	ID         string `json:"id"`
	BodyAccess string `json:"body_access"`
	Source     string `json:"source"`
}
type Match struct {
	Domain          string              `json:"domain"`
	Methods         []string            `json:"methods,omitempty"`
	PathPrefix      string              `json:"path_prefix,omitempty"`
	PathRegex       string              `json:"path_regex,omitempty"`
	Query           map[string][]string `json:"query,omitempty"`
	Headers         map[string][]string `json:"headers,omitempty"`
	StatusCodes     []int               `json:"status_codes,omitempty"`
	ContentTypes    []string            `json:"content_types,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	MessageTypes    []string            `json:"message_types,omitempty"`
}
type Consent struct {
	Domain  string   `json:"domain"`
	Schemes []string `json:"schemes"`
	Ports   []int    `json:"ports"`
}
type Rule struct {
	ID              string      `json:"id"`
	Enabled         bool        `json:"enabled"`
	Stage           string      `json:"stage"`
	Match           Match       `json:"match"`
	OnError         string      `json:"on_error"`
	RangeMode       string      `json:"range_mode,omitempty"`
	RedirectConsent []Consent   `json:"redirect_consent"`
	Operations      []Operation `json:"operations"`
}
type Operation struct {
	Op          string          `json:"op"`
	Name        string          `json:"name,omitempty"`
	Component   string          `json:"component,omitempty"`
	Pointer     string          `json:"pointer,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
	Pattern     string          `json:"pattern,omitempty"`
	Replacement string          `json:"replacement,omitempty"`
	All         bool            `json:"all,omitempty"`
	Encoding    string          `json:"encoding,omitempty"`
	Script      string          `json:"script,omitempty"`
	MessageType string          `json:"message_type,omitempty"`
}

// Snapshot exposes copies only; callers cannot mutate a request's rules/version.
type Snapshot struct {
	doc       Document
	canonical []byte
	version   string
	regex     map[string]*regexp.Regexp
}

func (s *Snapshot) Version() string    { return s.version }
func (s *Snapshot) Document() Document { var d Document; _ = json.Unmarshal(s.canonical, &d); return d }

// ExportSensitive contains raw rule values. Portable exports never enable rewriting.
func (s *Snapshot) ExportSensitive() []byte {
	d := s.Document()
	d.Enabled = false
	b, _ := marshalJSON(d)
	return b
}
func Empty() *Snapshot {
	s, _ := Compile(Document{Version: 2, Rules: []Rule{}, Scripts: []Script{}})
	return s
}
func validID(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// decodeJSON rejects duplicate keys, invalid UTF-8, trailing values and excessive depth.
func decodeJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, invalid()
	}
	return decodeJSONBudget(data, 0)
}
func decodeJSONBudget(data []byte, budget int) (any, error) {
	if !utf8.Valid(data) {
		return nil, invalid()
	}
	used := 0
	charge := func(n int) error {
		used += n
		if budget > 0 && used > budget {
			return &Error{Code: "rewrite_limit"}
		}
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 256 {
			return nil, invalid()
		}
		t, e := d.Token()
		if e != nil {
			return nil, invalid()
		}
		if e := charge(48); e != nil {
			return nil, e
		}
		switch v := t.(type) {
		case json.Delim:
			switch v {
			case '{':
				if e := charge(256); e != nil {
					return nil, e
				}
				m := map[string]any{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return nil, invalid()
					}
					key, ok := k.(string)
					if e := charge(len(key) + 128); e != nil {
						return nil, e
					}
					if !ok {
						return nil, invalid()
					}
					if _, ok = m[key]; ok {
						return nil, invalid()
					}
					x, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					m[key] = x
				}
				if t, e = d.Token(); e != nil || t != json.Delim('}') {
					return nil, invalid()
				}
				return m, nil
			case '[':
				a := []any{}
				for d.More() {
					x, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					a = append(a, x)
				}
				if t, e = d.Token(); e != nil || t != json.Delim(']') {
					return nil, invalid()
				}
				return a, nil
			}
			return nil, invalid()
		case string:
			if e := charge(len(v)); e != nil {
				return nil, e
			}
			return v, nil
		default:
			return v, nil
		}
	}
	v, e := read(0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, invalid()
	}
	return v, nil
}
func fields(v any, names ...string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != len(names) {
		return nil, invalid()
	}
	for _, k := range names {
		if _, ok = m[k]; !ok {
			return nil, invalid()
		}
	}
	return m, nil
}

// schema rejects unknown, missing, null and incorrectly typed fields using the
// current contract only. The typed decoder below checks every nested field.
func schema(v any) error {
	m, e := fields(v, "version", "enabled", "rules", "scripts")
	if e != nil {
		return e
	}
	if _, ok := m["enabled"].(bool); !ok {
		return invalid()
	}
	rs, ok := m["rules"].([]any)
	if !ok {
		return invalid()
	}
	ss, ok := m["scripts"].([]any)
	if !ok {
		return invalid()
	}
	for _, s := range ss {
		if _, e := fields(s, "id", "body_access", "source"); e != nil {
			return e
		}
	}
	for _, v := range rs {
		r, ok := v.(map[string]any)
		if !ok {
			return invalid()
		}
		for _, k := range []string{"id", "enabled", "stage", "match", "on_error", "redirect_consent", "operations"} {
			if r[k] == nil {
				return invalid()
			}
		}
		if _, ok := r["enabled"].(bool); !ok {
			return invalid()
		}
		match, ok := r["match"].(map[string]any)
		if !ok || match["domain"] == nil {
			return invalid()
		}
		if _, ok := r["redirect_consent"].([]any); !ok {
			return invalid()
		}
		ops, ok := r["operations"].([]any)
		if !ok {
			return invalid()
		}
		for _, v := range ops {
			o, ok := v.(map[string]any)
			if !ok {
				return invalid()
			}
			var keys []string
			switch o["op"] {
			case "header_set", "header_append", "trailer_set":
				keys = []string{"op", "name", "value"}
			case "header_delete", "trailer_delete":
				keys = []string{"op", "name"}
			case "url_set":
				keys = []string{"op", "component", "value"}
			case "method_set", "status_set":
				keys = []string{"op", "value"}
			case "json_set":
				keys = []string{"op", "pointer", "value"}
			case "json_delete":
				keys = []string{"op", "pointer"}
			case "body_set":
				keys = []string{"op", "value", "encoding"}
			case "text_replace", "regex_replace":
				keys = []string{"op", "pattern", "replacement", "all"}
			case "script":
				keys = []string{"op", "script"}
			default:
				return invalid()
			}
			// Message type changes are explicit and only legal for WS body operations.
			if _, ok := o["message_type"]; ok {
				keys = append(keys, "message_type")
			}
			if _, e := fields(v, keys...); e != nil {
				return e
			}
			for _, k := range keys {
				if k == "value" {
					continue
				}
				if k == "all" {
					if _, ok := o[k].(bool); !ok {
						return invalid()
					}
				} else {
					if _, ok := o[k].(string); !ok {
						return invalid()
					}
				}
			}
		}
	}
	return nil
}
func Parse(data []byte) (*Snapshot, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	v, e := decodeJSON(data)
	if e != nil {
		return nil, e
	}
	if e = schema(v); e != nil {
		return nil, e
	}
	var d Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil {
		return nil, invalid()
	}
	return compile(d)
}

// marshalJSON uses the same string representation as stored canonical documents.
// HTML/JSONP escaping must not expand an accepted document beyond its byte limit.
func marshalJSON(value any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if e := encoder.Encode(value); e != nil {
		return nil, e
	}
	decoded, e := decodeJSON(encoded.Bytes())
	if e != nil {
		return nil, e
	}
	return canonicalJSON(decoded), nil
}

// Compile validates a defensive JSON copy, including exact operation fields.
func Compile(doc Document) (*Snapshot, error) {
	b, e := marshalJSON(doc)
	if e != nil {
		return nil, invalid()
	}
	return Parse(b)
}
func compile(d Document) (*Snapshot, error) {
	if e := validateDocument(&d); e != nil {
		return nil, e
	}
	b, e := marshalJSON(d)
	if e != nil {
		return nil, invalid()
	}
	v, e := decodeJSON(b)
	if e != nil {
		return nil, e
	}
	canonical := canonicalJSON(v)
	if len(canonical) > MaxDocumentBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	sum := sha256.Sum256(canonical)
	s := &Snapshot{doc: d, canonical: canonical, version: "sha256:" + hex.EncodeToString(sum[:]), regex: map[string]*regexp.Regexp{}}
	for _, r := range d.Rules {
		if r.Match.PathRegex != "" {
			s.regex[r.Match.PathRegex] = regexp.MustCompile(r.Match.PathRegex)
		}
		for _, o := range r.Operations {
			if o.Op == "regex_replace" {
				s.regex[o.Pattern] = regexp.MustCompile(o.Pattern)
			}
		}
	}
	return s, nil
}
func canonicalJSON(v any) []byte {
	var b bytes.Buffer
	var write func(any)
	write = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(quote(k))
				b.WriteByte(':')
				write(x[k])
			}
			b.WriteByte('}')
		case []any:
			b.WriteByte('[')
			for i, v := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				write(v)
			}
			b.WriteByte(']')
		case string:
			b.WriteString(quote(x))
		case json.Number:
			b.WriteString(string(x))
		case bool:
			b.WriteString(strconv.FormatBool(x))
		case nil:
			b.WriteString("null")
		default:
			panic(fmt.Sprintf("unsupported canonical type %T", v))
		}
	}
	write(v)
	return b.Bytes()
}
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			if r < 32 {
				fmt.Fprintf(&b, "\\u%04x", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ValidateRuleJSON validates a standalone rule's JSON shape with the document
// parser. Semantic validation (including script references) still requires
// Compile with the complete document. Values never appear in returned errors.
func ValidateRuleJSON(data []byte) error {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return &Error{Code: "rewrite_limit"}
	}
	value, e := decodeJSON(data)
	if e != nil {
		return e
	}
	if e = schema(map[string]any{"version": json.Number("2"), "enabled": false, "rules": []any{value}, "scripts": []any{}}); e != nil {
		return e
	}
	var rule Rule
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&rule) != nil {
		return invalid()
	}
	return nil
}
