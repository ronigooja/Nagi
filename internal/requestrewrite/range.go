package requestrewrite

import (
	"bytes"
	"context"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// ValidateContentRange accepts only a complete, satisfiable bytes fragment.
func ValidateContentRange(value string, size int) error {
	if !strings.HasPrefix(value, "bytes ") {
		return invalid()
	}
	span, total, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	if !ok {
		return invalid()
	}
	lo, hi, ok := strings.Cut(span, "-")
	if !ok {
		return invalid()
	}
	parse := func(v string) (int64, error) {
		if v == "" || len(v) > 1 && v[0] == '0' {
			return 0, invalid()
		}
		for _, c := range v {
			if c < '0' || c > '9' {
				return 0, invalid()
			}
		}
		return strconv.ParseInt(v, 10, 64)
	}
	start, e := parse(lo)
	if e != nil {
		return invalid()
	}
	end, e := parse(hi)
	if e != nil || end < start || end-start != int64(size)-1 {
		return invalid()
	}
	if total != "*" {
		n, e := parse(total)
		if e != nil || n <= end {
			return invalid()
		}
	}
	return nil
}

// TransformFragments retains Content-Range and multipart envelope bytes exactly.
// Each transformed part must retain its original encoded byte length.
func TransformFragments(ctx context.Context, raw []byte, h http.Header, ops []Operation) ([]byte, error) {
	return TransformFragmentsWithHook(ctx, raw, h, ops, nil)
}
func TransformFragmentsWithHook(ctx context.Context, raw []byte, h http.Header, ops []Operation, hook BodyOperationHook) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, BodyTransformTimeout)
	defer cancel()
	if len(raw) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	media, params, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil {
		return nil, invalid()
	}
	if media != "multipart/byteranges" {
		if len(h.Values("Content-Range")) != 1 || ValidateContentRange(h.Get("Content-Range"), len(raw)) != nil {
			return nil, invalid()
		}
		out, e := BodyTransformWithHook(ctx, raw, h, ops, hook)
		if e != nil {
			return nil, e
		}
		if len(out) != len(raw) {
			return nil, invalid()
		}
		return out, nil
	}
	// Content encoding applies to the multipart representation, not individual parts.
	if len(h.Values("Content-Encoding")) > 0 {
		return nil, invalid()
	}
	boundary := params["boundary"]
	if boundary == "" || len(boundary) > 70 || strings.ContainsAny(boundary, "\r\n") {
		return nil, invalid()
	}
	marker := []byte("--" + boundary)
	if !bytes.HasPrefix(raw, marker) {
		return nil, invalid()
	}
	out := append([]byte(nil), raw...)
	position := 0
	parts := 0
	for {
		if !bytes.HasPrefix(raw[position:], marker) {
			return nil, invalid()
		}
		position += len(marker)
		if bytes.HasPrefix(raw[position:], []byte("--\r\n")) {
			position += 4
			break
		}
		if bytes.HasPrefix(raw[position:], []byte("--")) {
			position += 2
			break
		}
		if !bytes.HasPrefix(raw[position:], []byte("\r\n")) {
			return nil, invalid()
		}
		position += 2
		end := bytes.Index(raw[position:], []byte("\r\n\r\n"))
		if end < 0 {
			return nil, invalid()
		}
		head := raw[position : position+end]
		position += end + 4
		partHeader := http.Header{}
		for _, line := range bytes.Split(head, []byte("\r\n")) {
			name, value, ok := strings.Cut(string(line), ":")
			if !ok || !token(name) || !cleanValue(strings.TrimSpace(value)) {
				return nil, invalid()
			}
			partHeader.Add(name, strings.TrimSpace(value))
		}
		next := bytes.Index(raw[position:], append([]byte("\r\n"), marker...))
		if next < 0 {
			return nil, invalid()
		}
		body := raw[position : position+next]
		if len(partHeader.Values("Content-Range")) != 1 || ValidateContentRange(partHeader.Get("Content-Range"), len(body)) != nil {
			return nil, invalid()
		}
		changed, e := BodyTransformWithHook(ctx, body, partHeader, ops, hook)
		if e != nil {
			return nil, e
		}
		if len(changed) != len(body) {
			return nil, invalid()
		}
		copy(out[position:], changed)
		parts++
		position += next + 2
	}
	if position != len(raw) || parts == 0 {
		return nil, invalid()
	}
	return out, nil
}
