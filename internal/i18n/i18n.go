// Package i18n renders application-owned messages without changing machine data.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

type Language string

const (
	English           Language = "en"
	SimplifiedChinese Language = "zh-Hans"
)

// Normalize accepts supported locale aliases, including POSIX encoding/modifier suffixes.
func Normalize(value string) (Language, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Split(strings.Split(value, ".")[0], "@")[0]
	value = strings.ReplaceAll(value, "_", "-")
	switch value {
	case "en", "c", "posix":
		return English, true
	case "zh", "zh-hans", "zh-cn", "zh-sg":
		return SimplifiedChinese, true
	}
	if strings.HasPrefix(value, "en-") {
		return English, true
	}
	return English, false
}

// Resolve chooses one source: an unsupported automatic locale falls back to English.
func Resolve(explicit string, getenv func(string) string) (Language, error) {
	if explicit != "" {
		if lang, ok := Normalize(explicit); ok {
			return lang, nil
		}
		return English, NewMessage("cli.language.invalid", map[string]any{"value": explicit}, "Unsupported language "+explicit+"; use en or zh-Hans.", nil)
	}
	for _, key := range []string{"NAGI_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := getenv(key); value != "" {
			lang, _ := Normalize(value)
			return lang, nil
		}
	}
	return English, nil
}

type Descriptor struct {
	ID   string         `json:"id"`
	Args map[string]any `json:"args,omitempty"`
}

// Message retains a stable English error string and its original cause. Callers
// must redact sensitive parameters before construction; output applies its own
// final redaction to parameters and fallback text as defense in depth.
type Message struct {
	descriptor Descriptor
	fallback   string
	cause      error
}

func NewMessage(id string, args map[string]any, fallback string, cause error) *Message {
	copied := make(map[string]any, len(args))
	for key, value := range args {
		if scalar(value) {
			copied[key] = value
		}
	}
	return &Message{Descriptor{id, copied}, fallback, cause}
}
func (m *Message) Error() string { return m.fallback }
func (m *Message) Unwrap() error { return m.cause }
func (m *Message) Descriptor() Descriptor {
	args := make(map[string]any, len(m.descriptor.Args))
	for k, v := range m.descriptor.Args {
		args[k] = v
	}
	return Descriptor{m.descriptor.ID, args}
}

// Context is an immutable rendering context, safe to copy across goroutines.
type Context struct{ language Language }

func New(language Language) Context {
	if language != SimplifiedChinese {
		language = English
	}
	return Context{language}
}
func (c Context) Language() Language {
	if c.language == "" {
		return English
	}
	return c.language
}
func (c Context) Render(message *Message) string {
	return c.RenderDescriptor(message.Descriptor(), message.Error())
}
func (c Context) RenderDescriptor(d Descriptor, fallback string) string {
	entry, ok := catalogs[c.Language()][d.ID]
	if !ok {
		return fallback
	}
	english, ok := catalogs[English][d.ID]
	if !ok {
		return fallback
	}
	template := english.text(d.Args)
	if template == "" {
		return fallback
	}
	expected := placeholders.FindAllStringSubmatch(template, -1)
	required := map[string]bool{}
	for _, match := range expected {
		required[match[1]] = true
	}
	if len(required) != len(d.Args) {
		return fallback
	}
	for key, value := range d.Args {
		if !required[key] || !scalar(value) || (d.ID == "cli.language.invalid" && key == "value" && !isString(value)) {
			return fallback
		}
	}
	localized := entry.text(d.Args)
	if localized == "" {
		return fallback
	}
	return placeholders.ReplaceAllStringFunc(localized, func(token string) string {
		value, ok := d.Args[token[1:len(token)-1]]
		if !ok {
			return token
		}
		return fmt.Sprint(value)
	})
}
func scalar(value any) bool {
	switch v := value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case float32:
		return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
	case json.Number:
		_, err := v.Float64()
		return err == nil
	}
	return false
}

type entry struct {
	literal string
	forms   map[string]string
}

func (e entry) text(args map[string]any) string {
	if e.literal != "" {
		return e.literal
	}
	if count, ok := args["count"]; ok && fmt.Sprint(count) == "1" && e.forms["one"] != "" {
		return e.forms["one"]
	}
	return e.forms["other"]
}

var placeholders = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]*)\}`)

//go:embed catalogs/*/*.json
var files embed.FS
var catalogs = loadCatalogs()

func loadCatalogs() map[Language]map[string]entry {
	result := map[Language]map[string]entry{}
	for _, language := range []Language{English, SimplifiedChinese} {
		items := map[string]entry{}
		paths, err := files.ReadDir("catalogs/" + string(language))
		if err != nil {
			panic(err)
		}
		for _, path := range paths {
			data, err := files.ReadFile("catalogs/" + string(language) + "/" + path.Name())
			if err != nil {
				panic(err)
			}
			var raw map[string]json.RawMessage
			if err = json.Unmarshal(data, &raw); err != nil {
				panic(err)
			}
			for key, value := range raw {
				if _, exists := items[key]; exists {
					panic("duplicate message ID: " + key)
				}
				var item entry
				if json.Unmarshal(value, &item.literal) != nil {
					if err = json.Unmarshal(value, &item.forms); err != nil {
						panic(err)
					}
				}
				items[key] = item
			}
		}

		result[language] = items
	}
	return result
}

func isString(value any) bool { _, ok := value.(string); return ok }
