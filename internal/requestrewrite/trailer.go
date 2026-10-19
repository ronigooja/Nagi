package requestrewrite

import (
	"io"
	"net/http"
	"strings"
	"sync"
)

func ValidTrailerName(name string) bool {
	if !token(name) {
		return false
	}
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "proxy-authenticate", "www-authenticate", "cookie", "set-cookie", "host", "content-length", "transfer-encoding", "trailer", "connection", "keep-alive", "upgrade", "te", "content-encoding", "content-type", "content-range", "range", "cache-control", "expect", "max-forwards":
		return false
	}
	return true
}

// TransformTrailers applies only explicit trailer operations to a defensive copy.
func TransformTrailers(original http.Header, ops []Operation) (http.Header, error) {
	out := original.Clone()
	if out == nil {
		out = http.Header{}
	}
	for name, values := range out {
		if !ValidTrailerName(name) {
			return nil, invalid()
		}
		for _, value := range values {
			if !cleanValue(value) {
				return nil, invalid()
			}
		}
	}
	for _, op := range ops {
		if !ValidTrailerName(op.Name) {
			return nil, invalid()
		}
		switch op.Op {
		case "trailer_set":
			value, e := stringValue(op)
			if e != nil || !cleanValue(value) {
				return nil, invalid()
			}
			deleteHeader(out, op.Name)
			out.Set(op.Name, value)
		case "trailer_delete":
			deleteHeader(out, op.Name)
		default:
			return nil, invalid()
		}
	}
	n := 0
	for name, values := range out {
		for _, v := range values {
			n += len(name) + len(v) + 4
		}
	}
	if n > MaxHeadersBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	return out, nil
}

// TrailerBody waits for upstream EOF before reading trailers. The destination
// map must be the same map installed on the outgoing request or response.
func TrailerBody(source io.ReadCloser, original, destination http.Header, ops []Operation) io.ReadCloser {
	return &trailerBody{source: source, original: original, destination: destination, ops: append([]Operation(nil), ops...)}
}

type trailerBody struct {
	source                io.ReadCloser
	original, destination http.Header
	ops                   []Operation
	once                  sync.Once
	err                   error
}

func (b *trailerBody) Read(p []byte) (int, error) {
	n, e := b.source.Read(p)
	if e == io.EOF {
		b.once.Do(func() {
			var out http.Header
			out, b.err = TransformTrailers(b.original, b.ops)
			if b.err == nil {
				for k := range b.destination {
					delete(b.destination, k)
				}
				for k, v := range out {
					b.destination[k] = v
				}
			}
		})
		if b.err != nil {
			return n, b.err
		}
	}
	return n, e
}
func (b *trailerBody) Close() error { return b.source.Close() }
