package subscription

import (
	"context"
	"crypto/rand"
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
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/platform"
)

const maxSubscriptionSize = 8 << 20
const DefaultScheduleSeconds int64 = 3600
const MaxScheduleSeconds int64 = 31536000

type Entry struct {
	Name                       string    `json:"name"`
	KeyConfigured              bool      `json:"key_configured"`
	URL                        string    `json:"url"`
	Generation                 string    `json:"generation,omitempty"`
	HealthcheckURL             *string   `json:"healthcheck_url,omitempty"`
	HealthcheckIntervalSeconds *int      `json:"healthcheck_interval_seconds,omitempty"`
	ScheduleSeconds            int64     `json:"schedule_seconds"`
	NextDue                    time.Time `json:"next_due,omitempty"`
	LastFailureStage           string    `json:"last_failure_stage,omitempty"`
	LastFailureReason          string    `json:"last_failure_reason,omitempty"`
	PauseReason                string    `json:"pause_reason,omitempty"`
	CacheVersion               string    `json:"cache_version,omitempty"`
	AppliedCacheVersion        string    `json:"applied_cache_version,omitempty"`
	SourceHash                 string    `json:"source_hash,omitempty"`
	Profile                    string    `json:"profile,omitempty"`
	UpdatedAt                  time.Time `json:"updated_at,omitempty"`
	ExpiresAt                  time.Time `json:"expires_at,omitempty"`
	Upload                     int64     `json:"upload,omitempty"`
	Download                   int64     `json:"download,omitempty"`
	Total                      int64     `json:"total,omitempty"`
}

func (e Entry) MarshalJSON() ([]byte, error) {
	view := struct {
		Name                       string     `json:"name"`
		KeyConfigured              bool       `json:"key_configured"`
		HealthcheckURL             *string    `json:"healthcheck_url,omitempty"`
		HealthcheckIntervalSeconds *int       `json:"healthcheck_interval_seconds,omitempty"`
		ScheduleSeconds            int64      `json:"schedule_seconds"`
		NextDue                    *time.Time `json:"next_due,omitempty"`
		LastFailureStage           string     `json:"last_failure_stage,omitempty"`
		LastFailureReason          string     `json:"last_failure_reason,omitempty"`
		PauseReason                string     `json:"pause_reason,omitempty"`
		CacheVersion               string     `json:"cache_version,omitempty"`
		AppliedCacheVersion        string     `json:"applied_cache_version,omitempty"`
		SourceHash                 string     `json:"source_hash,omitempty"`
		Profile                    string     `json:"profile,omitempty"`
		UpdatedAt                  *time.Time `json:"updated_at,omitempty"`
		ExpiresAt                  *time.Time `json:"expires_at,omitempty"`
		Upload                     int64      `json:"upload,omitempty"`
		Download                   int64      `json:"download,omitempty"`
		Total                      int64      `json:"total,omitempty"`
	}{Name: e.Name, KeyConfigured: e.KeyConfigured, HealthcheckURL: publicHealthcheckURL(e.HealthcheckURL), HealthcheckIntervalSeconds: e.HealthcheckIntervalSeconds, ScheduleSeconds: e.ScheduleSeconds, LastFailureStage: e.LastFailureStage, LastFailureReason: e.LastFailureReason, PauseReason: e.PauseReason, CacheVersion: e.CacheVersion, AppliedCacheVersion: e.AppliedCacheVersion, SourceHash: e.SourceHash, Profile: e.Profile}
	if !e.NextDue.IsZero() {
		view.NextDue = &e.NextDue
	}
	if !e.UpdatedAt.IsZero() {
		view.UpdatedAt = &e.UpdatedAt
	}
	if !e.ExpiresAt.IsZero() {
		view.ExpiresAt = &e.ExpiresAt
	}
	view.Upload, view.Download, view.Total = e.Upload, e.Download, e.Total
	return json.Marshal(view)
}

