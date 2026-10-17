package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type historyClient struct{ calls []string }

func (c *historyClient) Get(_ context.Context, path string, dst any) error {
	c.calls = append(c.calls, path)
	var data string
	switch path {
	case "/proxies":
		data = `{"proxies":{"Auto":{"type":"URLTest","all":["Static","Remote","Duplicate","Fresh"],"testUrl":"https://user:secret@example.com/private?token=secret#x"},"Static":{"history":[{"time":"2026-01-02T00:00:00Z","delay":99}],"extra":{"https://user:secret@example.com/private?token=secret#x":{"history":[{"time":"2026-01-01T00:00:00Z","delay":42},{"time":"2026-01-03T00:00:00Z","delay":0}]}}}}}`
	case "/providers/proxies":
		data = `{"providers":{"one":{"type":"Proxy","vehicleType":"HTTP","testUrl":"https://example.com/check?secret=key","proxies":[{"name":"Remote","extra":{"https://user:secret@example.com/private?token=secret#x":{"history":[{"time":"2026-01-03T00:00:00Z","delay":12}]}}},{"name":"Duplicate","history":[{"delay":1}]}]},"two":{"vehicleType":"HTTP","proxies":[{"name":"Duplicate","history":[{"delay":2}]}]}}}`
	}
	return json.Unmarshal([]byte(data), dst)
}
func (*historyClient) Put(context.Context, string, any, any) error { return nil }
func TestGroupNativeHistoryLatestFailureAndProviderIdentity(t *testing.T) {
	c := &historyClient{}
	groups, err := NewService(c).Groups(context.Background())
	if err != nil || len(groups) != 1 {
		t.Fatalf("%+v %v", groups, err)
	}
	d := groups[0].NodeDelays
	if d["Static"].Status != "failed" || d["Static"].DelayMS != nil || d["Static"].MeasuredAt != "2026-01-03T00:00:00Z" {
		t.Fatalf("old success presented: %+v", d)
	}
	if d["Remote"].Status != "ok" || *d["Remote"].DelayMS != 12 || d["Duplicate"].Status != "untested" || d["Fresh"].Status != "untested" {
		t.Fatalf("identity: %+v", d)
	}
	b, _ := json.Marshal(groups)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "/private") || d["Static"].TestURL != "https://example.com" {
		t.Fatal(string(b))
	}
	for _, p := range c.calls {
		if strings.Contains(p, "healthcheck") || strings.Contains(p, "delay") {
			t.Fatal("read triggered probe", p)
		}
	}
}
func TestHistoryReportsActualLatestURL(t *testing.T) {
	var p proxyHistory
	json.Unmarshal([]byte(`{"history":[{"delay":100}],"extra":{"https://other.example":{"history":[{"delay":100}]}}}`), &p)
	if d := latestDelay(p, "https://example.com"); d.Status != "ok" || d.TestURL != "https://other.example" {
		t.Fatal(d)
	}
}

func TestNewestURLFailureSupersedesConfiguredSuccess(t *testing.T) {
	var p proxyHistory
	json.Unmarshal([]byte(`{"extra":{"https://configured.example/test":{"history":[{"time":"2026-01-01T00:00:00Z","delay":12}]},"https://manual.example/private?token=secret":{"history":[{"time":"2026-01-02T00:00:00Z","delay":0}]}}}`), &p)
	d := latestDelay(p, "https://configured.example/test")
	if d.Status != "failed" || d.DelayMS != nil || d.MeasuredAt != "2026-01-02T00:00:00Z" || d.TestURL != "https://manual.example" {
		t.Fatal(d)
	}
}
func TestURLTimestampTiePrefersContext(t *testing.T) {
	var p proxyHistory
	json.Unmarshal([]byte(`{"extra":{"https://a.example":{"history":[{"time":"2026-01-01T00:00:00Z","delay":12}]},"https://z.example":{"history":[{"time":"2026-01-01T00:00:00Z","delay":0}]}}}`), &p)
	for i := 0; i < 10; i++ {
		d := latestDelay(p, "https://z.example")
		if d.Status != "failed" || d.TestURL != "https://z.example" {
			t.Fatal(d)
		}
	}
}
func TestProviderHistorySnapshotReadOnceAcrossGroups(t *testing.T) {
	c := &historyClient{}
	s := NewService(c)
	r := groupHistoryReader{top: map[string]proxyHistory{}}
	for i := 0; i < 3; i++ {
		g := Group{All: []string{"Remote", "Duplicate"}}
		r.decorate(context.Background(), s, &g, "")
	}
	if len(c.calls) != 1 || c.calls[0] != "/providers/proxies" {
		t.Fatal(c.calls)
	}
}
