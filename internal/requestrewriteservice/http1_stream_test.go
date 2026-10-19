package requestrewriteservice

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Compress only the raw gate's one-minute header-idle clock. The handler still
// streams for several such intervals, without a minute-long default unit test.
type shortHeaderIdleConn struct{ net.Conn }

func (c shortHeaderIdleConn) SetReadDeadline(deadline time.Time) error {
	if !deadline.IsZero() && time.Until(deadline) > 30*time.Second {
		deadline = time.Now().Add(100 * time.Millisecond)
	}
	return c.Conn.SetReadDeadline(deadline)
}

func TestHTTP1LongResponseThenIdleAndPipeline(t *testing.T) {
	for _, pipeline := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "pipeline"}[pipeline], func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				serveHTTP1(ctx, shortHeaderIdleConn{server}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/long" {
						for i := 0; i < 6; i++ {
							_, _ = io.WriteString(w, "chunk")
							w.(http.Flusher).Flush()
							select {
							case <-r.Context().Done():
								return
							case <-time.After(50 * time.Millisecond):
							}
						}
					} else {
						_, _ = io.WriteString(w, "next")
					}
				}), "http")
			}()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			writeDone := make(chan error, 1)
			go func() {
				requests := "GET /long HTTP/1.1\r\nHost: fixture.example\r\n\r\n"
				if pipeline {
					requests += "GET /next HTTP/1.1\r\nHost: fixture.example\r\n\r\n"
				}
				_, e := io.WriteString(client, requests)
				writeDone <- e
			}()
			reader := bufio.NewReader(client)
			resp, e := http.ReadResponse(reader, nil)
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(resp.Body)
			resp.Body.Close()
			if e != nil || string(body) != strings.Repeat("chunk", 6) {
				t.Fatalf("active response interrupted: bytes=%d error=%v", len(body), e)
			}
			if pipeline {
				resp, e = http.ReadResponse(reader, nil)
				if e != nil {
					t.Fatal(e)
				}
				body, e = io.ReadAll(resp.Body)
				resp.Body.Close()
				if e != nil || string(body) != "next" {
					t.Fatalf("pipeline response %q %v", body, e)
				}
			}
			if e := <-writeDone; e != nil {
				t.Fatal(e)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("truly idle client was not closed")
			}
		})
	}
}

func TestHTTP1CancelActiveResponse(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveHTTP1(ctx, server, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }), "http")
	}()
	go io.WriteString(client, "GET / HTTP/1.1\r\nHost: fixture.example\r\n\r\n")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not dispatched")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation leaked gate or handler")
	}
}

func TestHTTP1HalfClosePreservesResponse(t *testing.T) {
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	client, e := net.DialTCP("tcp4", nil, ln.Addr().(*net.TCPAddr))
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	server, e := ln.Accept()
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveHTTP1(context.Background(), server, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Millisecond):
			}
			_, _ = io.WriteString(w, "after-half-close")
		}), "http")
	}()
	if _, e = io.WriteString(client, "GET / HTTP/1.1\r\nHost: fixture.example\r\n\r\n"); e != nil {
		t.Fatal(e)
	}
	if e = client.CloseWrite(); e != nil {
		t.Fatal(e)
	}
	resp, e := http.ReadResponse(bufio.NewReader(client), nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(b) != "after-half-close" {
		t.Fatalf("half-close lost response: %q %v", b, e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("half-close leaked connection")
	}
}

func TestHTTP1IncompleteHeaderStillTimesOut(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveHTTP1(context.Background(), shortHeaderIdleConn{server}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("incomplete header dispatched") }), "http")
	}()
	_, _ = io.WriteString(client, "GET / HTTP/1.1\r\nHost: ")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("incomplete header has no timeout")
	}
}

func TestHTTP1ServerRejectionReleasesGate(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveHTTP1(context.Background(), server, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported expectation dispatched") }), "http")
	}()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	go io.WriteString(client, "GET / HTTP/1.1\r\nHost: fixture.example\r\nExpect: unsupported\r\n\r\n")
	_, _ = io.Copy(io.Discard, client)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server rejection leaked response gate")
	}
}

func TestHTTP1HeaderDeadlineDoesNotBoundUploadOrUpgrade(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "upload", true: "upgrade"}[upgrade], func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				serveHTTP1(ctx, shortHeaderIdleConn{server}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if upgrade {
						c, rw, e := w.(http.Hijacker).Hijack()
						if e != nil {
							return
						}
						defer c.Close()
						_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: upgrade\r\nUpgrade: websocket\r\n\r\n")
						_ = rw.Flush()
						_, _ = io.Copy(c, rw)
						return
					}
					b, e := io.ReadAll(r.Body)
					if e != nil {
						t.Error(e)
						return
					}
					_, _ = w.Write(b)
				}), "http")
			}()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			request := "POST / HTTP/1.1\r\nHost: fixture.example\r\nContent-Length: 6\r\n\r\n"
			if upgrade {
				request = "GET / HTTP/1.1\r\nHost: fixture.example\r\nConnection: upgrade\r\nUpgrade: websocket\r\n\r\n"
			}
			if _, e := io.WriteString(client, request); e != nil {
				t.Fatal(e)
			}
			reader := bufio.NewReader(client)
			if upgrade {
				resp, e := http.ReadResponse(reader, nil)
				if e != nil || resp.StatusCode != 101 {
					t.Fatalf("upgrade rejected %v", e)
				}
			}
			sent := make(chan error, 1)
			go func() {
				for i := 0; i < 6; i++ {
					time.Sleep(50 * time.Millisecond)
					if _, e := io.WriteString(client, "x"); e != nil {
						sent <- e
						return
					}
				}
				sent <- nil
			}()
			var got []byte
			var e error
			if upgrade {
				got = make([]byte, 6)
				_, e = io.ReadFull(reader, got)
			} else {
				var resp *http.Response
				resp, e = http.ReadResponse(reader, nil)
				if e == nil {
					got, e = io.ReadAll(resp.Body)
					resp.Body.Close()
				}
			}
			if e != nil || string(got) != "xxxxxx" {
				t.Fatalf("active stream hit header deadline: %q %v", got, e)
			}
			if e = <-sent; e != nil {
				t.Fatal(e)
			}
			cancel()
			client.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stream gate leaked")
			}
		})
	}
}
