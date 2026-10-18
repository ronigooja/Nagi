package requestrewrite

import (
	"encoding/json"
	"net"
	"net/url"
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
func contained(pattern, authorized string) bool {
	if pattern == authorized {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasPrefix(authorized, "*.") && strings.HasSuffix(pattern[2:], authorized[1:])
	}
	return patternMatches(authorized, pattern)
}
func (s *Snapshot) Authorized(host string) bool {
	h, e := NormalizeDomain(host)
	if e != nil {
		return false
	}
	for _, d := range s.doc.Domains {
		if patternMatches(d.Pattern, h) {
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
	if strings.HasPrefix(s, "nagi-rewrite-") {
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
	if d.Version != 1 || d.Domains == nil || d.Rules == nil || len(d.Domains) > 256 || len(d.Rules) > 1024 {
		return invalid()
	}
	ids := map[string]bool{}
	for i := range d.Domains {
		a := &d.Domains[i]
		if !validID(a.ID) || ids[a.ID] {
			return invalid()
		}
		ids[a.ID] = true
		v, e := normalizePattern(a.Pattern)
		if e != nil {
			return e
		}
		a.Pattern = v
	}
	authorized := func(p string) bool {
		for _, a := range d.Domains {
			if contained(p, a.Pattern) {
				return true
			}
		}
		return false
	}
	ids = map[string]bool{}
	for i := range d.Rules {
		r := &d.Rules[i]
		if !validID(r.ID) || ids[r.ID] || r.RedirectConsent == nil || r.Operations == nil || len(r.Operations) < 1 || len(r.Operations) > 32 {
			return invalid()
		}
		ids[r.ID] = true
		var e error
		r.Match.Domain, e = normalizePattern(r.Match.Domain)
		if e != nil || !authorized(r.Match.Domain) {
			return invalid()
		}
		if len(r.Match.Methods) < 1 || len(r.Match.Methods) > 16 || len(r.Match.PathPrefix) > 2048 || !utf8.ValidString(r.Match.PathPrefix) || !strings.HasPrefix(r.Match.PathPrefix, "/") {
			return invalid()
		}
		for _, m := range r.Match.Methods {
			if !token(m) || m != strings.ToUpper(m) {
				return invalid()
			}
		}
		if r.OnError != "block" && r.OnError != "forward_original" {
			return invalid()
		}
		for j := range r.RedirectConsent {
			c := &r.RedirectConsent[j]
			c.Domain, e = normalizePattern(c.Domain)
			if e != nil || !authorized(c.Domain) || len(c.Schemes) < 1 || len(c.Ports) < 1 {
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
			switch o.Op {
			case "header_set", "header_delete":
				if !allowedHeader(o.Name) {
					return invalid()
				}
				if o.Op == "header_set" {
					v, e := stringValue(*o)
					if e != nil || len(v) > 8192 || !cleanValue(v) {
						return invalid()
					}
				}
			case "url_set":
				v, e := stringValue(*o)
				if e != nil || validateURLValue(o.Component, v) != nil {
					return invalid()
				}
				if o.Component == "host" {
					v, _ = NormalizeDomain(v)
					if !authorized(v) {
						return invalid()
					}
					o.Value, _ = json.Marshal(v)
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
			default:
				return invalid()
			}
		}
	}
	return nil
}

// MarshalJSON preserves required empty pointers and exact operation-specific fields.
func (o Operation) MarshalJSON() ([]byte, error) {
	m := map[string]any{"op": o.Op}
	switch o.Op {
	case "header_set":
		m["name"] = o.Name
		m["value"] = o.Value
	case "header_delete":
		m["name"] = o.Name
	case "url_set":
		m["component"] = o.Component
		m["value"] = o.Value
	case "json_set":
		m["pointer"] = o.Pointer
		m["value"] = o.Value
	case "json_delete":
		m["pointer"] = o.Pointer
	default:
		return nil, invalid()
	}
	return json.Marshal(m)
}
