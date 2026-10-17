package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/proxy"
)

type delayScenarioClient struct {
	kind  string
	fixed string
	calls []string
}

func (c *delayScenarioClient) Get(_ context.Context, path string, out any) error {
	c.calls = append(c.calls, path)
	if c.kind == "reject-only" {
		switch {
		case path == "/proxies/Auto":
			b, _ := json.Marshal(proxy.Group{Name: "Auto", Type: "URLTest", All: []string{"REJECT"}, Fixed: c.fixed, Now: "REJECT"})
			return json.Unmarshal(b, out)
		case path == "/proxies":
			return json.Unmarshal([]byte(`{"proxies":{"REJECT":{"name":"REJECT","type":"Reject"}}}`), out)
		case path == "/providers/proxies":
			return json.Unmarshal([]byte(`{"providers":{}}`), out)
		case strings.HasPrefix(path, "/group/Auto/delay?"):
			c.fixed = ""
			return &control.APIError{Status: 504, Message: "get delay: all proxies timeout"}
		}
		return errors.New("unexpected request")
	}
	if strings.HasPrefix(path, "/group/") {
		switch c.kind {
		case "timeout":
			c.fixed = ""
			return &control.APIError{Status: 504, Message: "timeout"}
		case "ambiguous-reset":
			c.fixed = ""
			return errors.New("connection lost")
		case "ambiguous-kept":
			return errors.New("connection lost")
		default:
			return json.Unmarshal([]byte(`{"Node":15}`), out)
		}
	}
	if strings.HasPrefix(path, "/proxies/") {
		typ := "URLTest"
		if c.kind == "selector" {
			typ = "Selector"
		}
		b, _ := json.Marshal(proxy.Group{Name: "Auto", Type: typ, All: []string{"Node"}, Fixed: c.fixed, Now: "Node"})
		return json.Unmarshal(b, out)
	}
	return errors.New("unexpected request")
}
func (*delayScenarioClient) Put(context.Context, string, any, any) error { return nil }

func TestRejectOnlyGroupStillCallsNativeAndPersistsAutomaticReset(t *testing.T) {
	dir := t.TempDir()
	store := proxy.SelectionStore{Path: filepath.Join(dir, "choices.json")}
	if err := store.Save("default", "Auto", "REJECT"); err != nil {
		t.Fatal(err)
	}
	c := &delayScenarioClient{kind: "reject-only", fixed: "REJECT"}
	service := proxy.NewService(c)
	if err := service.Configure([]byte("proxy-groups:\n- name: Auto\n  type: url-test\n  proxies: [REJECT]\n  url: https://example.com\n")); err != nil {
		t.Fatal(err)
	}
	value, err := proxyCommand(context.Background(), []string{"proxy", "delays", "Auto"}, service, store, dir)
	if err != nil {
		t.Fatal(err)
	}
	r := value.(proxy.BatchDelayResult)
	if len(r.Results) != 1 || r.Results[0].Unmeasurable == "" || !r.AutomaticReset || !r.SavedSelectionRemoved || c.fixed != "" {
		t.Fatalf("result=%+v fixed=%q", r, c.fixed)
	}
	nativeCall := false
	for _, path := range c.calls {
		if strings.HasPrefix(path, "/group/Auto/delay?") {
			nativeCall = true
		}
	}
	if !nativeCall {
		t.Fatalf("native group test was not called: %v", c.calls)
	}
	b, err := os.ReadFile(store.Path)
	if err != nil || strings.Contains(string(b), `"Auto"`) {
		t.Fatalf("saved fixed choice was not removed: %s, %v", b, err)
	}
}

