package requestrewriteservice

import (
	"bufio"
	"bytes"
	"context"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// serveHTTP1 puts a bounded raw-header gate before the standard HTTP server.
// Standard Request.Write streams the body into the server without an unbounded
// buffer. The standard server and goproxy retain response and upgrade ownership.
func serveHTTP1(ctx context.Context, conn net.Conn, handler http.Handler, scheme string) {
	input, output := io.Pipe()
	guard := &guardConn{Conn: conn, input: input, output: output, done: make(chan struct{})}
	go func() {
		defer close(guard.done)
		defer output.Close()
		source := bufio.NewReader(conn)
		for ctx.Err() == nil {
			conn.SetReadDeadline(time.Now().Add(time.Minute))
			head, e := readHead(source, 64<<10)
			if e != nil {
				return
			}
			if !validApplicationHead(head) {
				output.CloseWithError(errInvalid)
				return
			}
			parser := bufio.NewReader(io.MultiReader(bytes.NewReader(head), source))
			req, e := http.ReadRequest(parser)
			if e != nil {
				return
			}
			rawHost := ""
			for _, line := range strings.Split(string(head), "\r\n")[1:] {
				if strings.HasPrefix(strings.ToLower(line), "host:") {
					rawHost = strings.TrimSpace(line[5:])
				}
			}
			if req.URL.User != nil || req.URL.Fragment != "" || req.URL.Opaque != "" || strings.HasSuffix(rawHost, ":") || strings.HasSuffix(req.URL.Host, ":") {
				return
			}
			if req.URL.IsAbs() && (req.URL.Scheme != scheme || !sameAuthority(rawHost, req.URL.Host, scheme)) {
				return
			}
			if e = req.Write(output); e != nil {
				return
			}
			if headerToken(req.Header, "Connection", "upgrade") {
				_, _ = io.Copy(output, parser)
				guard.Close()
				return
			}
			// Preserve all prefetched bytes, then discard parser layers instead of
			// accumulating one reader wrapper per keep-alive request.
			pending := make([]byte, 0, parser.Buffered()+source.Buffered())
			if n := parser.Buffered(); n > 0 {
				b, _ := parser.Peek(n)
				pending = append(pending, b...)
			}
			if n := source.Buffered(); n > 0 {
				b, _ := source.Peek(n)
				pending = append(pending, b...)
			}
			source = bufio.NewReader(io.MultiReader(bytes.NewReader(pending), conn))
		}
	}()
	listener := &singleListener{conn: guard, closed: make(chan struct{})}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 64 << 10, ErrorLog: log.New(io.Discard, "", 0), BaseContext: func(net.Listener) context.Context { return ctx }}
	stop := context.AfterFunc(ctx, func() { server.Close(); guard.Close() })
	defer stop()
	_ = server.Serve(listener)
	guard.Close()
	<-guard.done
}
func validApplicationHead(head []byte) bool {
	if !strictCRLF(head) {
		return false
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(head), "\r\n")[1:] {
		if line == "" {
			break
		}
		i := strings.IndexByte(line, ':')
		if i < 1 || line[0] == ' ' || line[0] == '\t' {
			return false
		}
		name := strings.ToLower(line[:i])
		if (name == "host" || name == "content-length" || name == "transfer-encoding") && seen[name] {
			return false
		}
		seen[name] = true
	}
	return seen["host"] && !(seen["content-length"] && seen["transfer-encoding"])
}

type guardConn struct {
	net.Conn
	input   *io.PipeReader
	output  *io.PipeWriter
	done    chan struct{}
	once    sync.Once
	onClose func()
}

func (c *guardConn) Read(p []byte) (int, error) { return c.input.Read(p) }
func (c *guardConn) Close() error {
	c.once.Do(func() {
		c.input.Close()
		c.output.Close()
		c.Conn.Close()
		if c.onClose != nil {
			c.onClose()
		}
	})
	return nil
}

type singleListener struct {
	conn      *guardConn
	once      sync.Once
	closed    chan struct{}
	closeOnce sync.Once
}

func (l *singleListener) Accept() (net.Conn, error) {
	var result net.Conn
	l.once.Do(func() { l.conn.onClose = func() { l.closeOnce.Do(func() { close(l.closed) }) }; result = l.conn })
	if result != nil {
		return result, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *singleListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return l.conn.Close()
}
func (l *singleListener) Addr() net.Addr { return l.conn.LocalAddr() }

func sameAuthority(a, b, scheme string) bool {
	normalize := func(value string) string {
		u := &url.URL{Host: value}
		host, e := domain.NormalizeDomain(u.Hostname())
		if e != nil {
			return ""
		}
		port := u.Port()
		if port == "" {
			port = "80"
			if scheme == "https" {
				port = "443"
			}
		}
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return ""
		}
		return net.JoinHostPort(host, strconv.Itoa(n))
	}
	x := normalize(a)
	return x != "" && x == normalize(b)
}
