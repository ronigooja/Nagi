package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeClient struct{ path, selected, method, bodyPath string }

func (f *fakeClient) Get(_ context.Context, path string, out any) error {
	f.path = path
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
