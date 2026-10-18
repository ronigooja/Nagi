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
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxDocumentBytes = 256 << 10
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

type Document struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	Domains []Domain `json:"domains"`
	Rules   []Rule   `json:"rules"`
}
type Domain struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"`
}
type Match struct {
	Domain     string   `json:"domain"`
	Methods    []string `json:"methods"`
	PathPrefix string   `json:"path_prefix"`
}
type Consent struct {
	Domain  string   `json:"domain"`
	Schemes []string `json:"schemes"`
	Ports   []int    `json:"ports"`
}
type Rule struct {
	ID              string      `json:"id"`
	Enabled         bool        `json:"enabled"`
	Match           Match       `json:"match"`
	OnError         string      `json:"on_error"`
	RedirectConsent []Consent   `json:"redirect_consent"`
	Operations      []Operation `json:"operations"`
}
type Operation struct {
	Op        string          `json:"op"`
	Name      string          `json:"name,omitempty"`
	Component string          `json:"component,omitempty"`
	Pointer   string          `json:"pointer,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
}

// Snapshot exposes copies only; callers cannot mutate a request's rules/version.
type Snapshot struct {
	doc       Document
	canonical []byte
	version   string
}

func (s *Snapshot) Version() string    { return s.version }
func (s *Snapshot) Document() Document { var d Document; _ = json.Unmarshal(s.canonical, &d); return d }

// ExportSensitive contains raw rule values. Portable exports never enable rewriting.
func (s *Snapshot) ExportSensitive() []byte {
	d := s.Document()
	d.Enabled = false
	b, _ := json.Marshal(d)
	return b
}
func Empty() *Snapshot {
	s, _ := Compile(Document{Version: 1, Domains: []Domain{}, Rules: []Rule{}})
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
func schema(v any) error {
	m, e := fields(v, "version", "enabled", "domains", "rules")
	if e != nil {
		return e
	}
	if _, ok := m["enabled"].(bool); !ok {
		return invalid()
	}
	ds, ok := m["domains"].([]any)
	if !ok {
		return invalid()
	}
	rs, ok := m["rules"].([]any)
	if !ok {
		return invalid()
	}
	for _, d := range ds {
		if _, e = fields(d, "id", "pattern"); e != nil {
			return e
		}
	}
	for _, r := range rs {
		rm, e := fields(r, "id", "enabled", "match", "on_error", "redirect_consent", "operations")
		if e != nil {
			return e
		}
		if _, ok := rm["enabled"].(bool); !ok {
			return invalid()
		}
		if _, e = fields(rm["match"], "domain", "methods", "path_prefix"); e != nil {
			return e
		}
		cs, ok := rm["redirect_consent"].([]any)
		if !ok {
			return invalid()
		}
		for _, c := range cs {
			if _, e = fields(c, "domain", "schemes", "ports"); e != nil {
				return e
			}
		}
		ops, ok := rm["operations"].([]any)
		if !ok {
			return invalid()
		}
		for _, o := range ops {
			om, ok := o.(map[string]any)
			if !ok {
				return invalid()
			}
			var keys []string
			switch om["op"] {
			case "header_set":
				keys = []string{"op", "name", "value"}
			case "header_delete":
				keys = []string{"op", "name"}
			case "url_set":
				keys = []string{"op", "component", "value"}
			case "json_set":
				keys = []string{"op", "pointer", "value"}
			case "json_delete":
				keys = []string{"op", "pointer"}
			default:
				return invalid()
			}
			if _, e = fields(o, keys...); e != nil {
				return e
			}
			for _, key := range keys {
				if key == "value" {
					continue
				}
				if _, ok := om[key].(string); !ok {
					return invalid()
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
	if json.Unmarshal(data, &d) != nil {
		return nil, invalid()
	}
	return compile(d)
}

// Compile validates a defensive JSON copy, including exact operation fields.
func Compile(doc Document) (*Snapshot, error) {
	b, e := json.Marshal(doc)
	if e != nil {
		return nil, invalid()
	}
	return Parse(b)
}
func compile(d Document) (*Snapshot, error) {
	if e := validateDocument(&d); e != nil {
		return nil, e
	}
	b, e := json.Marshal(d)
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
	return &Snapshot{doc: d, canonical: canonical, version: "sha256:" + hex.EncodeToString(sum[:])}, nil
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
