package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/proxy"
)

func TestConnectionsCLIResponseLimit(t *testing.T) {
	for _, size := range []int{(1 << 20) + 1, control.MaxResponseBytes, control.MaxResponseBytes + 1} {
		t.Run(fmt.Sprintf("bytes_%d", size), func(t *testing.T) {
			root, err := os.MkdirTemp("", "ncl-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			paths := isolatedTestPaths(t, root)
			if err := os.MkdirAll(paths.RuntimeDir, 0700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("unix", paths.SocketPath)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			prefix := `{"connections":[{"id":"one","metadata":{"processPath":"`
			suffix := `"},"upload":2,"download":3}]}`
			payloadSize := size - len(prefix) - len(suffix)
			body := prefix + strings.Repeat("a", payloadSize) + suffix
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/connections" || r.Method != http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = w.Write([]byte(body))
			})}
			go server.Serve(listener)
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := Run([]string{"--json", "connections", "list"}, &stdout, &stderr, "test", "commit")
			if size > control.MaxResponseBytes {
				var result struct {
					OK    bool `json:"ok"`
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if code != 1 || stdout.Len() != 0 || json.Unmarshal(stderr.Bytes(), &result) != nil || result.OK || result.Error.Code != "response_too_large" {
					t.Fatalf("overflow code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
				}
				return
			}
			var result struct {
				OK   bool `json:"ok"`
				Data struct {
					Connections []proxy.Connection `json:"connections"`
				} `json:"data"`
			}
			if code != 0 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &result) != nil || !result.OK || len(result.Data.Connections) != 1 {
				t.Fatalf("list code=%d stdout bytes=%d stderr=%s", code, stdout.Len(), stderr.String())
			}
			c := result.Data.Connections[0]
			if c.ID != "one" || c.Upload != 2 || c.Download != 3 || len(c.Metadata["processPath"].(string)) != payloadSize {
				t.Fatal("connection data was truncated or changed")
			}
		})
	}
}
