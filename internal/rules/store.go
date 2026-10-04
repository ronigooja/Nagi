// Package rules stores user-owned routing rules outside subscription profiles.
package rules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Entry struct {
	Name     string   `yaml:"name" json:"name"`
	Enabled  bool     `yaml:"enabled" json:"enabled"`
	Kind     string   `yaml:"kind" json:"kind"`
	Type     string   `yaml:"type,omitempty" json:"type,omitempty"`
	Payload  string   `yaml:"payload,omitempty" json:"payload,omitempty"`
	Target   string   `yaml:"target" json:"target"`
	Behavior string   `yaml:"behavior,omitempty" json:"behavior,omitempty"`
	Items    []string `yaml:"items,omitempty" json:"items,omitempty"`
	Source   string   `yaml:"source,omitempty" json:"source,omitempty"`
}
type Document struct {
	Entries []Entry `yaml:"entries" json:"entries"`
}
type Store struct {
	Dir   string
	Check func(context.Context) error
	HTTP  *http.Client
}

func New(dir string, check func(context.Context) error) *Store {
	return &Store{Dir: dir, Check: check, HTTP: &http.Client{Timeout: 20 * time.Second}}
}
func Path(dir string) string { return filepath.Join(dir, "rules.yaml") }
func Exists(dir string) bool { _, err := os.Stat(Path(dir)); return err == nil }

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validName(name string) bool { return namePattern.MatchString(name) }
func (s *Store) List() (Document, error) {
	var doc Document
	info, err := os.Lstat(Path(s.Dir))
	if errors.Is(err, os.ErrNotExist) {
		return Document{Entries: []Entry{}}, nil
	}
	if err != nil {
		return doc, err
	}
	if !info.Mode().IsRegular() {
		return doc, errors.New("rules file is not regular")
	}
	data, err := os.ReadFile(Path(s.Dir))
	if err != nil {
		return doc, err
	}
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("parse rules file: %w", err)
	}
	if doc.Entries == nil {
		doc.Entries = []Entry{}
	}
	return doc, nil
}
func (s *Store) Save(ctx context.Context, doc Document) error {
	old, oldErr := os.ReadFile(Path(s.Dir))
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return oldErr
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	if err = write(Path(s.Dir), data); err != nil {
		return err
	}
	if s.Check != nil {
		if err = s.Check(ctx); err != nil {
			if errors.Is(oldErr, os.ErrNotExist) {
				return errors.Join(err, os.Remove(Path(s.Dir)))
			}
			return errors.Join(err, write(Path(s.Dir), old))
		}
	}
	return nil
}
func write(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".nagi-rules-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func Validate(e Entry) error {
	if !validName(e.Name) {
		return errors.New("rule name must use 1-128 ASCII letters, digits, _ or -")
	}
	if e.Target == "" || strings.ContainsAny(e.Target, ",\n\r") {
		return errors.New("target must be DIRECT, REJECT, or a proxy group name without commas")
	}
	if e.Kind == "rule" {
		types := map[string]bool{"DOMAIN": true, "DOMAIN-SUFFIX": true, "DOMAIN-KEYWORD": true, "DOMAIN-REGEX": true, "IP-CIDR": true, "IP-CIDR6": true, "GEOIP": true, "GEOSITE": true, "PROCESS-NAME": true, "MATCH": true}
		if !types[e.Type] {
			return errors.New("unsupported rule type")
		}
		if e.Type != "MATCH" && (e.Payload == "" || strings.ContainsAny(e.Payload, ",\n\r")) {
			return errors.New("payload must be nonempty and contain no comma or newline")
		}
		if e.Type == "MATCH" && e.Payload != "" {
			return errors.New("MATCH has no payload")
		}
		return nil
	}
	if e.Kind == "set" {
		if e.Behavior != "classical" && e.Behavior != "domain" && e.Behavior != "ipcidr" {
			return errors.New("rule set behavior must be classical, domain, or ipcidr")
		}
		if len(e.Items) == 0 {
			return errors.New("rule set is empty")
		}
		if len(e.Items) > 100000 {
			return errors.New("rule set exceeds 100000 entries")
		}
		for _, item := range e.Items {
			if item == "" || strings.ContainsAny(item, "\n\r") {
				return errors.New("rule set contains an invalid item")
			}
		}
		return nil
	}
	return errors.New("unknown rule kind")
}
func (s *Store) Add(ctx context.Context, e Entry) error {
	if err := Validate(e); err != nil {
		return err
	}
	doc, err := s.List()
	if err != nil {
		return err
	}
	for _, old := range doc.Entries {
		if old.Name == e.Name {
			return fmt.Errorf("rule %s already exists", e.Name)
		}
	}
	doc.Entries = append(doc.Entries, e)
	return s.Save(ctx, doc)
}
func (s *Store) Remove(ctx context.Context, name string) error {
	doc, err := s.List()
	if err != nil {
		return err
	}
	for i, e := range doc.Entries {
		if e.Name == name {
			doc.Entries = append(doc.Entries[:i], doc.Entries[i+1:]...)
			return s.Save(ctx, doc)
		}
	}
	return fmt.Errorf("rule %s not found; run `nagi rules custom`", name)
}
func (s *Store) SetEnabled(ctx context.Context, name string, enabled bool) error {
	doc, err := s.List()
	if err != nil {
		return err
	}
	for i := range doc.Entries {
		if doc.Entries[i].Name == name {
			doc.Entries[i].Enabled = enabled
			return s.Save(ctx, doc)
		}
	}
	return fmt.Errorf("rule %s not found; run `nagi rules custom`", name)
}
func ParseSet(data []byte, behavior string) ([]string, error) {
	if len(data) == 0 || len(data) > 8<<20 {
		return nil, errors.New("rule set must contain 1 byte to 8 MiB")
	}
	var obj struct {
		Payload []string `yaml:"payload"`
	}
	if err := yaml.Unmarshal(data, &obj); err == nil && len(obj.Payload) > 0 {
		return obj.Payload, nil
	}
	var items []string
	if err := yaml.Unmarshal(data, &items); err == nil && len(items) > 0 {
		return items, nil
	}
	if behavior != "classical" {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				items = append(items, line)
			}
		}
		if len(items) > 0 {
			return items, nil
		}
	}
	return nil, errors.New("rule set must be YAML payload/list (domain and ipcidr also accept text lines)")
}
func (s *Store) ImportLocal(ctx context.Context, name, behavior, path, target string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	items, err := ParseSet(data, behavior)
	if err != nil {
		return err
	}
	return s.Add(ctx, Entry{Name: name, Enabled: true, Kind: "set", Behavior: behavior, Items: items, Source: "local:" + filepath.Base(path), Target: target})
}
func (s *Store) ImportRemote(ctx context.Context, name, behavior, rawURL, target string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("remote rule set URL must be absolute HTTP(S) without userinfo")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download rule set: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download rule set: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return err
	}
	items, err := ParseSet(data, behavior)
	if err != nil {
		return err
	}
	return s.Add(ctx, Entry{Name: name, Enabled: true, Kind: "set", Behavior: behavior, Items: items, Source: "remote:" + u.Host, Target: target})
}
