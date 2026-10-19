package requestrewrite

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Result struct {
	Request     *http.Request
	RuleIDs     []string
	Outcome     string
	RuleVersion string
	RangeMode   string
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
