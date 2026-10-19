package requestrewrite

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
)

const BodyTransformTimeout = 5 * time.Second
const BodyWorkspaceBytes = 64 << 20

// ReadBodyBounded closes the source on completion, cancellation, or timeout.
// The source must unblock Read when Close is called, as HTTP body readers do.
func ReadBodyBounded(ctx context.Context, source io.ReadCloser) ([]byte, error) {
	if source == nil {
		return []byte{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, BodyTransformTimeout)
	defer cancel()
	done := make(chan struct{})
	var once sync.Once
	closeSource := func() { once.Do(func() { _ = source.Close() }) }
	defer closeSource()
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			closeSource()
		case <-done:
		}
	}()
	b, e := bodyRead(ctx, source)
	if ctx.Err() != nil {
		return nil, &Error{Code: "rewrite_cancelled"}
	}
	return b, e
}

type bodyContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r bodyContextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.reader.Read(p)
}
func bodyRead(ctx context.Context, r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(bodyContextReader{ctx, r}, MaxBodyBytes+1))
	if ctx.Err() != nil {
		return nil, &Error{Code: "rewrite_cancelled"}
	}
	if len(b) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	if e != nil {
		return nil, invalid()
	}
	return b, nil
}

type bodyOutput struct {
	ctx context.Context
	bytes.Buffer
}

func (w *bodyOutput) Write(p []byte) (int, error) {
	if w.ctx.Err() != nil {
		return 0, &Error{Code: "rewrite_cancelled"}
	}
	if w.Len()+len(p) > MaxBodyBytes {
		return 0, &Error{Code: "rewrite_limit"}
	}
	return w.Buffer.Write(p)
}
func bodyEncodings(h http.Header) ([]string, error) {
	var enc []string
	for _, line := range h.Values("Content-Encoding") {
		for _, v := range strings.Split(line, ",") {
			v = strings.ToLower(strings.TrimSpace(v))
			switch v {
			case "identity", "gzip", "deflate", "br", "zstd":
			default:
				return nil, invalid()
			}
			enc = append(enc, v)
		}
	}
	if len(enc) > 3 {
		return nil, invalid()
	}
	return enc, nil
}
func bodyDecode(ctx context.Context, raw []byte, enc []string) ([]byte, error) {
	if len(raw) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	for i := len(enc) - 1; i >= 0; i-- {
		encodedInput := bytes.NewReader(raw)
		var r io.Reader = encodedInput
		var closer io.Closer
		switch enc[i] {
		case "identity":
			continue
		case "gzip":
			g, e := gzip.NewReader(r)
			if e != nil {
				return nil, invalid()
			}
			r = g
			closer = g
		case "deflate":
			z, e := zlib.NewReader(r)
			if e != nil {
				return nil, invalid()
			}
			r = z
			closer = z
		case "br":
			r = brotli.NewReader(r)
		case "zstd":
			z, e := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(BodyWorkspaceBytes), zstd.WithDecoderMaxWindow(MaxBodyBytes))
			if e != nil {
				return nil, invalid()
			}
			defer z.Close()
			r = z
		}
		b, e := bodyRead(ctx, r)
		if e == nil && enc[i] == "deflate" && encodedInput.Len() != 0 {
			e = invalid()
		}
		if closer != nil {
			if ce := closer.Close(); e == nil && ce != nil {
				e = invalid()
			}
		}
		if e != nil {
			return nil, e
		}
		raw = b
	}
	return raw, nil
}
func bodyEncode(ctx context.Context, raw []byte, enc []string) ([]byte, error) {
	if len(raw) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	for _, kind := range enc {
		if kind == "identity" {
			continue
		}
		dst := &bodyOutput{ctx: ctx}
		var w io.WriteCloser
		switch kind {
		case "gzip":
			w = gzip.NewWriter(dst)
		case "deflate":
			z, e := zlib.NewWriterLevel(dst, zlib.DefaultCompression)
			if e != nil {
				return nil, invalid()
			}
			w = z
		case "br":
			w = brotli.NewWriter(dst)
		case "zstd":
			z, e := zstd.NewWriter(dst, zstd.WithEncoderConcurrency(1))
			if e != nil {
				return nil, invalid()
			}
			w = z
		default:
			return nil, invalid()
		}
		_, e := w.Write(raw)
		ce := w.Close()
		if e != nil {
			return nil, e
		}
		if ce != nil {
			return nil, ce
		}
		raw = dst.Bytes()
	}
	return raw, nil
}
func bodyCharset(h http.Header) (encoding.Encoding, error) {
	media, p, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil {
		return nil, invalid()
	}
	textual := strings.HasPrefix(media, "text/") || media == "application/json" || strings.HasSuffix(media, "+json") || media == "application/xml" || strings.HasSuffix(media, "+xml") || media == "application/javascript" || media == "application/x-www-form-urlencoded"
	if !textual {
		return nil, invalid()
	}
	name := strings.ToLower(p["charset"])
	if name == "" || name == "utf-8" || name == "utf8" {
		return nil, nil
	}
	enc, e := ianaindex.MIME.Encoding(name)
	if e != nil || enc == nil {
		return nil, invalid()
	}
	return enc, nil
}
func bodyText(raw []byte, h http.Header) (string, encoding.Encoding, error) {
	enc, e := bodyCharset(h)
	if e != nil {
		return "", nil, e
	}
	if enc == nil {
		if !utf8.Valid(raw) {
			return "", nil, invalid()
		}
		return string(raw), nil, nil
	}
	decoded, _, e := transform.Bytes(enc.NewDecoder(), raw)
	if e != nil || !utf8.Valid(decoded) {
		return "", nil, invalid()
	}
	encoded, _, e := transform.Bytes(enc.NewEncoder(), decoded)
	if e != nil || !bytes.Equal(encoded, raw) {
		return "", nil, invalid()
	}
	return string(decoded), enc, nil
}
func bodyTextEncode(text string, enc encoding.Encoding) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, invalid()
	}
	if len(text) > MaxBodyBytes {
		return nil, &Error{Code: "rewrite_limit"}
	}
	if enc == nil {
		return []byte(text), nil
	}
	raw, _, e := transform.Bytes(enc.NewEncoder(), []byte(text))
	if e != nil {
		return nil, invalid()
	}
	back, _, e := transform.Bytes(enc.NewDecoder(), raw)
	if e != nil || string(back) != text {
		return nil, invalid()
	}
	return raw, nil
}

