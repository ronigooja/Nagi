package control

import "testing"

func TestRewriteCapabilityVersionFence(t *testing.T) {
	c := RewriteCapabilities{Versions: []int{2}, TUNOnly: true, OriginalContext: true, Reinjection: true, QUIC: true, MaxContexts: 1024, MaxStreams: 64, ContextIdle: 60, ContextMax: 3600}
	if !c.Supported() {
		t.Fatal("v2 capability contract rejected")
	}
	c.TUNOnly = false
	c.TUNRequired = true
	c.ProxyListenerCapture = 1
	if !c.Supported() {
		t.Fatal("listener capability contract rejected")
	}
	c.ProxyListenerCapture = 0
	if c.Supported() {
		t.Fatal("unadvertised listener capability accepted")
	}
	c.ProxyListenerCapture = 1
	c.TUNRequired = false
	if c.Supported() {
		t.Fatal("unsupported listener lifecycle accepted")
	}
	c.TUNRequired = true
	c.Versions = []int{1}
	if c.Supported() {
		t.Fatal("legacy engine accepted")
	}
	c.Versions = []int{2}
	c.OriginalContext = false
	if c.Supported() {
		t.Fatal("missing original context support accepted")
	}
}
