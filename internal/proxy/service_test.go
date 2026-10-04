package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeClient struct{ path, selected string }

func (f *fakeClient) Get(_ context.Context, path string, out any) error {
	f.path = path
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