// BodyOperationHook handles script operations on the decoded representation.
// Callers must use the supplied context and return bounded output.
type BodyOperationHook func(context.Context, []byte, http.Header, Operation) ([]byte, error)

func BodyTransform(ctx context.Context, raw []byte, h http.Header, ops []Operation) ([]byte, error) {
	return BodyTransformWithHook(ctx, raw, h, ops, nil)
}
func BodyTransformWithHook(ctx context.Context, raw []byte, h http.Header, ops []Operation, hook BodyOperationHook) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, BodyTransformTimeout)
	defer cancel()
	enc, e := bodyEncodings(h)
	if e != nil {
		return nil, e
	}
	raw, e = bodyDecode(ctx, raw, enc)
	if e != nil {
		return nil, e
	}
	for _, op := range ops {
		if ctx.Err() != nil {
			return nil, &Error{Code: "rewrite_cancelled"}
		}
		switch op.Op {
		case "body_set":
			value, e := stringValue(op)
			if e != nil {
				return nil, e
			}
			switch op.Encoding {
			case "base64":
				raw, e = base64.StdEncoding.Strict().DecodeString(value)
			case "", "text":
				charset, ce := bodyCharset(h)
				if ce != nil {
					return nil, ce
				}
				raw, e = bodyTextEncode(value, charset)
			default:
				return nil, invalid()
			}
			if e != nil {
				return nil, invalid()
			}
		case "text_replace", "regex_replace":
			text, charset, e := bodyText(raw, h)
			if e != nil {
				return nil, e
			}
			if op.Op == "text_replace" {
				n := 1
				if op.All {
					n = -1
				}
				if op.Pattern == "" {
					return nil, invalid()
				}
				count := strings.Count(text, op.Pattern)
				if n == 1 && count > 1 {
					count = 1
				}
				if len(op.Replacement) > len(op.Pattern) && count > (MaxBodyBytes-len(text))/(len(op.Replacement)-len(op.Pattern)) {
					return nil, &Error{Code: "rewrite_limit"}
				}
				text = strings.Replace(text, op.Pattern, op.Replacement, n)
			} else {
				re, e := regexp.Compile(op.Pattern)
				if e != nil {
					return nil, invalid()
				}
				maxMatches := (16 << 20) / (16 * (re.NumSubexp() + 1))
				matches := re.FindAllStringSubmatchIndex(text, maxMatches+1)
				if len(matches) > maxMatches {
					return nil, &Error{Code: "rewrite_limit"}
				}
				var dst strings.Builder
				last := 0
				for _, m := range matches {
					dst.WriteString(text[last:m[0]])
					expanded, e := bodyExpand(re, op.Replacement, text, m, MaxBodyBytes-dst.Len())
					if e != nil {
						return nil, e
					}
					if dst.Len()+len(expanded) > MaxBodyBytes {
						return nil, &Error{Code: "rewrite_limit"}
					}
					dst.Write(expanded)
					last = m[1]
					if !op.All {
						break
					}
				}
				if dst.Len()+len(text)-last > MaxBodyBytes {
					return nil, &Error{Code: "rewrite_limit"}
				}
				dst.WriteString(text[last:])
				text = dst.String()
			}
			raw, e = bodyTextEncode(text, charset)
			if e != nil {
				return nil, e
			}
		case "json_set", "json_delete":
			media, p, e := mime.ParseMediaType(h.Get("Content-Type"))
			if e != nil || !(media == "application/json" || strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")) || p["charset"] != "" && !strings.EqualFold(p["charset"], "utf-8") {
				return nil, invalid()
			}
			tree, e := decodeJSONBudget(raw, 12<<20)
			if e != nil {
				return nil, e
			}
			parts, e := pointerParts(op.Pointer)
			if e != nil {
				return nil, e
			}
			var value any
			if op.Op == "json_set" {
				value, e = decodeJSON(op.Value)
				if e != nil {
					return nil, e
				}
			}
			tree, e = editJSON(tree, parts, value, op.Op == "json_delete")
			if e != nil {
				return nil, e
			}
			if jsonMemory(tree) > 12<<20 {
				return nil, &Error{Code: "rewrite_limit"}
			}
			raw = canonicalJSON(tree)
		case "script":
			if hook == nil {
				return nil, invalid()
			}
			raw, e = hook(ctx, raw, h, op)
			if e != nil {
				return nil, e
			}
		default:
			return nil, invalid()
		}
		if len(raw) > MaxBodyBytes {
			return nil, &Error{Code: "rewrite_limit"}
		}
	}
	if ctx.Err() != nil {
		return nil, &Error{Code: "rewrite_cancelled"}
	}
	return bodyEncode(ctx, raw, enc)
}

