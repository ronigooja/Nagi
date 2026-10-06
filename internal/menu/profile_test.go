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
		"subscription\x00preview\x00feed": {Data: map[string]any{"name": "feed", "added": []string{"Node A", "https://secret.example/token"}, "removed": []string{"Old"}, "changed": []string{"Changed"}}},
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