func TestGroupDelayFailureStates(t *testing.T) {
	for _, tc := range []struct {
		name, kind             string
		wantRemoved, wantError bool
		calls                  int
	}{
		{"accepted", "accepted", true, false, 2},
		{"handler timeout", "timeout", true, true, 2},
		{"transport reset observed", "ambiguous-reset", true, true, 3},
		{"transport unresolved", "ambiguous-kept", false, true, 3},
		{"selector", "selector", false, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := proxy.SelectionStore{Path: filepath.Join(dir, "choices.json")}
			if err := store.Save("default", "Auto", "Node"); err != nil {
				t.Fatal(err)
			}
			c := &delayScenarioClient{kind: tc.kind, fixed: "Node"}
			result, err := proxyCommand(context.Background(), []string{"proxy", "delays", "Auto"}, proxy.NewService(c), store, dir)
			if (err != nil) != tc.wantError {
				t.Fatalf("result=%v err=%v", result, err)
			}
			if len(c.calls) != tc.calls {
				t.Fatalf("calls=%v", c.calls)
			}
			b, readErr := os.ReadFile(store.Path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			removed := !strings.Contains(string(b), `"Auto"`)
			if removed != tc.wantRemoved {
				t.Fatalf("saved=%s err=%v", b, err)
			}
			if err == nil {
				r := result.(proxy.BatchDelayResult)
				if r.AutomaticReset != tc.wantRemoved || r.SavedSelectionRemoved != tc.wantRemoved {
					t.Fatalf("result=%+v", r)
				}
			}
			if tc.kind == "ambiguous-kept" && !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("missing recovery context: %v", err)
			}
		})
	}
}
func TestGroupDelayPreDispatchAndPersistenceFailure(t *testing.T) {
	t.Run("invalid URL", func(t *testing.T) {
		dir := t.TempDir()
		store := proxy.SelectionStore{Path: filepath.Join(dir, "choices.json")}
		if err := store.Save("default", "Auto", "Node"); err != nil {
			t.Fatal(err)
		}
		c := &delayScenarioClient{fixed: "Node"}
		_, err := proxyCommand(context.Background(), []string{"proxy", "delays", "Auto", "file:///tmp/test"}, proxy.NewService(c), store, dir)
		if err == nil || len(c.calls) != 1 {
			t.Fatalf("err=%v calls=%v", err, c.calls)
		}
		b, _ := os.ReadFile(store.Path)
		if !strings.Contains(string(b), `"Auto"`) {
			t.Fatalf("saved choice removed: %s", b)
		}
	})
	t.Run("profile unavailable", func(t *testing.T) {
		dir := t.TempDir()
		store := proxy.SelectionStore{Path: filepath.Join(dir, "choices.json")}
		if err := store.Save("default", "Auto", "Node"); err != nil {
			t.Fatal(err)
		}
		// A directory at the profile-selection path makes Current fail before dispatch.
		if err := os.MkdirAll(filepath.Join(dir, "settings.yaml"), 0700); err != nil {
			t.Fatal(err)
		}
		c := &delayScenarioClient{fixed: "Node"}
		_, err := proxyCommand(context.Background(), []string{"proxy", "delays", "Auto"}, proxy.NewService(c), store, dir)
		if err == nil || len(c.calls) != 1 {
			t.Fatalf("err=%v calls=%v", err, c.calls)
		}
		b, _ := os.ReadFile(store.Path)
		if !strings.Contains(string(b), `"Auto"`) {
			t.Fatalf("saved choice removed: %s", b)
		}
	})
	t.Run("save removal fails", func(t *testing.T) {
		dir := t.TempDir()
		store := proxy.SelectionStore{Path: filepath.Join(dir, "choices.json")}
		if err := os.WriteFile(store.Path, []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
		c := &delayScenarioClient{fixed: "Node"}
		_, err := proxyCommand(context.Background(), []string{"proxy", "delays", "Auto"}, proxy.NewService(c), store, dir)
		if err == nil || !strings.Contains(err.Error(), "running engine") || len(c.calls) != 2 {
			t.Fatalf("err=%v calls=%v", err, c.calls)
		}
	})
}

func TestGroupDelayUnavailableProfilePreventsImplicitDispatch(t *testing.T) {
	c := &delayScenarioClient{kind: "selector"}
	service := proxy.NewService(c)
	service.RequireEffectiveConfig()
	_, err := proxyCommand(context.Background(), []string{"proxy", "delays", "Auto"}, service, proxy.SelectionStore{}, t.TempDir())
	if err == nil || len(c.calls) != 0 {
		t.Fatalf("implicit request err=%v calls=%v", err, c.calls)
	}
	_, err = proxyCommand(context.Background(), []string{"proxy", "delays", "Auto", "https://example.test", "5000"}, service, proxy.SelectionStore{}, t.TempDir())
	if err != nil || len(c.calls) != 2 {
		t.Fatalf("explicit request err=%v calls=%v", err, c.calls)
	}
}