// bodyExpand implements Go/RE2 replacement references without an unbounded intermediate.
func bodyExpand(re *regexp.Regexp, replacement, text string, match []int, limit int) ([]byte, error) {
	out := make([]byte, 0)
	appendText := func(v string) error {
		if len(v) > limit-len(out) {
			return &Error{Code: "rewrite_limit"}
		}
		out = append(out, v...)
		return nil
	}
	for len(replacement) > 0 {
		pos := strings.IndexByte(replacement, '$')
		if pos < 0 {
			if e := appendText(replacement); e != nil {
				return nil, e
			}
			break
		}
		if e := appendText(replacement[:pos]); e != nil {
			return nil, e
		}
		replacement = replacement[pos+1:]
		if strings.HasPrefix(replacement, "$") {
			if e := appendText("$"); e != nil {
				return nil, e
			}
			replacement = replacement[1:]
			continue
		}
		name := ""
		consumed := 0
		braced := strings.HasPrefix(replacement, "{")
		start := 0
		if braced {
			start = 1
		}
		for _, r := range replacement[start:] {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				break
			}
			consumed += utf8.RuneLen(r)
		}
		if consumed > 0 {
			name = replacement[start : start+consumed]
		}
		if name == "" || braced && (start+consumed >= len(replacement) || replacement[start+consumed] != '}') {
			if e := appendText("$"); e != nil {
				return nil, e
			}
			continue
		}
		replacement = replacement[start+consumed:]
		if braced {
			replacement = replacement[1:]
		}
		index := -1
		if n, e := strconv.Atoi(name); e == nil && !(len(name) > 1 && name[0] == '0') {
			index = n
		} else {
			index = re.SubexpIndex(name)
		}
		if index >= 0 && index < len(match)/2 && match[2*index] >= 0 {
			if e := appendText(text[match[2*index]:match[2*index+1]]); e != nil {
				return nil, e
			}
		}
	}
	return out, nil
}
