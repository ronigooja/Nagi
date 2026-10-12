package command

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
)

func TestDefaultDNSRejectsLiveDirectChain(t *testing.T) {
	for _, tc := range []struct {
		name, leaf string
		refused    bool
	}{
		{"proxy", `{"type":"Shadowsocks"}`, false},
		{"direct", `{"type":"Direct"}`, true},
		{"reject", `{"type":"Reject"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "api.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/proxies" {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"proxies":{"DNS":{"type":"Selector","now":"Nested","all":["Nested"]},"Nested":{"type":"URLTest","now":"Leaf","all":["Leaf"]},"Leaf":` + tc.leaf + `}}`))
			})}
			go server.Serve(listener)
			t.Cleanup(func() { _ = server.Close() })
			store := profile.NewStore(t.TempDir(), nil, nil)
			args := []string{"profile", "override", "apply-default", "work", "--dns-proxy", "DNS"}
			err = validateDefaultDNSLiveTarget(context.Background(), args, store, "work", control.New(socket))
			if (err != nil) != tc.refused {
				t.Fatalf("error=%v refused=%v", err, tc.refused)
			}
			if err != nil && !strings.Contains(err.Error(), "select a proxy") {
				t.Fatalf("unactionable error: %v", err)
			}
			if err := validateDefaultDNSLiveTarget(context.Background(), args, store, "other", control.New(socket)); err != nil {
				t.Fatalf("live selection from another profile used: %v", err)
			}
		})
	}
}
