package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type providerClient struct {
	paths          []string
	fail           bool
	topLevelRemote bool
}

func (f *providerClient) Get(_ context.Context, path string, out any) error {
	f.paths = append(f.paths, path)
	if path == "/providers/proxies" {
		return json.Unmarshal([]byte(`{"providers":{"COMPATIBLE":{"vehicleType":"Compatible","proxies":[{"name":"DIRECT"}]},"work":{"name":"work","type":"Proxy","vehicleType":"HTTP","updatedAt":"2026-01-01T00:00:00Z","testUrl":"https://secret.example/path?token=private","proxies":[{"name":"Remote","server":"secret.example","password":"private"}]}}}`), out)
	}
	if path == "/proxies" {
		if f.topLevelRemote {
			return json.Unmarshal([]byte(`{"proxies":{"Remote":{"name":"Remote"}}}`), out)
		}
		return json.Unmarshal([]byte(`{"proxies":{"DIRECT":{"name":"DIRECT"}}}`), out)
	}
	if strings.HasPrefix(path, "/providers/proxies/work/Remote/healthcheck?") {
		return json.Unmarshal([]byte(`{"delay":42}`), out)
	}
	if path == "/proxies/Auto" {
		return json.Unmarshal([]byte(`{"name":"Auto","type":"URLTest","all":["Remote"],"now":"Remote","fixed":"Remote"}`), out)
	}
	if strings.HasPrefix(path, "/group/Auto/delay?") {
		return json.Unmarshal([]byte(`{"Remote":30}`), out)
	}
	return errors.New("unexpected path")
}
func (f *providerClient) Put(_ context.Context, path string, _ any, _ any) error {
	f.paths = append(f.paths, path)
	if f.fail {
		return errors.New("https://secret.example/?token=private")
	}
	return nil
}
func (f *providerClient) Delete(_ context.Context, path string, _ any) error {
	f.paths = append(f.paths, path)
	return nil
}
func TestProviderRedactionAndNativeNodeDelay(t *testing.T) {
	f := &providerClient{}
	s := NewService(f)
	providers, err := s.Providers(context.Background())
	if err != nil || len(providers) != 1 || providers[0].Nodes[0] != "Remote" {
		t.Fatalf("providers=%+v err=%v", providers, err)
	}
	b, _ := json.Marshal(providers)
	if strings.Contains(string(b), "token=") || strings.Contains(string(b), "?token") || strings.Contains(string(b), "password") || strings.Contains(string(b), "testUrl") {
		t.Fatalf("sensitive provider response: %s", b)
	}
	if _, fromProvider, err := s.providerForNode(context.Background(), "DIRECT"); err != nil || fromProvider {
		t.Fatalf("built-in node assigned to provider: %v %v", fromProvider, err)
	}
	delay, err := s.Delay(context.Background(), "Remote", "https://example.test", 5000)
	if err != nil || delay.DelayMS != 42 || !strings.HasPrefix(f.paths[len(f.paths)-1], "/providers/proxies/work/Remote/healthcheck?") {
		t.Fatalf("delay=%+v err=%v paths=%v", delay, err, f.paths)
	}
	f.topLevelRemote = true
	if _, fromProvider, err := s.providerForNode(context.Background(), "Remote"); err != nil || fromProvider {
		t.Fatalf("top-level node was shadowed: %v %v", fromProvider, err)
	}
	f.fail = true
	if err := s.ProviderUpdate(context.Background(), "work"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unredacted update error: %v", err)
	}
}
func TestAutoAndGroupDelayUseNativeRoutesAndRemoveSavedPin(t *testing.T) {
	f := &providerClient{}
	s := NewService(f)
	if err := s.Auto(context.Background(), "Auto"); err != nil || f.paths[len(f.paths)-1] != "/proxies/Auto" {
		t.Fatalf("auto err=%v paths=%v", err, f.paths)
	}
	result, err := s.Delays(context.Background(), "Auto", "https://example.test", 5000)
	if err != nil || len(result.Results) != 1 || *result.Results[0].DelayMS != 30 || !strings.HasPrefix(f.paths[len(f.paths)-1], "/group/Auto/delay?") {
		t.Fatalf("result=%+v err=%v paths=%v", result, err, f.paths)
	}
	store := SelectionStore{Path: filepath.Join(t.TempDir(), "choices.json")}
	if err := store.Save("profile", "Auto", "Remote"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("profile", "Manual", "Remote"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveGroup("profile", "Auto"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(store.Path)
	if err != nil || strings.Contains(string(b), "Auto") || !strings.Contains(string(b), "Manual") {
		t.Fatalf("choices=%s err=%v", b, err)
	}
}

type chainRefreshClient struct {
	providerClient
	failPath string
}

func (f *chainRefreshClient) Put(_ context.Context, path string, _ any, _ any) error {
	f.paths = append(f.paths, path)
	if path == f.failPath {
		return errors.New("https://secret.example/?private=credential")
	}
	return nil
}
func TestProviderUpdateRefreshesAllExactDependents(t *testing.T) {
	for _, fail := range []string{"", "/providers/proxies/work", "/providers/proxies/derived-one"} {
		f := &chainRefreshClient{failPath: fail}
		s := NewService(f)
		s.ProviderChainSources = map[string][]string{"work": {"derived-one", "derived-two"}, "other": {"unrelated"}}
		err := s.ProviderUpdate(context.Background(), "work")
		if (err != nil) != (fail != "") || err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		calls := strings.Join(f.paths, ",")
		if strings.Contains(calls, "unrelated") {
			t.Fatal(calls)
		}
		refreshed := strings.Contains(calls, "/providers/proxies/derived-two")
		if refreshed != (fail != "/providers/proxies/work") {
			t.Fatal(calls)
		}
		if fail == "/providers/proxies/derived-one" && !strings.Contains(err.Error(), "provider updated") {
			t.Fatal(err)
		}
	}
}
