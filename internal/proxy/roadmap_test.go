package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type roadmapClient struct {
	selected map[string]string
	fail     map[string]bool
}

func (f *roadmapClient) Get(_ context.Context, path string, out any) error {
	switch {
	case path == "/proxies":
		return json.Unmarshal([]byte(`{"proxies":{"Choice":{"type":"Selector","all":["Slow","Fast","Missing"],"now":"Missing","alive":false},"Other":{"type":"Selector","all":["Fast"],"now":"Fast","alive":true}}}`), out)
	case path == "/proxies/Choice":
		return json.Unmarshal([]byte(`{"name":"Choice","type":"Selector","all":["Slow","Fast"],"now":"Gone","alive":true}`), out)
	case path == "/proxies/Other":
		return json.Unmarshal([]byte(`{"name":"Other","type":"Selector","all":["Fast"],"now":"Fast","alive":true}`), out)
	case strings.Contains(path, "/delay"):
		if strings.Contains(path, "Missing") {
			return errors.New("unavailable")
		}
		if strings.Contains(path, "Slow") {
			return json.Unmarshal([]byte(`{"delay":90}`), out)
		}
		return json.Unmarshal([]byte(`{"delay":10}`), out)
	}
	return errors.New("unknown path")
}
func (f *roadmapClient) Put(_ context.Context, path string, body any, _ any) error {
	if f.selected == nil {
		f.selected = map[string]string{}
	}
	f.selected[path] = body.(map[string]string)["name"]
	return nil
}
func TestSearchBatchAndSelectionStatus(t *testing.T) {
	s := NewService(&roadmapClient{})
	groups, err := s.Groups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if groups[0].SelectedStatus != "unavailable" {
		t.Fatalf("status: %+v", groups[0])
	}
	show, err := s.Show(context.Background(), "Choice")
	if err != nil || show.SelectedStatus != "removed" {
		t.Fatalf("show: %+v %v", show, err)
	}
	matches, err := s.Search(context.Background(), "fast")
	if err != nil || len(matches) != 2 || !reflect.DeepEqual(matches[0].All, []string{"Fast"}) {
		t.Fatalf("search: %+v %v", matches, err)
	}
	batch, err := s.Delays(context.Background(), "Choice", "https://example.com", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Results) != 2 || batch.Results[0].Proxy != "Fast" || batch.Results[1].Proxy != "Slow" {
		t.Fatalf("batch: %+v", batch)
	}
}
func TestSelectionsPersistAndReportRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-selections.json")
	store := SelectionStore{Path: path}
	if err := store.Save("work", "Choice", "Fast"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("work", "Old", "Gone"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("other", "Choice", "Slow"); err != nil {
		t.Fatal(err)
	}
	client := &roadmapClient{}
	result, err := store.Restore(context.Background(), "work", NewService(client))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Restored, []string{"Choice -> Fast"}) || !reflect.DeepEqual(result.Unavailable, []string{"Old -> Gone"}) || client.selected["/proxies/Choice"] != "Fast" {
		t.Fatalf("restore: %+v, selections=%v", result, client.selected)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
}
