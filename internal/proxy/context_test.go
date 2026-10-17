package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type contextClient struct {
	paths    []string
	measured bool
	fail     bool
	alive    bool
	zero     bool
}

func (c *contextClient) Put(context.Context, string, any, any) error { return nil }
func (c *contextClient) Get(_ context.Context, path string, dst any) error {
	c.paths = append(c.paths, path)
	history := `"history":[{"time":"2026-01-01T00:00:00Z","delay":4}]`
	if c.measured {
		delay := 11
		if c.zero || !c.alive {
			delay = 0
		}
		history = fmt.Sprintf(`"extra":{"https://checks.example/test":{"alive":%t,"history":[{"time":%q,"delay":%d}]}}`, c.alive, time.Now().UTC().Format(time.RFC3339Nano), delay)
	}
	var data string
	switch {
	case strings.Contains(path, "/healthcheck?") || strings.Contains(path, "/delay?"):
		c.measured = true
		if c.fail {
			return fmt.Errorf("native handler failure")
		}
		data = `{"delay":11}`
	case path == "/proxies":
		data = `{"proxies":{"Outer":{"type":"Selector","all":["Nested","LB","DIRECT"],"now":"Nested"},"Nested":{"type":"Selector","all":["Twin"],"now":"Twin"},"LB":{"type":"LoadBalance","all":["Twin"],"now":"Twin","history":[{"delay":1}]},"G":{"type":"Selector","all":["Twin"]},"Twin":{"name":"Twin","type":"Direct","history":[{"delay":99}]},"DIRECT":{"type":"Direct"}}}`
	case path == "/providers/proxies":
		data = `{"providers":{"A":{"vehicleType":"HTTP","proxies":[{"name":"Twin","type":"Reject",` + history + `}]},"B":{"vehicleType":"HTTP","proxies":[{"name":"Twin","history":[{"delay":88}]}]}}}`
	case path == "/providers/proxies/A":
		data = `{"proxies":[{"name":"Twin",` + history + `}]}`
	default:
		return fmt.Errorf("unexpected path %s", path)
	}
	return json.Unmarshal([]byte(data), dst)
}
func configuredContext(t *testing.T, c *contextClient) *Service {
	t.Helper()
	s := NewService(c)
	if e := s.Configure([]byte(`proxy-groups:
  - name: G
    use: [A]
    url: https://checks.example/test
    timeout: 8000
    expected-status: 204
  - name: Nested
    use: [A]
  - name: Outer
    proxies: [Nested, LB, DIRECT]
  - name: LB
    use: [A]
proxy-providers:
  A:
    health-check:
      url: https://checks.example/test
      timeout: 8000
      expected-status: 204
`)); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestContextIdentityAndNestedKinds(t *testing.T) {
	c := &contextClient{}
	s := configuredContext(t, c)
	gs, e := s.Groups(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range gs {
		switch g.Name {
		case "G":
			if *g.NodeDelays["Twin"].DelayMS != 4 || g.NodeInfo["Twin"].Kind != "node" {
				t.Fatalf("wrong source: %+v", g)
			}
		case "Outer":
			if g.NodeInfo["Nested"].Kind != "group" || g.NodeInfo["Nested"].Status != "available" || g.NodeInfo["LB"].Status != "" || g.NodeInfo["DIRECT"].Kind != "builtin" {
				t.Fatalf("classification: %+v", g)
			}
			if _, ok := g.NodeDelays["Nested"]; ok {
				t.Fatal("group inherited latency")
			}
		}
	}
	for _, p := range c.paths {
		if strings.Contains(p, "healthcheck") || strings.Contains(p, "delay?") {
			t.Fatal("read measured")
		}
	}
}
func TestContextManualConditionsZeroAndExpectedFailure(t *testing.T) {
	for _, kind := range []string{"group", "provider"} {
		for _, tc := range []struct{ alive, zero, fail bool }{{true, false, false}, {true, true, true}, {false, true, false}, {false, true, true}} {
			c := &contextClient{alive: tc.alive, zero: tc.zero, fail: tc.fail}
			s := configuredContext(t, c)
			owner := "G"
			if kind == "provider" {
				owner = "A"
			}
			got, e := s.TestNode(context.Background(), kind, owner, "Twin")
			if (e == nil) != tc.alive {
				t.Fatalf("%s %+v result=%+v err=%v", kind, tc, got, e)
			}
			if e == nil && tc.zero && got.DelayMS != 0 {
				t.Fatal(got)
			}
			found := false
			for _, p := range c.paths {
				if strings.Contains(p, "healthcheck?") {
					found = true
					if !strings.HasPrefix(p, "/providers/proxies/A/Twin/healthcheck?") || !strings.Contains(p, "expected=204") || !strings.Contains(p, "timeout=8000") {
						t.Fatal(p)
					}
				}
			}
			if !found {
				t.Fatal(c.paths)
			}
		}
	}
}
func TestContextAmbiguityStopsDispatch(t *testing.T) {
	c := &contextClient{}
	s := configuredContext(t, c)
	s.Configure([]byte("proxy-groups:\n- name: G\n  use: [A,B]\n"))
	if _, e := s.TestNode(context.Background(), "group", "G", "Twin"); e == nil {
		t.Fatal("accepted ambiguity")
	}
	for _, p := range c.paths {
		if strings.Contains(p, "healthcheck?") {
			t.Fatal("measured ambiguous source")
		}
	}
}
func TestZeroHistoryAliveAndLatestFailure(t *testing.T) {
	for _, alive := range []bool{true, false} {
		var p proxyHistory
		json.Unmarshal([]byte(fmt.Sprintf(`{"extra":{"https://a":{"alive":%t,"history":[{"time":"2026-01-01T00:00:00Z","delay":0}]}}}`, alive)), &p)
		d := latestDelay(p, "")
		if (d.Status == "ok") != alive {
			t.Fatal(d)
		}
		if alive && (d.DelayMS == nil || *d.DelayMS != 0) {
			t.Fatal(d)
		}
	}
}

func TestGroupConditionsInheritProviderURLAndWeakTimeout(t *testing.T) {
	s := NewService(&contextClient{})
	if e := s.Configure([]byte(`proxy-groups:
- name: Ordered
  use: [B,A]
  timeout: "7000"
- name: Included
  include-all-providers: true
proxy-providers:
  A:
    health-check:
      url: https://a.example/check
  B:
    health-check:
      url: https://b.example/check
`)); e != nil {
		t.Fatal(e)
	}
	u, timeout, e := s.GroupTestConditions("Ordered")
	if e != nil || u != "https://b.example/check" || timeout != 7000 {
		t.Fatalf("%s %d %v", u, timeout, e)
	}
	u, _, e = s.GroupTestConditions("Included")
	if e != nil || u != "https://a.example/check" {
		t.Fatalf("%s %v", u, e)
	}
}
func TestNestedCycleAndAmbiguityHaveNoStatus(t *testing.T) {
	c := &contextClient{}
	s := configuredContext(t, c)
	r, e := s.historyReader(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	r.top["Nested"] = proxyHistory{Group: Group{Type: "Selector", All: []string{"Outer"}, Now: "Outer"}}
	s.config.Groups = append(s.config.Groups, groupConfig{Name: "Nested", Proxies: []string{"Outer"}})
	// Replace the existing definition to form a true selection cycle.
	for i := range s.config.Groups {
		if s.config.Groups[i].Name == "Nested" {
			s.config.Groups[i].Use = nil
			s.config.Groups[i].Proxies = []string{"Outer"}
		}
	}
	if status := r.leafStatus(context.Background(), s, "Outer", map[string]bool{}); status != "" {
		t.Fatal(status)
	}
}

type compatibleProviderClient struct{ contextClient }

func (c *compatibleProviderClient) Get(ctx context.Context, path string, dst any) error {
	if path == "/providers/proxies" {
		return json.Unmarshal([]byte(`{"providers":{"Real":{"vehicleType":"HTTP","proxies":[{"name":"Twin","history":[{"delay":7}]}]},"Generated":{"vehicleType":"Compatible","proxies":[{"name":"Twin","history":[{"delay":99}]}]}}}`), dst)
	}
	return c.contextClient.Get(ctx, path, dst)
}
func TestIncludeAllProvidersIgnoresGeneratedCompatibleProviders(t *testing.T) {
	c := &compatibleProviderClient{}
	s := NewService(c)
	if e := s.Configure([]byte("proxy-groups:\n- name: G\n  include-all-providers: true\nproxy-providers:\n  Real: {}\n")); e != nil {
		t.Fatal(e)
	}
	reader, e := s.historyReader(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	p, provider, ok := reader.resolve(context.Background(), s, "G", "Twin")
	if !ok || provider != "Real" || *latestDelay(p, "").DelayMS != 7 {
		t.Fatalf("ownership %s %v %+v", provider, ok, p)
	}
}
func TestUnavailableEffectiveConfigurationRejectsImplicitConditions(t *testing.T) {
	s := NewService(&contextClient{})
	s.RequireEffectiveConfig()
	if _, _, e := s.GroupTestConditions("G"); e == nil {
		t.Fatal("missing profile silently defaulted")
	}
	if e := s.Configure([]byte("proxy-groups: [bad]")); e == nil {
		t.Fatal("invalid config accepted")
	}
	if _, _, e := s.GroupTestConditions("G"); e == nil {
		t.Fatal("invalid profile silently defaulted")
	}
}
