package subscription

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshCachesAndHidesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		return nil, errors.New("token=private")
	})})
	if err := s.Add("work", "https://example.invalid/sub?token=private"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Refresh(context.Background(), "work")
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe error: %v", err)
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