type RefreshResult struct {
	Name      string     `json:"name"`
	Bytes     int        `json:"bytes"`
	UpdatedAt time.Time  `json:"updated_at"`
	Preview   *Preview   `json:"preview,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Upload    int64      `json:"upload,omitempty"`
	Download  int64      `json:"download,omitempty"`
	Total     int64      `json:"total,omitempty"`
}

type Store struct {
	ConfigDir string
	CacheDir  string
	Client    *http.Client
	Binary    string
	// CommitUnderConfigLock wraps the short cache/index commit. The CLI supplies
	// config.lock here; fetching, decoding, and validation run outside it.
	CommitUnderConfigLock func(func() error) error
}

// subscriptionHeaders identify Nagi as a mihomo/Clash Meta client. XBoard
// uses the client hint to select its native YAML subscription format instead
// of returning a generic V2Ray link list.
const subscriptionUserAgent = "clash.meta"

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

type storedEntry struct {
	Name                       string    `json:"name"`
	URL                        string    `json:"url"`
	Generation                 string    `json:"generation,omitempty"`
	HealthcheckURL             *string   `json:"healthcheck_url,omitempty"`
	HealthcheckIntervalSeconds *int      `json:"healthcheck_interval_seconds,omitempty"`
	ScheduleSeconds            int64     `json:"schedule_seconds,omitempty"`
	NextDue                    time.Time `json:"next_due,omitempty"`
	LastFailureStage           string    `json:"last_failure_stage,omitempty"`
	LastFailureReason          string    `json:"last_failure_reason,omitempty"`
	PauseReason                string    `json:"pause_reason,omitempty"`
	CacheVersion               string    `json:"cache_version,omitempty"`
	AppliedCacheVersion        string    `json:"applied_cache_version,omitempty"`
	SourceHash                 string    `json:"source_hash,omitempty"`
	Profile                    string    `json:"profile,omitempty"`
	UpdatedAt                  time.Time `json:"updated_at,omitempty"`
	ExpiresAt                  time.Time `json:"expires_at,omitempty"`
	Upload                     int64     `json:"upload,omitempty"`
	Download                   int64     `json:"download,omitempty"`
	Total                      int64     `json:"total,omitempty"`
}

func (s *Store) read() ([]Entry, error) {
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Subscriptions []storedEntry `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.New("invalid subscriptions index")
	}
	seen := map[string]bool{}
	result := make([]Entry, 0, len(file.Subscriptions))
	for _, stored := range file.Subscriptions {
		e := Entry{Name: stored.Name, URL: stored.URL, Generation: stored.Generation, HealthcheckURL: stored.HealthcheckURL, HealthcheckIntervalSeconds: stored.HealthcheckIntervalSeconds, ScheduleSeconds: stored.ScheduleSeconds, NextDue: stored.NextDue, LastFailureStage: stored.LastFailureStage, LastFailureReason: stored.LastFailureReason, PauseReason: stored.PauseReason, CacheVersion: stored.CacheVersion, AppliedCacheVersion: stored.AppliedCacheVersion, SourceHash: stored.SourceHash, Profile: stored.Profile, UpdatedAt: stored.UpdatedAt, ExpiresAt: stored.ExpiresAt, Upload: stored.Upload, Download: stored.Download, Total: stored.Total}
		if e.PauseReason == "" && e.LastFailureStage == "paused" {
			e.PauseReason = e.LastFailureReason // Migrate indices written before pause_reason existed.
		}
		if !validName(e.Name) || !validURL(e.URL) || seen[e.Name] || (e.HealthcheckURL != nil && !ValidHealthcheckURL(*e.HealthcheckURL)) || (e.HealthcheckIntervalSeconds != nil && (*e.HealthcheckIntervalSeconds < 1 || *e.HealthcheckIntervalSeconds > 31536000)) || e.ScheduleSeconds < 0 || e.ScheduleSeconds > MaxScheduleSeconds || (e.Profile != "" && !validName(e.Profile)) {
			return nil, errors.New("invalid subscriptions index")
		}
		seen[e.Name] = true
		result = append(result, e)
	}
	return result, nil
}

// withIndexLock uses a persistent inode outside ConfigDir, which a failed
// applyTransaction can restore wholesale. Never remove or replace this file.
func (s *Store) withIndexLock(fn func() error) error {
	if err := os.MkdirAll(s.CacheDir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.CacheDir, ".index.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := platform.Lock(f); err != nil {
		return err
	}
	defer platform.Unlock(f)
	return fn()
}

func (s *Store) mutate(fn func([]Entry) ([]Entry, error)) error {
	return s.withIndexLock(func() error {
		entries, err := s.read()
		if err != nil {
			return err
		}
		entries, err = fn(entries)
		if err != nil {
			return err
		}
		return s.write(entries)
	})
}

func (s *Store) Find(name string) (Entry, error) {
	var found Entry
	err := s.withIndexLock(func() error {
		entries, err := s.read()
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Name == name {
				found = e
				found.URL = ""
				found.KeyConfigured = s.hasKey(name)
				return nil
			}
		}
		return errors.New("subscription not found")
	})
	return found, err
}

