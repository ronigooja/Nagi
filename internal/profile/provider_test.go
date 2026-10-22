package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func providerFixture(t *testing.T) (*Store, []byte, []byte) {
	t.Helper()
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	source := []byte("mixed-port: 17890\ndns:\n  enable: true\nproxies:\n  - name: Local\n    type: direct\nproxy-providers:\n  original:\n    type: inline\n    payload: []\nproxy-groups:\n  - name: My Proxy\n    type: select\n    proxies: [Local, DIRECT]\n    use: [original]\n  - name: Auto\n    type: url-test\n    use: [original]\n    url: https://example.com/204\n    interval: 180\nrules: [MATCH,DIRECT]\n")
	override := []byte("dns:\n  ipv6: true\nmode: global\n")
	if err := s.Write(context.Background(), "default", source); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOverride(context.Background(), "default", override); err != nil {
		t.Fatal(err)
	}
	return s, source, override
}
func yamlMapping(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestAddProviderPreservesUnrelatedConfiguration(t *testing.T) {
	for _, group := range []string{"My Proxy", "New Proxy", "Auto"} {
		t.Run(group, func(t *testing.T) {
			s, source, _ := providerFixture(t)
			before, _ := s.Effective("default")
			reloads := 0
			s.Reload = func(context.Context) error { reloads++; return nil }
			created, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/feed?token=private", group, 7200)
			if err != nil || created != (group == "New Proxy") || reloads != 1 {
				t.Fatalf("created=%v err=%v reloads=%d", created, err, reloads)
			}
			after, _ := s.Effective("default")
			previous, current := yamlMapping(t, before), yamlMapping(t, after)
			for _, key := range []string{"dns", "rules", "proxies", "mode", "mixed-port"} {
				if !reflect.DeepEqual(previous[key], current[key]) {
					t.Errorf("%s changed", key)
				}
			}
			savedSource, _ := s.Show("default")
			if string(savedSource) != string(source) {
				t.Fatal("source changed")
			}
			providers := current["proxy-providers"].(map[string]any)
			if !reflect.DeepEqual(providers["original"], previous["proxy-providers"].(map[string]any)["original"]) {
				t.Fatal("old provider changed")
			}
			provider := providers["work"].(map[string]any)
			if provider["type"] != "http" || provider["interval"] != 7200 || provider["health-check"].(map[string]any)["interval"] != 300 {
				t.Fatalf("provider=%v", provider)
			}
			wantURL := "https://cp.cloudflare.com/generate_204"
			if group == "Auto" {
				wantURL = "https://example.com/204"
			}
			if got := provider["health-check"].(map[string]any)["url"]; got != wantURL {
				t.Fatalf("healthcheck URL=%v, want %s", got, wantURL)
			}
			groups := current["proxy-groups"].([]any)
			for _, value := range groups {
				item := value.(map[string]any)
				if item["name"] == group {
					use := item["use"].([]any)
					if use[len(use)-1] != "work" {
						t.Fatalf("use=%v", use)
					}
					if created && !reflect.DeepEqual(item["proxies"], []any{"DIRECT"}) {
						t.Fatalf("new group=%v", item)
					}
				} else {
					matched := false
					for _, old := range previous["proxy-groups"].([]any) {
						if reflect.DeepEqual(old, item) {
							matched = true
						}
					}
					if !matched {
						t.Fatalf("unrelated group changed: %v", item)
					}
				}
			}
			info, _ := os.Stat(filepath.Join(s.Dir, "overrides", "default.yaml"))
			if info.Mode().Perm() != 0600 {
				t.Fatalf("override permissions=%v", info.Mode())
			}
		})
	}
}

func TestAddProviderUsesAutomaticGroupHealthcheck(t *testing.T) {
	for _, kind := range []string{"url-test", "fallback", "load-balance"} {
		for _, inherited := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "explicit", true: "inherited"}[inherited], func(t *testing.T) {
				s, _, _ := providerFixture(t)
				data, _ := s.Show("default")
				doc := yamlMapping(t, data)
				group := doc["proxy-groups"].([]any)[1].(map[string]any)
				group["type"] = kind
				group["url"] = "http://example.com/generate_204"
				group["expected-status"] = "204"
				if inherited {
					delete(group, "url")
					doc["proxy-providers"].(map[string]any)["original"].(map[string]any)["health-check"] = map[string]any{"url": "http://example.com/generate_204"}
				}
				data, _ = yaml.Marshal(doc)
				if err := s.Write(context.Background(), "default", data); err != nil {
					t.Fatal(err)
				}
				if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/feed", "Auto", 3600); err != nil {
					t.Fatal(err)
				}
				data, _ = s.Effective("default")
				current := yamlMapping(t, data)
				check := current["proxy-providers"].(map[string]any)["work"].(map[string]any)["health-check"].(map[string]any)
				if check["url"] != "http://example.com/generate_204" || check["expected-status"] != "204" || check["enable"] != true {
					t.Fatalf("healthcheck=%v", check)
				}
				currentGroup := current["proxy-groups"].([]any)[1].(map[string]any)
				delete(currentGroup, "use")
				delete(group, "use")
				if !reflect.DeepEqual(currentGroup, group) {
					t.Fatalf("group settings changed: %v", currentGroup)
				}
			})
		}
	}
}
func TestAddProviderFailuresPreserveFiles(t *testing.T) {
	for _, stage := range []string{"validation", "reload", "duplicate", "builtin", "node", "provider", "unsupported", "malformed"} {
		t.Run(stage, func(t *testing.T) {
			s, _, _ := providerFixture(t)
			name, group := "work", "My Proxy"
			switch stage {
			case "validation":
				s.Validate = func([]byte) error { return errors.New("validation failed") }
			case "reload":
				s.Reload = func(context.Context) error { return errors.New("reload failed") }
			case "duplicate":
				name = "original"
			case "builtin":
				group = "GLOBAL"
			case "node":
				group = "Local"
			case "provider":
				group = "original"
			case "unsupported", "malformed":
				source, _ := s.Show("default")
				doc := yamlMapping(t, source)
				if stage == "unsupported" {
					doc["proxy-groups"].([]any)[0].(map[string]any)["type"] = "relay"
				} else {
					doc["proxy-providers"] = []any{}
				}
				bytes, _ := yaml.Marshal(doc)
				if err := s.Write(context.Background(), "default", bytes); err != nil {
					t.Fatal(err)
				}
			}
			source, _ := s.Show("default")
			override, _ := s.Override("default")
			if _, err := s.AddProvider(context.Background(), "default", name, "https://example.com/feed", group, 3600); err == nil {
				t.Fatal("accepted failed operation")
			}
			afterSource, _ := s.Show("default")
			afterOverride, _ := s.Override("default")
			if string(source) != string(afterSource) || string(override) != string(afterOverride) {
				t.Fatal("failed operation changed files")
			}
		})
	}
}
func TestAddProviderRejectsOverrideDuplicateAndRestoresAbsentOverride(t *testing.T) {
	s, _, _ := providerFixture(t)
	if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/feed", "My Proxy", 3600); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Override("default")
	if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/other", "My Proxy", 3600); err == nil {
		t.Fatal("accepted duplicate")
	}
	after, _ := s.Override("default")
	if string(before) != string(after) {
		t.Fatal("duplicate changed override")
	}
	if err := s.ClearOverride(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	s.Validate = func([]byte) error { return errors.New("validation failed") }
	if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/feed", "My Proxy", 3600); err == nil {
		t.Fatal("accepted invalid config")
	}
	if _, err := s.Override("default"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("override left behind: %v", err)
	}
}
