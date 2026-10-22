package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshCachesAndHidesURL(t *testing.T) {
	var gotUserAgent, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.Write([]byte("proxies: []\n"))
	}))
	defer server.Close()
	dir := t.TempDir()
	s := NewStore(dir, filepath.Join(dir, "cache"), nil)
	secretURL := server.URL + "/?token=private"
	if err := s.Add("work", secretURL); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil || len(entries) != 1 || entries[0].URL != "" {
		t.Fatalf("unsafe list: %+v, %v", entries, err)
	}
	result, err := s.Refresh(context.Background(), "work")
	if err != nil || result.Bytes == 0 {
		t.Fatalf("refresh: %+v, %v", result, err)
	}
	if gotUserAgent != subscriptionUserAgent || !strings.Contains(gotAccept, "application/yaml") {
		t.Fatalf("subscription negotiation headers: user-agent=%q accept=%q", gotUserAgent, gotAccept)
	}
	cache, err := s.Cached("work")
	if err != nil || string(cache) != "proxies: []\n" {
		t.Fatalf("cache: %q, %v", cache, err)
	}
	if err := s.Remove("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.cachePath("work")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache remained: %v", err)
	}
}

func TestRefreshErrorDoesNotRevealURL(t *testing.T) {
	s := NewStore(t.TempDir(), t.TempDir(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: "https://user:pass@example.invalid/private-feed-token?jwt=secret", Err: errors.New("dial tcp: lookup example.invalid: no such host")}
	})})
	if err := s.Add("work", "https://example.invalid/sub?token=private"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Refresh(context.Background(), "work")
	if err == nil || strings.Contains(err.Error(), "private-feed-token") || strings.Contains(err.Error(), "jwt=secret") || strings.Contains(err.Error(), "user:pass") {
		t.Fatalf("unsafe error: %v", err)
	}
	if !strings.Contains(err.Error(), "lookup example.invalid") {
		t.Fatalf("useful fetch cause was lost: %v", err)
	}
}

func TestRefreshRestoresCacheWhenIndexWriteFails(t *testing.T) {
	for _, existingCache := range []bool{false, true} {
		name := "without previous cache"
		if existingCache {
			name = "with previous cache"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("new cache"))
			}))
			defer server.Close()
			dir := t.TempDir()
			s := NewStore(dir, filepath.Join(dir, "cache"), nil)
			if err := s.Add("work", server.URL); err != nil {
				t.Fatal(err)
			}
			cachePath := s.cachePath("work")
			if existingCache {
				if err := os.MkdirAll(s.CacheDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, []byte("old cache"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// The index remains readable, but its backup cannot be replaced.
			if err := os.Mkdir(s.indexPath()+".bak", 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Refresh(context.Background(), "work"); err == nil {
				t.Fatal("expected index write failure")
			}
			got, err := os.ReadFile(cachePath)
			if existingCache {
				if err != nil || string(got) != "old cache" {
					t.Fatalf("cache = %q, %v", got, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("new cache remains: %q, %v", got, err)
			}
			entries, err := s.List()
			if err != nil || len(entries) != 1 || !entries[0].UpdatedAt.IsZero() {
				t.Fatalf("index changed: %+v, %v", entries, err)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestScheduleDefaultsAndLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := s.Add("new", "https://example.com/x"); err != nil {
		t.Fatal(err)
	}
	e, err := s.Find("new")
	if err != nil || e.ScheduleSeconds != DefaultScheduleSeconds {
		t.Fatalf("new schedule: %+v %v", e, err)
	}
	old := `{"subscriptions":[{"name":"old","url":"https://example.com/x"}]}`
	if err := os.WriteFile(s.indexPath(), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	e, err = s.Find("old")
	if err != nil || e.ScheduleSeconds != 0 || !e.NextDue.IsZero() {
		t.Fatalf("legacy schedule: %+v %v", e, err)
	}
}

func TestVersionsRemainDistinctAndURLPrivate(t *testing.T) {
	s := NewStore(t.TempDir(), t.TempDir(), nil)
	if err := s.Add("x", "https://secret.example/token"); err != nil {
		t.Fatal(err)
	}
	data := []byte("proxies: []\n")
	if err := s.RecordApplySuccess("x", "x", "cache-one", data); err != nil {
		t.Fatal(err)
	}
	e, err := s.Find("x")
	if err != nil || e.AppliedCacheVersion != "cache-one" || e.SourceHash != Hash(data) || e.URL != "" {
		t.Fatalf("metadata: %+v %v", e, err)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "secret.example") || strings.Contains(string(b), "cache-one") == false {
		t.Fatalf("unsafe json: %s", b)
	}
}

func TestRefreshDoesNotOverwriteConcurrentSchedule(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer srv.Close()
	s := NewStore(t.TempDir(), t.TempDir(), nil)
	if err := s.Add("x", srv.URL); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Refresh(context.Background(), "x"); done <- err }()
	<-started
	if err := s.Schedule("x", 120); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e, err := s.Find("x")
	if err != nil || e.ScheduleSeconds != 120 {
		t.Fatalf("schedule overwritten: %+v %v", e, err)
	}
}
