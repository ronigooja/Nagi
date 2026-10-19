package requestrewriteservice

import (
	"bytes"
	"context"
	"errors"
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

type byteReader struct{ io.Reader }

func (r byteReader) Read(p []byte) (int, error) { return r.Reader.Read(p[:1]) }
func TestStreamingTextUTF8Boundaries(t *testing.T) {
	for _, payload := range []string{"plain", "a€𐀀終", strings.Repeat("€", 12000)} {
		var out bytes.Buffer
		if err := copyTextMessage(&out, byteReader{strings.NewReader(payload)}); err != nil || out.String() != payload {
			t.Fatal(err, out.Len())
		}
	}
	for _, payload := range [][]byte{{255}, {0xe2, 0x82}, {0xed, 0xa0, 0x80}, {0xc0, 0x80}} {
		var out bytes.Buffer
		if err := copyTextMessage(&out, byteReader{bytes.NewReader(payload)}); !errors.Is(err, errTextUTF8) {
			t.Fatal(payload, err)
		}
	}
}

func TestWebSocketInvalidOriginalTextClosesWith1007(t *testing.T) {
	for _, editing := range []bool{false, true} {
		t.Run(map[bool]string{false: "streaming", true: "unmatched-edit-rule"}[editing], func(t *testing.T) {
			forwarded := make(chan bool, 1)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
				defer cancel()
				_, _, err = conn.Read(ctx)
				forwarded <- err == nil
			}))
			defer origin.Close()
			stage := domain.StageWebSocketServer
			if editing {
				stage = domain.StageWebSocketClient
			}
			rule := v2Rule("opposite-or-unmatched", stage, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("changed")})
			rule.Match.MessageTypes = []string{"binary"}
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
			if err = conn.Write(ctx, websocket.MessageText, []byte{255}); err != nil {
				t.Fatal(err)
			}
			_, _, err = conn.Read(ctx)
			if websocket.CloseStatus(err) != websocket.StatusInvalidFramePayloadData {
				t.Fatalf("close %v", err)
			}
			select {
			case got := <-forwarded:
				if got {
					t.Fatal("forwarded invalid complete text")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
