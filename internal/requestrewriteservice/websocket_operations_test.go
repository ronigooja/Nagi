package requestrewriteservice

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func TestWebSocketRequestOperationsExecuteOrRejectBeforeOrigin(t *testing.T) {
	for _, kind := range []string{"buffer_script", "method", "body"} {
		t.Run(kind, func(t *testing.T) {
			var origins atomic.Int32
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origins.Add(1)
				if kind != "buffer_script" {
					t.Errorf("invalid handshake reached origin")
					return
				}
				if r.Method != "GET" || r.URL.Path != "/changed" || r.Header.Get("X-Edited") != "yes" {
					t.Errorf("request operations ignored: %s %s %v", r.Method, r.URL.Path, r.Header)
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				_, _, _ = conn.Read(r.Context())
			}))
			defer origin.Close()
			op := domain.Operation{Op: "method_set", Value: textValue("POST")}
			if kind == "body" {
				op = domain.Operation{Op: "body_set", Encoding: "base64", Value: textValue("eA==")}
			}
			if kind == "buffer_script" {
				op = domain.Operation{Op: "script", Script: "buffer"}
			}
			rule := v2Rule("request", domain.StageRequest, op)
			rule.OnError = "forward_original"
			doc := domain.Document{Version: 2, Rules: []domain.Rule{rule}, Scripts: []domain.Script{}}
			if kind == "buffer_script" {
				doc.Scripts = []domain.Script{{ID: "buffer", BodyAccess: "buffer", Source: `function transform(input) { if (input.message.body_base64 !== "") return {action:"block"}; input.message.headers["X-Edited"]=["yes"]; return {action:"replace",message:{url:"http://api.service.example/changed",headers:input.message.headers,body_base64:""}}; }`}}
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"), raw)
			tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return f.handoff(t, "http"), nil }}
			defer tr.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, response, err := websocket.Dial(ctx, "ws://api.service.example/original", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
			if kind == "buffer_script" {
				if err != nil {
					t.Fatal(err)
				}
				conn.CloseNow()
				if origins.Load() != 1 {
					t.Fatalf("origin requests: %d", origins.Load())
				}
			} else {
				if conn != nil {
					conn.CloseNow()
				}
				if err == nil || response == nil || response.StatusCode == 101 || origins.Load() != 0 {
					t.Fatalf("invalid handshake was accepted: %v, %+v, requests=%d", err, response, origins.Load())
				}
			}
		})
	}
}
