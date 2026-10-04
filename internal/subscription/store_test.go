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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