func (s *Store) Due(now time.Time) ([]Entry, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	due := make([]Entry, 0)
	for _, e := range entries {
		if e.ScheduleSeconds > 0 && !e.NextDue.After(now) {
			due = append(due, e)
		}
	}
	return due, nil
}

func (s *Store) List() ([]Entry, error) {
	entries, err := s.read()
	if err != nil {
		return nil, err
	}
	// URLs are deliberately omitted from ordinary output, including JSON serialization.
	for i := range entries {
		entries[i].URL = ""
		entries[i].KeyConfigured = s.hasKey(entries[i].Name)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (s *Store) Add(name, rawURL string) error {
	return s.AddWithOptions(name, rawURL, AddOptions{})
}

// AddOptions are saved atomically before any subscription download or apply.
type AddOptions struct {
	ScheduleSeconds *int64
	HealthcheckURL  *string
}

func (s *Store) AddWithOptions(name, rawURL string, options AddOptions) error {
	seconds := DefaultScheduleSeconds
	if options.ScheduleSeconds != nil {
		seconds = *options.ScheduleSeconds
	}
	if seconds < 0 || seconds > MaxScheduleSeconds {
		return errors.New("schedule seconds must be 0 (off) or 1..31536000")
	}
	if options.HealthcheckURL != nil && !ValidHealthcheckURL(*options.HealthcheckURL) {
		return errors.New("test URL must be absolute HTTP(S) without user information or fragment")
	}
	if !validName(name) || !validURL(rawURL) {
		return errors.New("invalid subscription name or URL")
	}
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		return err
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for _, e := range entries {
			if e.Name == name {
				return nil, errors.New("subscription already exists")
			}
		}
		var nextDue time.Time
		if seconds > 0 {
			nextDue = time.Now().UTC().Add(time.Duration(seconds) * time.Second)
		}
		return append(entries, Entry{Name: name, URL: rawURL, Generation: hex.EncodeToString(generation[:]), HealthcheckURL: options.HealthcheckURL, ScheduleSeconds: seconds, NextDue: nextDue}), nil
	})
}

func (s *Store) Remove(name string) error {
	if !validName(name) {
		return errors.New("invalid subscription name")
	}
	return s.withIndexLock(func() error {
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
				cacheErr := os.Remove(s.cachePath(name))
				if errors.Is(cacheErr, fs.ErrNotExist) {
					cacheErr = nil
				}
				return errors.Join(cacheErr, s.ClearKey(name))
			}
		}
		return errors.New("subscription not found")
	})
}

