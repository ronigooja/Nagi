package command

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/proxy"
)

type tailscalePathCLIClient struct {
	getPath, postPath string
	body              any
}

func (c *tailscalePathCLIClient) Get(_ context.Context, path string, out any) error {
	c.getPath = path
	*(out.(*proxy.TailscalePathStatus)) = proxy.TailscalePathStatus{Available: true, State: "ok"}
	return nil
}
func (*tailscalePathCLIClient) Put(context.Context, string, any, any) error { return nil }
func (c *tailscalePathCLIClient) Post(_ context.Context, path string, body any, out any) error {
	c.postPath = path
	c.body = body
	*(out.(*proxy.TailscalePathProbe)) = proxy.TailscalePathProbe{PeerIP: "100.64.0.2", NodeName: "peer", Transport: "direct", LatencyMS: 4}
	return nil
}

func TestProxyTailscaleCLICommands(t *testing.T) {
	c := &tailscalePathCLIClient{}
	service := proxy.NewService(c)
	data, err := proxyCommand(context.Background(), []string{"proxy", "tailscale", "status", "work"}, service, proxy.SelectionStore{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data.(proxy.TailscalePathStatus); !ok || c.getPath != "/proxies/work/tailscale/status" {
		t.Fatalf("data=%#v path=%q", data, c.getPath)
	}
	data, err = proxyCommand(context.Background(), []string{"proxy", "tailscale", "probe", "work", "100.64.0.2"}, service, proxy.SelectionStore{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data.(proxy.TailscalePathProbe); !ok || c.postPath != "/proxies/work/tailscale/ping" {
		t.Fatalf("data=%#v path=%q", data, c.postPath)
	}
	if _, err = proxyCommand(context.Background(), []string{"proxy", "tailscale", "probe", "work", "peer.example"}, service, proxy.SelectionStore{}, t.TempDir()); err == nil {
		t.Fatal("accepted host name instead of IP literal")
	}
}

func TestTailscaleProbeUsageValidationPrecedesRuntimeResolution(t *testing.T) {
	for _, args := range [][]string{{"proxy", "tailscale", "probe", "Tail", "peer.example"}, {"proxy", "tailscale", "probe", "Tail", "100.64.0.2", "999"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "dev", "test"); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage: nagi proxy tailscale probe") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
