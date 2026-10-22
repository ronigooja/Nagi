package installation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestUpdateErrorSourceMetadataPersistsWithCompatibleText(t *testing.T) {
	fixture(t)
	source := i18n.Errorf("system.installation.cannot.start.background.update", "cannot start background update: %w", errors.New("open https://user:fictional@private.invalid/feed?token=fictional: unavailable"))
	if err := SaveUpdateError(source); err != nil {
		t.Fatal(err)
	}
	path, err := updateErrorPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + ".messages"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("permissions %s: %o", name, info.Mode().Perm())
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "fictional") {
			t.Fatalf("sensitive sidecar or message: %s", data)
		}
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "cannot start background update: open https://private.invalid/feed?token=%5Bredacted%5D unavailable" {
		t.Fatalf("compatible text=%q", text)
	}
	manager := &Manager{OS: "darwin"}
	result, err := manager.ManageWithMessages(context.Background(), "app-update-error", "")
	if err != nil {
		t.Fatal(err)
	}
	value := result.Value.(map[string]any)
	if value["error"] != string(text) {
		t.Fatalf("data=%+v", value)
	}
	descriptor, ok := result.Messages["/error"]
	if !ok {
		t.Fatal("missing source descriptor")
	}
	if got := i18n.New(i18n.SimplifiedChinese).RenderDescriptor(descriptor, value["error"].(string)); got != "无法启动后台更新：open https://private.invalid/feed?token=%5Bredacted%5D unavailable" {
		t.Fatalf("localized=%q", got)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "messages") || !strings.Contains(string(encoded), "cannot start") {
		t.Fatalf("business JSON=%s", encoded)
	}
	for _, name := range []string{path, path + ".messages"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("not consumed %s: %v", name, err)
		}
	}
}

func TestUpdateErrorMetadataInvalidOrStaleFallsBack(t *testing.T) {
	for _, kind := range []string{"legacy", "malformed", "digest", "arguments", "unknown", "null"} {
		t.Run(kind, func(t *testing.T) {
			fixture(t)
			source := i18n.Errorf("system.installation.missing.prepared.update.artifact", "missing prepared update artifact")
			if err := SaveUpdateError(source); err != nil {
				t.Fatal(err)
			}
			path, err := updateErrorPath()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "legacy":
				if err := os.Remove(path + ".messages"); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(path+".messages", []byte("bad metadata"), 0600); err != nil {
					t.Fatal(err)
				}
			case "digest":
				if err := os.WriteFile(path, []byte("different historical English"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				data, err := os.ReadFile(path + ".messages")
				if err != nil {
					t.Fatal(err)
				}
				var metadata map[string]any
				if err := json.Unmarshal(data, &metadata); err != nil {
					t.Fatal(err)
				}
				message := metadata["message"].(map[string]any)
				if kind == "arguments" {
					message["args"] = map[string]any{"secret": "not part of signature"}
				}
				if kind == "unknown" {
					message["id"] = "unknown.event"
				}
				if kind == "null" {
					message["args"] = nil
				}
				data, err = json.Marshal(metadata)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".messages", data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := (&Manager{OS: "darwin"}).ManageWithMessages(context.Background(), "app-update-error", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Messages) != 0 || result.Value.(map[string]any)["error"] == "" {
				t.Fatalf("fallback=%+v", result)
			}
			if _, err := os.Stat(path + ".messages"); !os.IsNotExist(err) {
				t.Fatalf("sidecar not consumed: %v", err)
			}
		})
	}
}
