// Package rules stores user-owned routing rules outside subscription profiles.
package rules

import (
	"bytes"
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
	if e.Kind == "rules" {
		if e.Target != "" {
			return errors.New("rules format preserves each rule target and does not accept TARGET")
		}
		return validateItems(e.Items, "rule list")
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
		return validateItems(e.Items, "rule set")
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

// Import stores a bounded local or HTTP(S) snapshot. Full rules retain their
// native mihomo targets; provider formats require a shared target.
func (s *Store) Import(ctx context.Context, name, format, source, target string) error {
	if !validName(name) {
		return errors.New("rule name must use 1-128 ASCII letters, digits, _ or -")
	}
	if format != "rules" && format != "classical" && format != "domain" && format != "ipcidr" {
		return errors.New("rule import format must be rules, classical, domain, or ipcidr")
	}
	if format == "rules" && target != "" {
		return errors.New("rules format preserves each rule target and does not accept TARGET")
	}
	if format != "rules" && (target == "" || strings.ContainsAny(target, ",\n\r")) {
		return errors.New("rule set format requires a target without commas or newlines")
	}
	data, origin, err := s.readSource(ctx, source)
	if err != nil {
		return err
	}
	e := Entry{Name: name, Enabled: true, Kind: "set", Behavior: format, Source: origin, Target: target}
	if format == "rules" {
		e.Kind, e.Behavior = "rules", ""
		e.Items, err = ParseRules(data)
	} else {
		e.Items, err = ParseSet(data, format)
	}
	if err != nil {
		return fmt.Errorf("parse rule import: %w", err)
	}
	return s.Add(ctx, e)
}

const maxImportBytes = 8 << 20

func validateItems(items []string, label string) error {
	if len(items) == 0 {
		return fmt.Errorf("%s is empty", label)
	}
	if len(items) > 100000 {
		return fmt.Errorf("%s exceeds 100000 entries", label)
	}
	for _, item := range items {
		if strings.TrimSpace(item) == "" || strings.ContainsAny(item, "\n\r") {
			return fmt.Errorf("%s contains an invalid item", label)
		}
	}
	return nil
}

// ParseRules accepts only a YAML mapping's rules list or a YAML string list.
// Mihomo validates rule syntax and semantics when the snapshot is activated.
func ParseRules(data []byte) ([]string, error) {
	if len(data) == 0 || len(data) > maxImportBytes {
		return nil, errors.New("rule import must contain 1 byte to 8 MiB")
	}
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("rule import must contain exactly one YAML document")
	}
	if len(doc.Content) != 1 {
		return nil, errors.New("rules must be a YAML rules mapping or string list")
	}
	node := doc.Content[0]
	if node.Kind == yaml.MappingNode {
		var rules *yaml.Node
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "rules" {
				if rules != nil {
					return nil, errors.New("duplicate rules field")
				}
				rules = node.Content[i+1]
			}
		}
		node = rules
	}
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, errors.New("rules must be a YAML rules mapping or string list")
	}
	items := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			return nil, errors.New("rules must contain only strings")
		}
		items = append(items, item.Value)
	}
	if err := validateItems(items, "rule list"); err != nil {
		return nil, err
	}
	return items, nil
}

func readSnapshot(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxImportBytes {
		return nil, errors.New("rule import must contain 1 byte to 8 MiB")
	}
	return data, nil
}

func (s *Store) readSource(ctx context.Context, source string) ([]byte, string, error) {
	if source == "" {
		return nil, "", errors.New("rule import source must be a file path or HTTP(S) URL")
	}
	u, err := url.Parse(source)
	// Paths (including paths containing colons) remain local unless URL syntax is used.
	remote := strings.Contains(source, "://") || strings.HasPrefix(strings.ToLower(source), "http:") || strings.HasPrefix(strings.ToLower(source), "https:")
	if !remote {
		f, err := os.Open(source)
		if err != nil {
			return nil, "", fmt.Errorf("read rule import file: %w", err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return nil, "", fmt.Errorf("read rule import file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, "", errors.New("rule import source must be a regular file")
		}
		data, err := readSnapshot(f)
		if err != nil {
			return nil, "", fmt.Errorf("read rule import file: %w", err)
		}
		return data, "local:" + filepath.Base(source), nil
	}
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, "", errors.New("rule import URL must be absolute HTTP(S) without userinfo")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", fmt.Errorf("download rule import: %w", err)
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download rule import: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download rule import: HTTP %d", resp.StatusCode)
	}
	data, err := readSnapshot(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("download rule import: %w", err)
	}
	return data, "remote:" + u.Host, nil
}
