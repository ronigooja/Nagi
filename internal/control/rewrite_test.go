package control

import "testing"

func TestRewriteCapabilityVersionFence(t *testing.T) {
	c := RewriteCapabilities{Versions: []int{2}, TUNOnly: true, OriginalContext: true, Reinjection: true, QUIC: true, MaxContexts: 1024, MaxStreams: 64, ContextIdle: 60, ContextMax: 3600}
	if !c.Supported() {
		t.Fatal("v2 capability contract rejected")
	}
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
