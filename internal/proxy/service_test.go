package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
)

type fakeClient struct{ path, selected, method, bodyPath string }

type rejectDelayClient struct {
	calls     []string
	failMixed bool
}

func (c *rejectDelayClient) Get(_ context.Context, path string, out any) error {
	c.calls = append(c.calls, path)
	switch {
	case path == "/proxies":
		return json.Unmarshal([]byte(`{"proxies":{"REJECT":{"name":"REJECT","type":"Reject"},"Node":{"name":"Node","type":"Shadowsocks"},"Nested":{"name":"Nested","type":"Selector","all":["REJECT"],"now":"REJECT"}}}`), out)
	case path == "/providers/proxies":
		return json.Unmarshal([]byte(`{"providers":{"feed":{"vehicleType":"HTTP","proxies":[{"name":"REJECT","type":"Shadowsocks"}]}}}`), out)
	case strings.HasPrefix(path, "/group/Only/delay?") || strings.HasPrefix(path, "/group/Parent/delay?"):
		return &control.APIError{Status: 504, Message: "get delay: all proxies timeout"}
	case strings.HasPrefix(path, "/group/Mixed/delay?"):
		if c.failMixed {
			return &control.APIError{Status: 504, Message: "get delay: all proxies timeout"}
		}
		return json.Unmarshal([]byte(`{"Node":42}`), out)
	case strings.HasPrefix(path, "/group/Provider/delay?"):
		return json.Unmarshal([]byte(`{"REJECT":23}`), out)
	}
	return errors.New("unexpected path")
}
func (*rejectDelayClient) Put(context.Context, string, any, any) error { return nil }

func TestRejectOnlyNativeFailureIsUnmeasurable(t *testing.T) {
	c := &rejectDelayClient{}
	s := NewService(c)
	if err := s.Configure([]byte("proxy-groups:\n- name: Only\n  type: select\n  proxies: [REJECT]\n- name: Mixed\n  type: select\n  proxies: [REJECT, Node]\n- name: Provider\n  type: select\n  use: [feed]\n- name: Nested\n  type: select\n  proxies: [REJECT]\n- name: Parent\n  type: select\n  proxies: [Nested]\n")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ group, member string }{{"Only", "REJECT"}, {"Parent", "Nested"}} {
		result, state, err := s.DelaysKnown(context.Background(), Group{Name: tc.group, All: []string{tc.member}}, "https://example.com", 1000)
		if err != nil || state != DelayResetConfirmed || len(result.Results) != 1 || result.Results[0].Unmeasurable == "" || result.Results[0].Error != "" {
			t.Fatalf("%s: result=%+v state=%v err=%v", tc.group, result, state, err)
		}
	}
	mixed, state, err := s.DelaysKnown(context.Background(), Group{Name: "Mixed", All: []string{"REJECT", "Node"}}, "https://example.com", 1000)
	if err != nil || state != DelayResetConfirmed || len(mixed.Results) != 2 || mixed.Results[0].Proxy != "Node" || mixed.Results[0].DelayMS == nil || *mixed.Results[0].DelayMS != 42 || mixed.Results[1].Unmeasurable == "" || mixed.Results[1].Error != "" {
		t.Fatalf("mixed result=%+v state=%v err=%v", mixed, state, err)
	}
	provider, _, err := s.DelaysKnown(context.Background(), Group{Name: "Provider", All: []string{"REJECT"}}, "https://example.com", 1000)
	if err != nil || len(provider.Results) != 1 || provider.Results[0].DelayMS == nil || *provider.Results[0].DelayMS != 23 || provider.Results[0].Unmeasurable != "" {
		t.Fatalf("provider node named REJECT: %+v, %v", provider, err)
	}
	c.failMixed = true
	_, _, err = s.DelaysKnown(context.Background(), Group{Name: "Mixed", All: []string{"REJECT", "Node"}}, "https://example.com", 1000)
	if err == nil {
		t.Fatal("real node failure was treated as an expected reject result")
	}
	for _, path := range c.calls {
		if strings.HasPrefix(path, "/group/Only/delay?") || strings.HasPrefix(path, "/group/Parent/delay?") {
			return
		}
	}
	t.Fatal("native group test was not called")
}

