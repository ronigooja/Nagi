package profile

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSubscriptionRebasesLocalNodesAndGroups(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	old := []byte(`proxies:
  - {name: Changed, type: direct, routing-mark: 1}
  - {name: Gone, type: direct}
  - {name: RemovedLocally, type: direct}
proxy-groups:
  - {name: Choice, type: select, proxies: [Changed, DIRECT]}
rules: ["MATCH,DIRECT"]
`)
	patch := []byte(`mode: global
proxies:
  - {name: Changed, type: direct, routing-mark: 1, interface-name: local-interface}
  - {name: Gone, type: direct}
  - {name: Manual, type: direct}
proxy-groups:
  - {name: Choice, type: select, proxies: [Changed, DIRECT]}
  - {name: LocalChoice, type: select, proxies: [Manual]}
`)
	next := []byte(`proxies:
  - {name: Changed, type: direct, routing-mark: 2}
  - {name: RemovedLocally, type: direct}
  - {name: New, type: direct}
proxy-groups:
  - {name: Choice, type: select, proxies: [Changed, New, DIRECT]}
  - {name: NewChoice, type: select, proxies: [New]}
rules: ["MATCH,DIRECT"]
`)
	if err := s.Apply(ctx, "work", old); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOverride(ctx, "work", patch); err != nil {
		t.Fatal(err)
	}
	preview, err := s.EffectiveSubscriptionSource("work", next)
	if err != nil {
		t.Fatal(err)
	}
	if saved, _ := s.Override("work"); string(saved) != string(patch) {
		t.Fatal("preview modified the override")
	}
	if err := s.ApplySubscription(ctx, "work", next); err != nil {
		t.Fatal(err)
	}
	effective, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	if string(effective) != string(preview) {
		t.Fatal("preview and applied configuration differ")
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(effective, &cfg); err != nil {
		t.Fatal(err)
	}
	_, nodes, _ := namedList(cfg["proxies"])
	if len(nodes) != 3 || nodes["Manual"] == nil || nodes["New"] == nil || nodes["Changed"]["routing-mark"] != 2 || nodes["Changed"]["interface-name"] != "local-interface" {
		t.Fatalf("unexpected effective nodes: %v", nodes)
	}
	_, groups, _ := namedList(cfg["proxy-groups"])
	if len(groups) != 3 || groups["LocalChoice"] == nil || !reflect.DeepEqual(groups["Choice"]["proxies"], []any{"Changed", "New", "DIRECT"}) || cfg["mode"] != "global" {
		t.Fatalf("unexpected effective groups or runtime settings: %v", cfg)
	}
	source, _ := s.Show("work")
	if string(source) != string(next) || strings.Contains(string(source), "Manual") {
		t.Fatal("local changes leaked into subscription source")
	}
	// A second update must continue advancing subscription fields while keeping
	// the manual node and local edits, rather than freezing the first snapshot.
	later := []byte(strings.Replace(string(next), "routing-mark: 2", "routing-mark: 3", 1))
	if err := s.ApplySubscription(ctx, "work", later); err != nil {
		t.Fatal(err)
	}
	effective, _ = s.Effective("work")
	if !strings.Contains(string(effective), "routing-mark: 3") || !strings.Contains(string(effective), "local-interface") || !strings.Contains(string(effective), "Manual") {
		t.Fatalf("second update lost intent or froze nodes: %s", effective)
	}
}

func TestSubscriptionRetainsDefaultSettingsAndRestorePoint(t *testing.T) {
	for _, failure := range []string{"none", "validate", "reload", "collision"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
			old := []byte("mode: rule\nproxies: [{name: A, type: ss}]\nproxy-groups: [{name: Proxy, type: select, proxies: [A]}]\nrules: ['MATCH,DIRECT']\n")
			patch := []byte("mode: global\nproxies: [{name: A, type: ss}, {name: Manual, type: direct}]\n")
			if err := s.Write(ctx, "default", []byte("mode: rule\nmixed-port: 17890\n")); err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(ctx, "work", old); err != nil {
				t.Fatal(err)
			}
			if err := s.SetOverride(ctx, "work", patch); err != nil {
				t.Fatal(err)
			}
			if err := s.ApplyDefaultOverride(ctx, "work", "Proxy"); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Override("work")
			statePath, _ := s.defaultOverrideStatePath("work")
			beforeState, _ := os.ReadFile(statePath)
			s.Validate = func(data []byte) error {
				if failure == "validate" {
					return errors.New("validation rejected")
				}
				return nil
			}
			s.Reload = func(context.Context) error {
				if failure == "reload" {
					return errors.New("activation rejected")
				}
				return nil
			}
			// Keep the DNS-bound node in this fixture, changing a harmless field.
			next := []byte("mode: rule\nproxies: [{name: A, type: ss, routing-mark: 2}, {name: B, type: direct}]\nproxy-groups: [{name: Proxy, type: select, proxies: [A]}]\nrules: ['MATCH,DIRECT']\n")
			if failure == "collision" {
				next = []byte(strings.Replace(string(next), "{name: B, type: direct}", "{name: Manual, type: reject}", 1))
			}
			err := s.ApplySubscription(ctx, "work", next)
			if failure != "none" {
				if err == nil {
					t.Fatal("expected rejected update")
				}
				gotSource, _ := s.Show("work")
				gotOverride, _ := s.Override("work")
				gotState, _ := os.ReadFile(statePath)
				if string(gotSource) != string(old) || string(gotOverride) != string(before) || string(gotState) != string(beforeState) {
					t.Fatal("failed update changed source, override, or restore point")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if active, err := s.DefaultOverrideStatus("work"); err != nil || !active {
				t.Fatalf("default settings status lost: %v, %v", active, err)
			}
			if err := s.RestoreDefaultOverride(ctx, "work"); err != nil {
				t.Fatal(err)
			}
			effective, _ := s.Effective("work")
			if !strings.Contains(string(effective), "mode: global") || !strings.Contains(string(effective), "name: Manual") || !strings.Contains(string(effective), "name: B") || !strings.Contains(string(effective), "routing-mark: 2") || strings.Contains(string(effective), "mixed-port: 17890") {
				t.Fatalf("restored override reverted feed or lost intent: %s", effective)
			}
		})
	}
}
