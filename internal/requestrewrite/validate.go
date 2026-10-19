package requestrewrite

import (
	"encoding/base64"
	"encoding/json"
	"mime"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// NormalizeDomain accepts validated ASCII A-labels only, never Unicode input.
func NormalizeDomain(input string) (string, error) {
	s := strings.TrimSuffix(strings.ToLower(input), ".")
	if len(s) == 0 || len(s) > 253 || net.ParseIP(s) != nil {
		return "", invalid()
	}
	for _, c := range s {
		if c > 127 || !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return "", invalid()
		}
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", invalid()
		}
	}
	a, e := idna.Lookup.ToASCII(s)
	if e != nil || a != s {
		return "", invalid()
	}
	u, e := idna.Lookup.ToUnicode(s)
	if e != nil {
		return "", invalid()
	}
	a, e = idna.Lookup.ToASCII(u)
	if e != nil || a != s {
		return "", invalid()
	}
	return s, nil
}
func normalizePattern(s string) (string, error) {
	wild := strings.HasPrefix(s, "*.")
	if wild {
		s = s[2:]
	}
	s, e := NormalizeDomain(s)
	if e != nil {
		return "", e
	}
	if wild {
		suffix, _ := publicsuffix.PublicSuffix(s)
		if suffix == s || !strings.Contains(s, ".") {
			return "", invalid()
		}
		return "*." + s, nil
	}
	return s, nil
}
func patternMatches(pattern, host string) bool {
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:]
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return pattern == host
}

