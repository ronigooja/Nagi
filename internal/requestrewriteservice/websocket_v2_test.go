package requestrewriteservice

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func TestWebSocketV2MessagesCompressionAndOriginalHandshake(t *testing.T) {
	received := make(chan string, 2)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/changed" {
			t.Errorf("upstream path %q", r.URL.Path)
		}
		w.Header().Set("X-Origin", "original")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"fixture"}, CompressionMode: websocket.CompressionNoContextTakeover})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		kind, body, err := conn.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		received <- string(body)
		if err = conn.Write(ctx, kind, []byte("server")); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = conn.Read(ctx)
	}))
	defer origin.Close()
	request := v2Rule("url", domain.StageRequest, domain.Operation{Op: "url_set", Component: "path", Value: textValue("/changed")})
	response := v2Rule("response", domain.StageResponse, domain.Operation{Op: "header_set", Name: "X-Origin", Value: textValue("response-edited")})
	clientRule := v2Rule("client", domain.StageWebSocketClient, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("client-edited")}, domain.Operation{Op: "header_set", Name: "X-Handshake", Value: textValue("ws-edited")})
	clientRule.Match.PathPrefix = "/original"
	clientRule.Match.ResponseHeaders = map[string][]string{"X-Origin": {"original"}}
	serverRule := v2Rule("server", domain.StageWebSocketServer, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("server-edited")})
	serverRule.Match.PathPrefix = "/original"
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"), v2Document(t, request, response, clientRule, serverRule))
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return f.handoff(t, "http"), nil }}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws://api.service.example/original", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}, Subprotocols: []string{"fixture"}, CompressionMode: websocket.CompressionNoContextTakeover, CompressionThreshold: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != "fixture" || resp.Header.Get("X-Handshake") != "ws-edited" || resp.Header.Get("X-Origin") != "response-edited" {
		t.Fatalf("handshake result %v", resp.Header)
	}
	if extensions := resp.Header.Get("Sec-WebSocket-Extensions"); !strings.Contains(extensions, "client_no_context_takeover") || !strings.Contains(extensions, "server_no_context_takeover") {
		t.Fatalf("compression policy %q", extensions)
	}
	if err = conn.Write(ctx, websocket.MessageText, []byte("client")); err != nil {
		t.Fatal(err)
	}
	kind, body, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageText || string(body) != "server-edited" {
		t.Fatalf("server message %v %q %v", kind, body, err)
	}
	select {
	case got := <-received:
		if got != "client-edited" {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() { _, _, _ = conn.Read(ctx) }()
	if err = conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestWebSocketV2UneditedDirectionStreamsLargeMessages(t *testing.T) {
	payload := strings.Repeat("x", domain.MaxBodyBytes+1024)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err = conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
			t.Error(err)
		}
		_, _, _ = conn.Read(ctx)
	}))
	defer origin.Close()
	// A client-only message rule keeps the reverse direction streaming.
	rule := v2Rule("client", domain.StageWebSocketClient, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("edited")})
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"), v2Document(t, rule))
	tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return f.handoff(t, "http"), nil }}
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://api.service.example/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(-1)
	kind, reader, err := conn.Reader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	size, err := io.Copy(io.Discard, reader)
	if err != nil || kind != websocket.MessageText || size != int64(len(payload)) {
		t.Fatalf("stream %d %v", size, err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestWebSocketV2EditedOversizeClosesBeforeForward(t *testing.T) {
	forwarded := make(chan int, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, body, err := conn.Read(ctx)
		if err == nil {
			forwarded <- len(body)
		}
	}))
	defer origin.Close()
	rule := v2Rule("client", domain.StageWebSocketClient, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("edited")})
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"), v2Document(t, rule))
	tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return f.handoff(t, "http"), nil }}
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://api.service.example/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_ = conn.Write(ctx, websocket.MessageBinary, make([]byte, domain.MaxBodyBytes+1))
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("oversize message accepted")
	}
	select {
	case n := <-forwarded:
		t.Fatalf("forwarded partial message %d", n)
	default:
	}
}
