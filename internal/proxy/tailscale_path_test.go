package proxy

import (
	"context"
	"net/http"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
)

type pathClient struct {
	getPath, postPath string
	body              any
	getErr, postErr   error
	status            TailscalePathStatus
	probe             TailscalePathProbe
}

func (*pathClient) Put(context.Context, string, any, any) error { return nil }

func (c *pathClient) Get(_ context.Context, path string, out any) error {
	c.getPath = path
	if c.getErr != nil {
		return c.getErr
	}
	*(out.(*TailscalePathStatus)) = c.status
	return nil
}
func (c *pathClient) Post(_ context.Context, path string, body any, out any) error {
	c.postPath = path
	c.body = body
	if c.postErr != nil {
		return c.postErr
	}
	*(out.(*TailscalePathProbe)) = c.probe
	return nil
}

func TestTailscalePathStatusAndProbeRequests(t *testing.T) {
	c := &pathClient{status: TailscalePathStatus{Available: true, State: "ok", Peers: []TailscalePathPeer{{Hostname: "build"}}}, probe: TailscalePathProbe{PeerIP: "100.64.0.2", NodeName: "build", Transport: "direct", LatencyMS: 8}}
	s := NewService(c)
	status, err := s.TailscalePathStatus(context.Background(), "tail node")
	if err != nil || !status.Available || c.getPath != "/proxies/tail%20node/tailscale/status" {
		t.Fatalf("status=%+v path=%q err=%v", status, c.getPath, err)
	}
	probe, err := s.TailscalePathProbe(context.Background(), "tail node", "100.64.0.2", 0)
	if err != nil || probe.LatencyMS != 8 || c.postPath != "/proxies/tail%20node/tailscale/ping" {
		t.Fatalf("probe=%+v path=%q err=%v", probe, c.postPath, err)
	}
	body := c.body.(map[string]any)
	if len(body) != 2 || body["peer_ip"] != "100.64.0.2" || body["timeout_ms"] != 5000 {
		t.Fatalf("body=%#v", body)
	}
}

func TestTailscalePathProbeRejectsInvalidInputAndUnsupportedEngine(t *testing.T) {
	c := &pathClient{}
	s := NewService(c)
	for _, tc := range []struct {
		ip      string
		timeout int
	}{{"host.example", 5000}, {"100.64.0.1", 999}, {"100.64.0.1", 15001}} {
		if _, err := s.TailscalePathProbe(context.Background(), "tail", tc.ip, tc.timeout); err == nil {
			t.Fatalf("accepted %#v", tc)
		}
	}
	if c.postPath != "" {
		t.Fatalf("invalid request reached API: %s", c.postPath)
	}
	c.getErr = &control.APIError{Status: http.StatusNotFound, Message: "no such route"}
	if _, err := s.TailscalePathStatus(context.Background(), "tail"); err == nil {
		t.Fatal("expected unsupported engine error")
	}
}
