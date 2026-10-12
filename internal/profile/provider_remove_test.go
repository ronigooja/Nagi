package profile

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRemoveProviderFromSourceAndOverride(t *testing.T) {
	for _, name := range []string{"original", "work"} {
		t.Run(name, func(t *testing.T) {
			s, source, _ := providerFixture(t)
			if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/feed", "My Proxy", 3600); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Effective("default")
			reloads := 0
			s.Reload = func(context.Context) error { reloads++; return nil }
			if err := s.RemoveProvider(context.Background(), "default", name); err != nil {
				t.Fatal(err)
			}
			if reloads != 1 {
				t.Fatalf("reloads=%d", reloads)
			}
			after, _ := s.Effective("default")
			previous, current := yamlMapping(t, before), yamlMapping(t, after)
			if _, exists := current["proxy-providers"].(map[string]any)[name]; exists {
				t.Fatal("provider remains")
			}
			for _, key := range []string{"dns", "rules", "proxies", "mode", "mixed-port"} {
				if !reflect.DeepEqual(previous[key], current[key]) {
					t.Fatalf("%s changed", key)
				}
			}
			for _, value := range current["proxy-groups"].([]any) {
				group := value.(map[string]any)
				for _, value := range group["use"].([]any) {
					if value == name {
						t.Fatal("reference remains")
					}
				}
			}
			saved, _ := s.Show("default")
			if name == "work" && string(saved) != string(source) {
				t.Fatal("local removal changed source")
			}
			if err := s.ClearOverride(context.Background(), "default"); err != nil {
				t.Fatal(err)
			}
			if name == "original" {
				data, _ := s.Effective("default")
				if _, exists := yamlMapping(t, data)["proxy-providers"].(map[string]any)[name]; exists {
					t.Fatal("source provider reappeared")
				}
			}
		})
	}
}

func TestRemoveProviderFailureRestoresBothLayers(t *testing.T) {
	for _, stage := range []string{"missing", "validation", "reload"} {
		t.Run(stage, func(t *testing.T) {
			s, _, _ := providerFixture(t)
			if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/feed", "My Proxy", 3600); err != nil {
				t.Fatal(err)
			}
			source, _ := s.Show("default")
			overlay, _ := s.Override("default")
			name := "original"
			switch stage {
			case "missing":
				name = "missing"
			case "validation":
				s.Validate = func([]byte) error { return errors.New("invalid remaining references") }
			case "reload":
				s.Reload = func(context.Context) error { return errors.New("activation failed") }
			}
			if err := s.RemoveProvider(context.Background(), "default", name); err == nil {
				t.Fatal("expected failure")
			}
			gotSource, _ := s.Show("default")
			gotOverlay, _ := s.Override("default")
			if string(source) != string(gotSource) || string(overlay) != string(gotOverlay) {
				t.Fatal("failed removal changed files")
			}
		})
	}
}

func TestRemoveSourceProviderWithoutOverride(t *testing.T) {
	s, _, _ := providerFixture(t)
	if err := s.ClearOverride(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProvider(context.Background(), "default", "original"); err != nil {
		t.Fatal(err)
	}
	data, _ := s.Effective("default")
	if len(yamlMapping(t, data)["proxy-providers"].(map[string]any)) != 0 {
		t.Fatal("provider remains")
	}
}