func (f *fakeClient) Get(_ context.Context, path string, out any) error {
	f.path = path
	if path == "/providers/proxies" {
		return json.Unmarshal([]byte(`{"providers":{}}`), out)
	}
	if path == "/configs" {
		return json.Unmarshal([]byte(`{"mode":"global"}`), out)
	}
	if len(path) > 6 && path[len(path)-6:] == "/delay" {
		return json.Unmarshal([]byte(`{"delay":123}`), out)
	}
	data := []byte(`{"name":"Group A","type":"Selector","all":["Node A","Node B"],"now":"Node A"}`)
	return json.Unmarshal(data, out)
}

func (f *fakeClient) Put(_ context.Context, path string, body any, _ any) error {
	if path != "/proxies/Group%20A" {
		return errors.New("wrong path")
	}
	f.selected = body.(map[string]string)["name"]
	return nil
}

func (f *fakeClient) Delete(_ context.Context, path string, _ any) error {
	f.method, f.path = "DELETE", path
	return nil
}
func (f *fakeClient) Patch(_ context.Context, path string, _ any, _ any) error {
	f.method, f.path = "PATCH", path
	return nil
}

func TestSelectChecksMembership(t *testing.T) {
	f := &fakeClient{}
	s := NewService(f)
	if err := s.Select(context.Background(), "Group A", "Unknown"); err == nil {
		t.Fatal("selected absent node")
	}
	if err := s.Select(context.Background(), "Group A", "Node B"); err != nil {
		t.Fatal(err)
	}
	if f.path != "/proxies/Group%20A" || f.selected != "Node B" {
		t.Fatalf("path=%q selected=%q", f.path, f.selected)
	}
	if err := s.Select(context.Background(), "Group A", "Node A"); err != nil || f.selected != "Node A" {
		t.Fatalf("explicit choice matching current node must reach mihomo: %v, selected=%q", err, f.selected)
	}
}

func TestDelayEscapesQueryAndCloseMode(t *testing.T) {
	f := &fakeClient{}
	s := NewService(f)
	result, err := s.Delay(context.Background(), "Node/A", "https://example.test/a?x=1", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proxy != "Node/A" || result.TimeoutMS != 30000 || f.path != "/proxies/Node%2FA/delay?timeout=30000&url=https%3A%2F%2Fexample.test%2Fa%3Fx%3D1" {
		t.Fatalf("result=%+v path=%q", result, f.path)
	}
	if err := s.Close(context.Background(), "id/1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background(), " "); err == nil || f.path == "/connections" {
		t.Fatalf("empty ID close: err=%v path=%q", err, f.path)
	}
	if f.method != "DELETE" || f.path != "/connections/id%2F1" {
		t.Fatalf("method=%q path=%q", f.method, f.path)
	}
	mode := "global"
	if _, err := s.Mode(context.Background(), &mode); err != nil {
		t.Fatal(err)
	}
	if f.method != "PATCH" || f.path != "/configs" {
		t.Fatalf("method=%q path=%q", f.method, f.path)
	}
}

type connectionClient struct{}

func (connectionClient) Get(_ context.Context, path string, out any) error {
	if path != "/connections" {
		return errors.New("unexpected API path")
	}
	return json.Unmarshal([]byte(`{"connections":[{"id":"id/1","chains":["Group","Node"],"upload":5,"download":9}]}`), out)
}
func (connectionClient) Put(context.Context, string, any, any) error { return nil }
func TestConnectionInspectByID(t *testing.T) {
	service := NewService(connectionClient{})
	connection, err := service.Connection(context.Background(), "id/1")
	if err != nil || connection.ID != "id/1" || len(connection.Chains) != 2 {
		t.Fatalf("connection=%+v err=%v", connection, err)
	}
	if _, err := service.Connection(context.Background(), "gone"); err == nil {
		t.Fatal("missing connection accepted")
	}
}