func (s *Store) Schedule(name string, seconds int64) error {
	if !validName(name) || seconds < 0 || seconds > MaxScheduleSeconds {
		return errors.New("schedule seconds must be 0 (off) or 1..31536000")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				entries[i].ScheduleSeconds = seconds
				if seconds == 0 {
					entries[i].NextDue = time.Time{}
				} else {
					entries[i].NextDue = time.Now().UTC().Add(time.Duration(seconds) * time.Second)
				}
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

// RecordApplySuccess is called inside the outer configuration transaction, after
// profile activation and choice clearing have succeeded. It records exactly the
// cache version used by the caller, even if a newer download exists.
func (s *Store) RecordApplySuccess(name, profileName, cacheVersion string, source []byte) error {
	if !validName(profileName) || cacheVersion == "" {
		return errors.New("invalid apply metadata")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				for j := range entries {
					if j != i && entries[j].Profile == profileName {
						entries[j].Profile = ""
						entries[j].AppliedCacheVersion = ""
						entries[j].SourceHash = ""
					}
				}
				entries[i].Profile = profileName
				entries[i].AppliedCacheVersion = cacheVersion
				entries[i].SourceHash = Hash(source)
				entries[i].LastFailureStage = ""
				entries[i].LastFailureReason = ""
				entries[i].PauseReason = ""
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

func (s *Store) DetachProfile(profileName string) error {
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Profile == profileName {
				entries[i].Profile = ""
				entries[i].AppliedCacheVersion = ""
				entries[i].SourceHash = ""
			}
		}
		return entries, nil
	})
}

// RecordFailure accepts only a fixed stage and never persists a raw external
// error; reasons are stable, redacted operational codes.
func (s *Store) RecordFailure(name, stage, reason string, nextDue time.Time) error {
	allowedStage := map[string]bool{"fetch": true, "decrypt": true, "validate": true, "apply": true, "paused": true}
	allowedReason := map[string]bool{"network_error": true, "invalid_content": true, "identity_unavailable": true, "activation_failed": true, "engine_unavailable": true, "profile_changed": true, "schedule_off": true, "source_unreadable": true, "dns_target_missing": true, "custom_rule_conflict": true, "selection_conflict": true}
	if !allowedStage[stage] || !allowedReason[reason] {
		return errors.New("invalid failure metadata")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				entries[i].LastFailureStage = stage
				entries[i].LastFailureReason = reason
				if stage == "paused" {
					entries[i].PauseReason = reason
				}
				entries[i].NextDue = nextDue.UTC()
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

// RecordPause blocks automatic activation until explicit apply, independently
// of the most recent download failure and its retry schedule.
func (s *Store) RecordPause(name, reason string) error {
	if reason != "profile_changed" && reason != "source_unreadable" {
		return errors.New("invalid pause reason")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				entries[i].PauseReason = reason
				if entries[i].LastFailureStage != "fetch" && entries[i].LastFailureStage != "decrypt" && entries[i].LastFailureStage != "validate" {
					entries[i].LastFailureStage = "paused"
					entries[i].LastFailureReason = reason
				}
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

// RecordDownloadSuccess advances the refresh schedule without clearing a
// persistent automatic-apply pause.
func (s *Store) RecordDownloadSuccess(name string, nextDue time.Time) error {
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				entries[i].NextDue = nextDue.UTC()
				if entries[i].PauseReason != "" {
					entries[i].LastFailureStage = "paused"
					entries[i].LastFailureReason = entries[i].PauseReason
				} else {
					entries[i].LastFailureStage = ""
					entries[i].LastFailureReason = ""
				}
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

func (s *Store) RecordSuccess(name string, nextDue time.Time) error {
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				// A successful fetch cannot resume an externally edited or
				// unreadable source. Explicit apply owns that recovery.
				entries[i].NextDue = nextDue.UTC()
				if entries[i].PauseReason != "" {
					entries[i].LastFailureStage = "paused"
					entries[i].LastFailureReason = entries[i].PauseReason
				} else {
					entries[i].LastFailureStage = ""
					entries[i].LastFailureReason = ""
				}
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

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
	var original Entry
	found := false
	for _, e := range entries {
		if e.Name == name {
			original = e
			found = true
			break
		}
	}
	if !found {
		return zero, errors.New("subscription not found")
	}
	keyVersion, err := s.keyVersion(name)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, original.URL, nil)
	if err != nil {
		return zero, errors.New("invalid subscription request")
	}
	req.Header.Set("User-Agent", subscriptionUserAgent)
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain;q=0.9, */*;q=0.8")
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
	if strings.HasPrefix(string(data), "-----BEGIN AGE ENCRYPTED FILE-----") {
		data, err = s.decrypt(ctx, name, data)
		if err != nil {
			return zero, err
		}
	}
	incoming, format, err := Document(data)
	if err != nil {
		return zero, err
	}
	incoming, err = NormalizeChecked(incoming)
	if err != nil {
		return zero, err
	}
	expiry, upload, download, total := parseUserInfo(resp.Header.Get("Subscription-Userinfo"))
	var result RefreshResult
	commit := func() error {
		return s.withIndexLock(func() error {
			latest, err := s.read()
			if err != nil {
				return err
			}
			for i := range latest {
				if latest[i].Name == name {
					currentKeyVersion, err := s.keyVersion(name)
					if err != nil {
						return err
					}
					if latest[i].URL != original.URL || latest[i].Generation != original.Generation || currentKeyVersion != keyVersion {
						return errors.New("subscription changed during download; retry update")
					}
					cachePath := s.cachePath(name)
					old, oldErr := os.ReadFile(cachePath)
					if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
						return oldErr
					}
					var previous map[string]any
					if oldErr == nil {
						previous, _, _ = Document(old)
						if previous != nil {
							previous, _ = NormalizeChecked(previous)
						}
					}
					preview := Compare(name, format, incoming, previous)
					if err := writeReplace(cachePath, data); err != nil {
						return err
					}
					latest[i].UpdatedAt = time.Now().UTC()
					latest[i].ExpiresAt, latest[i].Upload, latest[i].Download, latest[i].Total = expiry, upload, download, total
					latest[i].CacheVersion = Hash(data)
					if err := s.write(latest); err != nil {
						var restoreErr error
						if errors.Is(oldErr, fs.ErrNotExist) {
							restoreErr = os.Remove(cachePath)
							if errors.Is(restoreErr, fs.ErrNotExist) {
								restoreErr = nil
							}
						} else {
							restoreErr = writeReplace(cachePath, old)
						}
						return errors.Join(err, restoreErr)
					}
					result = RefreshResult{Name: name, Bytes: len(data), UpdatedAt: latest[i].UpdatedAt, Preview: &preview, Upload: upload, Download: download, Total: total}
					if !expiry.IsZero() {
						result.ExpiresAt = &expiry
					}
					return nil
				}
			}
			return errors.New("subscription removed during download; retry update")
		})
	}
	if s.CommitUnderConfigLock != nil {
		err = s.CommitUnderConfigLock(commit)
	} else {
		err = commit()
	}
	if err != nil {
		return zero, err
	}
	return result, nil
}

// keyVersion lets a refresh reject plaintext produced with an identity that
// changed while the HTTP request or decrypt command was in flight.
func (s *Store) keyVersion(name string) (string, error) {
	path := s.keyPath(name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxIdentitySize {
		return "", errors.New("subscription identity is unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read subscription identity")
	}
	return Hash(data), nil
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

func parseUserInfo(header string) (time.Time, int64, int64, int64) {
	var expiry time.Time
	var upload, download, total int64
	for _, field := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			continue
		}
		number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || number < 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "expire":
			if number > 0 {
				expiry = time.Unix(number, 0).UTC()
			}
		case "upload":
			upload = number
		case "download":
			download = number
		case "total":
			total = number
		}
	}
	return expiry, upload, download, total
}

func (s *Store) write(entries []Entry) error {
	file := struct {
		Subscriptions []storedEntry `json:"subscriptions"`
	}{Subscriptions: make([]storedEntry, 0, len(entries))}
	for _, e := range entries {
		file.Subscriptions = append(file.Subscriptions, storedEntry{Name: e.Name, URL: e.URL, Generation: e.Generation, HealthcheckURL: e.HealthcheckURL, HealthcheckIntervalSeconds: e.HealthcheckIntervalSeconds, ScheduleSeconds: e.ScheduleSeconds, NextDue: e.NextDue, LastFailureStage: e.LastFailureStage, LastFailureReason: e.LastFailureReason, PauseReason: e.PauseReason, CacheVersion: e.CacheVersion, AppliedCacheVersion: e.AppliedCacheVersion, SourceHash: e.SourceHash, Profile: e.Profile, UpdatedAt: e.UpdatedAt, ExpiresAt: e.ExpiresAt, Upload: e.Upload, Download: e.Download, Total: e.Total})
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

// SetHealthcheckInterval saves intent independently of downloads and profile activation.
// The caller owns config.lock and the outer configuration transaction.
func (s *Store) SetHealthcheckInterval(name string, seconds *int) error {
	if seconds != nil && (*seconds < 1 || *seconds > 31536000) {
		return errors.New("SECONDS must be an integer from 1 to 31536000")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				entries[i].HealthcheckIntervalSeconds = seconds
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}

// StageProfileAssociation makes saved policy available to candidate composition.
// Call only inside a configuration transaction; RecordApplySuccess commits the
// applied version and source hash after activation succeeds.
func (s *Store) StageProfileAssociation(name, profileName string) error {
	if !validName(profileName) {
		return errors.New("invalid profile name")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		found := -1
		for i := range entries {
			if entries[i].Name == name {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, errors.New("subscription not found")
		}
		for i := range entries {
			if i != found && entries[i].Profile == profileName {
				entries[i].Profile = ""
				entries[i].AppliedCacheVersion = ""
				entries[i].SourceHash = ""
			}
		}
		entries[found].Profile = profileName
		return entries, nil
	})
}

// ValidHealthcheckURL validates a native mihomo test URL without revealing it.
func ValidHealthcheckURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(raw, "\r\n\t ")
}

func publicHealthcheckURL(raw *string) *string {
	if raw == nil {
		return nil
	}
	u, err := url.Parse(*raw)
	if err != nil {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	return &origin
}

// SetHealthcheckURL saves persistent URL intent; nil follows source settings.
// The caller owns config.lock and the enclosing configuration transaction.
func (s *Store) SetHealthcheckURL(name string, value *string) error {
	if value != nil && !ValidHealthcheckURL(*value) {
		return errors.New("test URL must be absolute HTTP(S) without user information or fragment")
	}
	return s.mutate(func(entries []Entry) ([]Entry, error) {
		for i := range entries {
			if entries[i].Name == name {
				entries[i].HealthcheckURL = value
				return entries, nil
			}
		}
		return nil, errors.New("subscription not found")
	})
}