// InterceptDomains and TargetDomains contain only enabled rule policy.
func (s *Snapshot) InterceptDomains() []string { return s.policy(false) }
func (s *Snapshot) TargetDomains() []string    { return s.policy(true) }
func (s *Snapshot) HasEnabledRules() bool      { return len(s.InterceptDomains()) > 0 }
func (s *Snapshot) policy(targets bool) []string {
	set := map[string]bool{}
	for _, r := range s.doc.Rules {
		if !r.Enabled {
			continue
		}
		set[r.Match.Domain] = true
		if targets && r.Stage == StageRequest {
			for _, c := range r.RedirectConsent {
				set[c.Domain] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
func (s *Snapshot) Authorized(host string) bool       { return s.permits(host, s.InterceptDomains()) }
func (s *Snapshot) TargetAuthorized(host string) bool { return s.permits(host, s.TargetDomains()) }
func (s *Snapshot) permits(host string, patterns []string) bool {
	h, e := NormalizeDomain(host)
	if e != nil {
		return false
	}
	for _, p := range patterns {
		if patternMatches(p, h) {
			return true
		}
	}
	return false
}
func token(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}
func cleanValue(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func allowedHeader(s string) bool {
	if !token(s) {
		return false
	}
	s = strings.ToLower(s)
	if strings.HasPrefix(s, "nagi-rewrite-") || strings.HasPrefix(s, "sec-websocket-") {
		return false
	}
	switch s {
	case "host", "content-length", "transfer-encoding", "content-encoding", "connection", "upgrade", "trailer", "proxy-authorization", "proxy-authenticate", "keep-alive", "te", "proxy-connection":
		return false
	}
	return true
}
func stringValue(o Operation) (string, error) {
	var s string
	if len(o.Value) == 0 || json.Unmarshal(o.Value, &s) != nil || string(o.Value) == "null" {
		return "", invalid()
	}
	return s, nil
}
func validateURLValue(component, value string) error {
	switch component {
	case "scheme":
		if value != "http" && value != "https" {
			return invalid()
		}
	case "host":
		if _, e := NormalizeDomain(value); e != nil {
			return e
		}
	case "port":
		if value != "" {
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 || n > 65535 {
				return invalid()
			}
			for _, c := range value {
				if c < '0' || c > '9' {
					return invalid()
				}
			}
		}
	case "path", "query":
		if !cleanValue(value) || strings.Contains(value, "#") {
			return invalid()
		}
		if component == "path" && !strings.HasPrefix(value, "/") {
			return invalid()
		}
		if component == "query" && strings.HasPrefix(value, "?") {
			return invalid()
		}
		if _, e := url.PathUnescape(value); e != nil {
			return invalid()
		}
		for i := 0; i < len(value); i++ {
			c := value[i]
			if c == '%' {
				i += 2
				continue
			}
			if c > 127 || !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~!$&'()*+,;=:@/", rune(c)) || component == "query" && c == '?') {
				return invalid()
			}
		}
		if component == "path" {
			u, e := url.ParseRequestURI(value)
			if e != nil || u.RawQuery != "" || u.ForceQuery || u.Host != "" {
				return invalid()
			}
		}
	default:
		return invalid()
	}
	return nil
}
func validateDocument(d *Document) error {
	if d.Version != 2 || d.Rules == nil || d.Scripts == nil || len(d.Rules) > 1024 || len(d.Scripts) > 1024 {
		return invalid()
	}
	scripts := map[string]Script{}
	for _, s := range d.Scripts {
		if !validID(s.ID) || scripts[s.ID].ID != "" || s.BodyAccess != "none" && s.BodyAccess != "buffer" || len(s.Source) == 0 || len(s.Source) > MaxScriptBytes || !utf8.ValidString(s.Source) {
			return invalid()
		}
		scripts[s.ID] = s
	}
	ids := map[string]bool{}
	for i := range d.Rules {
		r := &d.Rules[i]
		if !validID(r.ID) || ids[r.ID] || r.RedirectConsent == nil || len(r.Operations) < 1 || len(r.Operations) > 32 {
			return invalid()
		}
		ids[r.ID] = true
		if r.Stage != StageRequest && r.Stage != StageResponse && r.Stage != StageWebSocketClient && r.Stage != StageWebSocketServer {
			return invalid()
		}
		var e error
		r.Match.Domain, e = normalizePattern(r.Match.Domain)
		if e != nil {
			return e
		}
		m := &r.Match
		if len(m.Methods) > 16 || len(m.PathPrefix) > 2048 || m.PathPrefix != "" && validateURLValue("path", m.PathPrefix) != nil {
			return invalid()
		}
		for _, v := range m.Methods {
			if !token(v) || strings.ToUpper(v) != v {
				return invalid()
			}
		}
		if len(m.PathRegex) > 8192 {
			return invalid()
		}
		if m.PathRegex != "" {
			if _, e := regexp.Compile(m.PathRegex); e != nil {
				return invalid()
			}
		}
		for pi, pairs := range []map[string][]string{m.Headers, m.ResponseHeaders, m.Query} {
			seen := map[string]bool{}
			for name, vs := range pairs {
				key := name
				if pi < 2 {
					key = strings.ToLower(name)
					if !token(name) {
						return invalid()
					}
				}
				if seen[key] || !cleanValue(name) || len(vs) == 0 {
					return invalid()
				}
				seen[key] = true
				for _, v := range vs {
					if !cleanValue(v) {
						return invalid()
					}
				}
			}
		}
		if r.Stage == StageRequest && (len(m.StatusCodes) > 0 || len(m.ContentTypes) > 0 || len(m.ResponseHeaders) > 0) || r.Stage != StageWebSocketClient && r.Stage != StageWebSocketServer && len(m.MessageTypes) > 0 {
			return invalid()
		}
		for _, n := range m.StatusCodes {
			if n < 100 || n > 599 {
				return invalid()
			}
		}
		for j, v := range m.ContentTypes {
			media, p, e := mime.ParseMediaType(v)
			if e != nil || len(p) > 0 {
				return invalid()
			}
			m.ContentTypes[j] = media
		}
		for _, v := range m.MessageTypes {
			if v != "text" && v != "binary" {
				return invalid()
			}
		}
		if r.OnError != "block" && r.OnError != "forward_original" {
			return invalid()
		}
		if r.RangeMode == "" {
			r.RangeMode = "fragment"
		}
		if r.RangeMode != "fragment" && r.RangeMode != "full" {
			return invalid()
		}
		if r.Stage != StageRequest && len(r.RedirectConsent) > 0 {
			return invalid()
		}
		for j := range r.RedirectConsent {
			c := &r.RedirectConsent[j]
			c.Domain, e = normalizePattern(c.Domain)
			if e != nil || len(c.Schemes) == 0 || len(c.Ports) == 0 {
				return invalid()
			}
			for _, s := range c.Schemes {
				if s != "http" && s != "https" {
					return invalid()
				}
			}
			for _, p := range c.Ports {
				if p < 1 || p > 65535 {
					return invalid()
				}
			}
		}
		for j := range r.Operations {
			o := &r.Operations[j]
			ws := r.Stage == StageWebSocketClient || r.Stage == StageWebSocketServer
			if o.MessageType != "" && (!ws || o.MessageType != "text" && o.MessageType != "binary" || !bodyOperation(*o) && o.Op != "script") {
				return invalid()
			}
			switch o.Op {
			case "header_set", "header_append", "header_delete", "trailer_set", "trailer_delete":
				if !allowedHeader(o.Name) || strings.HasPrefix(o.Op, "trailer_") && (ws || !allowedTrailer(o.Name)) {
					return invalid()
				}
				if o.Op == "header_set" || o.Op == "header_append" || o.Op == "trailer_set" {
					v, e := stringValue(*o)
					if e != nil || len(v) > 8192 || !cleanValue(v) {
						return invalid()
					}
				}
			case "url_set":
				if r.Stage != StageRequest {
					return invalid()
				}
				v, e := stringValue(*o)
				if e != nil || validateURLValue(o.Component, v) != nil {
					return invalid()
				}
				if o.Component == "host" {
					v, _ = NormalizeDomain(v)
					o.Value, _ = json.Marshal(v)
				}
			case "method_set":
				v, e := stringValue(*o)
				if r.Stage != StageRequest || e != nil || !token(v) || v != strings.ToUpper(v) || v == "CONNECT" {
					return invalid()
				}
			case "status_set":
				var n int
				if r.Stage != StageResponse || json.Unmarshal(o.Value, &n) != nil || n < 200 || n > 599 {
					return invalid()
				}
			case "json_set", "json_delete":
				if _, e := pointerParts(o.Pointer); e != nil {
					return e
				}
				if o.Op == "json_delete" && (o.Pointer == "" || strings.HasSuffix(o.Pointer, "/-")) {
					return invalid()
				}
				if o.Op == "json_set" {
					if _, e := decodeJSON(o.Value); e != nil {
						return e
					}
				}
			case "body_set":
				v, e := stringValue(*o)
				if e != nil || o.Encoding != "text" && o.Encoding != "base64" {
					return invalid()
				}
				if o.Encoding == "base64" {
					if _, e := base64.StdEncoding.Strict().DecodeString(v); e != nil || strings.ContainsAny(v, "\r\n") {
						return invalid()
					}
				}
			case "text_replace", "regex_replace":
				if o.Pattern == "" || !utf8.ValidString(o.Pattern) || !utf8.ValidString(o.Replacement) {
					return invalid()
				}
				if o.Op == "regex_replace" {
					if _, e := regexp.Compile(o.Pattern); e != nil {
						return invalid()
					}
				}
			case "script":
				if scripts[o.Script].ID == "" {
					return invalid()
				}
			default:
				return invalid()
			}
		}
	}
	s := &Snapshot{doc: *d}
	if len(s.InterceptDomains()) > 256 || len(s.TargetDomains()) > 256 {
		return &Error{Code: "rewrite_limit"}
	}
	return nil
}
func bodyOperation(o Operation) bool {
	switch o.Op {
	case "json_set", "json_delete", "body_set", "text_replace", "regex_replace":
		return true
	}
	return false
}
func allowedTrailer(name string) bool { return ValidTrailerName(name) && allowedHeader(name) }

// MarshalJSON preserves the exact fields required for each operation.
func (o Operation) MarshalJSON() ([]byte, error) {
	m := map[string]any{"op": o.Op}
	switch o.Op {
	case "header_set", "header_append", "trailer_set":
		m["name"] = o.Name
		m["value"] = o.Value
	case "header_delete", "trailer_delete":
		m["name"] = o.Name
	case "url_set":
		m["component"] = o.Component
		m["value"] = o.Value
	case "method_set", "status_set":
		m["value"] = o.Value
	case "json_set":
		m["pointer"] = o.Pointer
		m["value"] = o.Value
	case "json_delete":
		m["pointer"] = o.Pointer
	case "body_set":
		m["value"] = o.Value
		m["encoding"] = o.Encoding
	case "text_replace", "regex_replace":
		m["pattern"] = o.Pattern
		m["replacement"] = o.Replacement
		m["all"] = o.All
	case "script":
		m["script"] = o.Script
	default:
		return nil, invalid()
	}
	if o.MessageType != "" {
		m["message_type"] = o.MessageType
	}
	return json.Marshal(m)
}
