package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const maxSubscriptionSize = 8 << 20

type Entry struct {
	Name      string    `json:"name"`
	URL       string    `json:"-"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (e Entry) MarshalJSON() ([]byte, error) {
	view := struct {
		Name      string     `json:"name"`
		UpdatedAt *time.Time `json:"updated_at,omitempty"`
	}{Name: e.Name}
	if !e.UpdatedAt.IsZero() {
		view.UpdatedAt = &e.UpdatedAt
	}
	return json.Marshal(view)
}

type RefreshResult struct {
	Name      string    `json:"name"`
	Bytes     int       `json:"bytes"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct {
	ConfigDir string
	CacheDir  string
	Client    *http.Client
}

func NewStore(configDir, cacheDir string, client *http.Client) *Store {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Store{ConfigDir: configDir, CacheDir: cacheDir, Client: client}
}

func validName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

func (s *Store) indexPath() string { return filepath.Join(s.ConfigDir, "subscriptions.yaml") }

func (s *Store) read() ([]Entry, error) {
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Subscriptions []struct {
			Name      string    `json:"name"`
			URL       string    `json:"url"`
			UpdatedAt time.Time `json:"updated_at,omitempty"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.New("invalid subscriptions index")
	}
	seen := map[string]bool{}
	result := make([]Entry, 0, len(file.Subscriptions))
	for _, e := range file.Subscriptions {
		if !validName(e.Name) || !validURL(e.URL) || seen[e.Name] {
			return nil, errors.New("invalid subscriptions index")
		}
		seen[e.Name] = true
		result = append(result, Entry{Name: e.Name, URL: e.URL, UpdatedAt: e.UpdatedAt})
	}
	return result, nil
}

func (s *Store) List() ([]Entry, error) {
	entries, err := s.read()
	if err != nil {
		return nil, err
	}
	// URLs are deliberately omitted from ordinary output, including JSON serialization.
	for i := range entries {
		entries[i].URL = ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (s *Store) Add(name, rawURL string) error {
	if !validName(name) || !validURL(rawURL) {
		return errors.New("invalid subscription name or URL")
	}
	entries, err := s.read()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == name {
			return errors.New("subscription already exists")
		}
	}
	entries = append(entries, Entry{Name: name, URL: rawURL})
	return s.write(entries)
}

func (s *Store) Remove(name string) error {
	if !validName(name) {
		return errors.New("invalid subscription name")
	}
	entries, err := s.read()
	if err != nil {
		return err
	}
	for i, e := range entries {
		if e.Name == name {
			entries = append(entries[:i], entries[i+1:]...)
			if err := s.write(entries); err != nil {
				return err
			}
			err := os.Remove(s.cachePath(name))
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
	}
	return errors.New("subscription not found")
}

func (s *Store) cachePath(name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(s.CacheDir, hex.EncodeToString(sum[:])+".yaml")
}

func (s *Store) Cached(name string) ([]byte, error) {
	if !validName(name) {
		return nil, errors.New("invalid subscription name")
	}
	entries, err := s.read()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == name {
			return os.ReadFile(s.cachePath(name))
		}
	}
	return nil, errors.New("subscription not found")
}

func (s *Store) Refresh(ctx context.Context, name string) (RefreshResult, error) {
	var zero RefreshResult
	if !validName(name) {
		return zero, errors.New("invalid subscription name")
	}
	entries, err := s.read()
	if err != nil {
		return zero, err
	}
	for i := range entries {
		if entries[i].Name != name {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, entries[i].URL, nil)
		if err != nil {
			return zero, errors.New("invalid subscription request")
		}
		resp, err := s.Client.Do(req)
		if err != nil {
			return zero, fmt.Errorf("subscription fetch failed: %w", sanitizeError(err))
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return zero, fmt.Errorf("subscription fetch returned HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionSize+1))
		if err != nil {
			return zero, errors.New("subscription read failed")
		}
		if len(data) == 0 || len(data) > maxSubscriptionSize {
			return zero, errors.New("subscription content empty or too large")
		}
		if err := os.MkdirAll(s.CacheDir, 0700); err != nil {
			return zero, err
		}
		cachePath := s.cachePath(name)
		oldCache, oldErr := os.ReadFile(cachePath)
		if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
			return zero, oldErr
		}
		if err := writeReplace(cachePath, data); err != nil {
			return zero, err
		}
		entries[i].UpdatedAt = time.Now().UTC()
		if err := s.write(entries); err != nil {
			var restoreErr error
			if errors.Is(oldErr, fs.ErrNotExist) {
				restoreErr = os.Remove(cachePath)
				if errors.Is(restoreErr, fs.ErrNotExist) {
					restoreErr = nil
				}
			} else {
				restoreErr = writeReplace(cachePath, oldCache)
			}
			return zero, errors.Join(err, restoreErr)
		}
		return RefreshResult{Name: name, Bytes: len(data), UpdatedAt: entries[i].UpdatedAt}, nil
	}
	return zero, errors.New("subscription not found")
}

func sanitizeError(err error) error {
	// net/http errors often contain the full URL, including tokens in its query.
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("network error")
}

func (s *Store) write(entries []Entry) error {
	// JSON is a valid YAML 1.2 document, allowing strict standard-library parsing.
	file := struct {
		Subscriptions []struct {
			Name      string    `json:"name"`
			URL       string    `json:"url"`
			UpdatedAt time.Time `json:"updated_at,omitempty"`
		} `json:"subscriptions"`
	}{}
	for _, e := range entries {
		file.Subscriptions = append(file.Subscriptions, struct {
			Name      string    `json:"name"`
			URL       string    `json:"url"`
			UpdatedAt time.Time `json:"updated_at,omitempty"`
		}{e.Name, e.URL, e.UpdatedAt})
	}
	if file.Subscriptions == nil {
		file.Subscriptions = make([]struct {
			Name      string    `json:"name"`
			URL       string    `json:"url"`
			UpdatedAt time.Time `json:"updated_at,omitempty"`
		}, 0)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	path := s.indexPath()
	if old, err := os.ReadFile(path); err == nil {
		if err := writeReplace(path+".bak", old); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeReplace(path, append(data, '\n'))
}

func writeReplace(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("target is not a regular file")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nagi-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
