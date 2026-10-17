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
	paths      []string
	measured   bool
	fail       bool
	alive      bool
	zero       bool
	measuredAt time.Time
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
		measuredAt := c.measuredAt
		if measuredAt.IsZero() {
			measuredAt = time.Now()
		}
		history = fmt.Sprintf(`"extra":{"https://checks.example/test":{"alive":%t,"history":[{"time":%q,"delay":%d}]}}`, c.alive, measuredAt.UTC().Format(time.RFC3339Nano), delay)
	}
	var data string
	switch {
	case strings.Contains(path, "/healthcheck?") || strings.Contains(path, "/delay?"):
		c.measured = true
		if c.fail || !c.alive {
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
			if d := g.NodeInfo["Nested"].SelectedDelay; d == nil || d.Status != "ok" || d.DelayMS == nil || *d.DelayMS != 4 {
				t.Fatalf("nested selected delay: %+v", d)
			}
			if g.NodeInfo["LB"].SelectedDelay != nil || g.NodeInfo["DIRECT"].SelectedDelay != nil {
				t.Fatalf("groups without selected leaves gained delays: %+v", g.NodeInfo)
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
func TestNestedSelectedLeafMeasurementStates(t *testing.T) {
	c := &contextClient{}
	s := configuredContext(t, c)
	r, e := s.historyReader(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name    string
		history []historyRecord
		want    string
	}{
		{name: "untested", want: "untested"},
		{name: "failed", history: []historyRecord{{Time: time.Now(), Delay: 0}}, want: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.providers = map[string][]proxyHistory{"A": {{Name: "Twin", History: tc.history}}}
			r.loaded = true
			d, builtin := r.selectedLeaf(context.Background(), s, "Outer", map[string]bool{})
			if d == nil || d.Status != tc.want || d.DelayMS != nil || builtin != "" {
				t.Fatalf("selected leaf: delay=%+v builtin=%q", d, builtin)
			}
		})
	}
}
func TestNestedSelectedBuiltin(t *testing.T) {
	c := &contextClient{}
	s := configuredContext(t, c)
	r, e := s.historyReader(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"DIRECT", "REJECT"} {
		t.Run(name, func(t *testing.T) {
			r.top[name] = proxyHistory{Group: Group{Type: map[string]string{"DIRECT": "Direct", "REJECT": "Reject"}[name]}}
			r.top["Nested"] = proxyHistory{Group: Group{Type: "Selector", All: []string{name}, Now: name}}
			for i := range s.config.Groups {
				if s.config.Groups[i].Name == "Nested" {
					s.config.Groups[i].Use = nil
					s.config.Groups[i].Proxies = []string{name}
				}
			}
			var outer Group
			outer.Name = "Outer"
			outer.All = []string{"Nested"}
			r.decorate(context.Background(), s, &outer, "")
			info := outer.NodeInfo["Nested"]
			if info.SelectedBuiltin != name || info.SelectedDelay != nil || info.Status != "" {
				t.Fatalf("nested builtin: %+v", info)
			}
		})
	}
}
func TestNestedProviderNodeNamedDirectIsMeasured(t *testing.T) {
	c := &contextClient{}
	s := configuredContext(t, c)
	r, e := s.historyReader(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	r.top["Nested"] = proxyHistory{Group: Group{Type: "Selector", All: []string{"DIRECT"}, Now: "DIRECT"}}
	r.providers = map[string][]proxyHistory{"A": {{Name: "DIRECT", History: []historyRecord{{Time: time.Now(), Delay: 12}}}}}
	r.loaded = true
	d, builtin := r.selectedLeaf(context.Background(), s, "Nested", map[string]bool{})
	if builtin != "" || d == nil || d.Status != "ok" || d.DelayMS == nil || *d.DelayMS != 12 {
		t.Fatalf("provider node misclassified: delay=%+v builtin=%q", d, builtin)
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

func TestContextManualTestAcceptsReusedSuccessWithoutNewHistory(t *testing.T) {
	for _, kind := range []string{"group", "provider"} {
		c := &contextClient{alive: true, measured: true, measuredAt: time.Now().Add(-3 * time.Second)}
		s := configuredContext(t, c)
		owner := "G"
		if kind == "provider" {
			owner = "A"
		}
		got, err := s.TestNode(context.Background(), kind, owner, "Twin")
		if err != nil || got.DelayMS != 11 {
			t.Fatalf("reused %s measurement rejected: %+v %v", kind, got, err)
		}
	}
}

type reusedBatchClient struct{ contextClient }

func (c *reusedBatchClient) Get(ctx context.Context, path string, dst any) error {
	if strings.HasPrefix(path, "/group/G/delay?") {
		if !strings.Contains(path, "expected=204") {
			return fmt.Errorf("missing expected status")
		}
		return json.Unmarshal([]byte(`{"Twin":11}`), dst)
	}
	return c.contextClient.Get(ctx, path, dst)
}

func TestGroupManualTestAcceptsReusedSuccessWithoutNewHistory(t *testing.T) {
	c := &reusedBatchClient{contextClient{alive: true, measured: true, measuredAt: time.Now().Add(-3 * time.Second)}}
	s := NewService(c)
	if err := s.Configure([]byte("proxy-groups:\n- name: G\n  use: [A]\n  expected-status: 204\nproxy-providers:\n  A: {}\n")); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.DelaysKnown(context.Background(), Group{Name: "G", All: []string{"Twin"}}, "https://checks.example/test", 8000)
	if err != nil || len(got.Results) != 1 || got.Results[0].DelayMS == nil || *got.Results[0].DelayMS != 11 {
		t.Fatalf("reused group result rejected: %+v %v", got, err)
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
	if d, builtin := r.selectedLeaf(context.Background(), s, "Outer", map[string]bool{}); d != nil || builtin != "" {
		t.Fatalf("cycle resolved: delay=%+v builtin=%q", d, builtin)
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
