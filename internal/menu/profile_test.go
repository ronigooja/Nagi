package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestProfileListAndFileAction(t *testing.T) {
	ctx := context.Background()
	runner := &featureRunner{results: map[string]Result{
		"profile\x00list": {Data: map[string]any{"profiles": []string{"work", "default"}, "current": "default"}},
		"profile\x00export\x00default\x00/tmp/Profile A.yaml": {Data: map[string]any{"name": "default", "file": "/tmp/Profile A.yaml", "exported": true}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("/tmp/Profile A.yaml\n"), &out)
	view, err := profileListPage().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Items[3].Label, "selected") {
		t.Fatalf("current profile not marked: %q", view.Items[3].Label)
	}
	detail, err := view.Items[3].Next().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := detail.Items[1].Action(ctx, s); err != nil {
		t.Fatal(err)
	}
	want := []string{"profile", "export", "default", "/tmp/Profile A.yaml"}
	if !reflect.DeepEqual(runner.calls[len(runner.calls)-1], want) {
		t.Fatalf("export args = %#v, want %#v", runner.calls[len(runner.calls)-1], want)
	}
	if detail.Items[8].Confirm == "" || detail.Items[3].Confirm == "" {
		t.Fatal("profile removal and restore must require confirmation")
	}
}

func TestSubscriptionPreviewBeforeApplyAndURLRedaction(t *testing.T) {
	ctx := context.Background()
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00preview\x00feed": {Data: map[string]any{"name": "feed", "added": []string{"Node A", "HTTPS://secret.example/token", "trojan://password@secret.example:443"}, "removed": []string{"Old"}, "changed": []string{"Changed"}}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	view, err := subscriptionDetailPage("feed").Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Context, "Added: Node A") || !strings.Contains(view.Context, "Removed: Old") || !strings.Contains(view.Context, "Changed: Changed") {
		t.Fatalf("preview absent before apply: %q", view.Context)
	}
	if strings.Contains(view.Context, "secret.example") || !strings.Contains(view.Context, "[redacted URL]") {
		t.Fatalf("URL leaked in preview: %q", view.Context)
	}
	if view.Items[2].Confirm == "" || view.Items[3].Confirm == "" {
		t.Fatal("subscription apply and remove must require confirmation")
	}
}

func TestSubscriptionApplyUnavailableWithoutSuccessfulPreview(t *testing.T) {
	for _, result := range []Result{
		{Code: 1, Error: &CLIError{Code: "subscription_error", Message: "cache missing at https://secret.example/token"}},
		{Code: 0, Error: &CLIError{Code: "subscription_error", Message: "preview unavailable"}},
	} {
		runner := &featureRunner{results: map[string]Result{"subscription\x00preview\x00feed": result}}
		view, err := subscriptionDetailPage("feed").Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range view.Items {
			if item.Label == "Apply cached subscription" {
				t.Fatalf("apply offered after failed preview: %+v", result)
			}
		}
		if len(view.Items) != 3 || view.Items[2].Label != "Remove subscription and cache" {
			t.Fatalf("recovery actions missing: %+v", view.Items)
		}
		if !strings.Contains(view.Context, "Preview unavailable") || strings.Contains(view.Context, "secret.example") {
			t.Fatalf("preview failure or URL redaction missing: %q", view.Context)
		}
	}
}

func TestSubscriptionAddDoesNotPrintURL(t *testing.T) {
	ctx := context.Background()
	const url = "https://secret.example/feed?token=abc"
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00add\x00feed\x00" + url: {Data: map[string]any{"name": "feed"}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("feed\n"+url+"\n"), &out)
	if err := addSubscription(ctx, s); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), url) {
		t.Fatalf("URL leaked in menu output: %q", out.String())
	}
	want := []string{"subscription", "add", "feed", url}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("add args = %#v, want %#v", runner.calls[0], want)
	}
}

func TestSubscriptionUpdateRedactsURLInNodeChanges(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00update\x00feed": {Data: map[string]any{"name": "feed", "bytes": 42, "preview": map[string]any{"name": "feed", "added": []string{"trojan://private@secret.example:443"}}}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader(""), &out)
	if err := updateSubscription(context.Background(), s, "feed"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "secret.example") || !strings.Contains(out.String(), "[redacted URL]") || !strings.Contains(out.String(), "42 bytes") {
		t.Fatalf("update summary: %q", out.String())
	}
}
