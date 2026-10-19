package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Invalid arguments retain their name so construction cannot turn a malformed
// descriptor into an apparently valid zero-argument message.
type invalidArgument struct{}

func matchesType(value any, kind string) bool {
	if !scalar(value) {
		return false
	}
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		switch value.(type) {
		case string, bool:
			return false
		}
		return true
	}
	return false
}

func loadArgumentSchemas() map[string]map[string]string {
	paths, err := files.ReadDir("catalogs")
	if err != nil {
		panic(err)
	}
	schemas := map[string]map[string]string{}
	for _, path := range paths {
		if path.IsDir() || !strings.HasSuffix(path.Name(), ".arguments.json") {
			continue
		}
		data, err := files.ReadFile("catalogs/" + path.Name())
		if err != nil {
			panic(err)
		}
		if err = uniqueJSON(data); err != nil {
			panic(err)
		}
		var module map[string]map[string]string
		if err = json.Unmarshal(data, &module); err != nil {
			panic(err)
		}
		for id, signature := range module {
			if _, exists := schemas[id]; exists {
				panic("duplicate argument schema: " + id)
			}
			schemas[id] = signature
		}
	}
	return schemas
}

// Reject duplicate keys at every object depth instead of silently accepting the
// last translation or argument declaration.
func uniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		keys := map[string]bool{}
		for decoder.More() {
			if delimiter == '{' {
				token, err = decoder.Token()
				if err != nil {
					return err
				}
				key := token.(string)
				if keys[key] {
					return fmt.Errorf("duplicate catalog key: %s", key)
				}
				keys[key] = true
			}
			if err = value(); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing catalog JSON: %v", err)
	}
	return nil
}

func validateCatalogs(all map[Language]map[string]entry, schemas map[string]map[string]string) error {
	for _, language := range []Language{English, SimplifiedChinese} {
		if len(all[language]) != len(schemas) {
			return fmt.Errorf("%s catalog/schema ID mismatch", language)
		}
		for id, signature := range schemas {
			if id == "" || signature == nil {
				return fmt.Errorf("invalid message signature: %s", id)
			}
			item, exists := all[language][id]
			if !exists {
				return fmt.Errorf("missing %s message %s", language, id)
			}
			texts := []string{item.literal}
			if item.literal == "" {
				if item.forms["other"] == "" {
					return fmt.Errorf("missing other plural: %s", id)
				}
				texts = nil
				for form, text := range item.forms {
					if form != "one" && form != "other" {
						return fmt.Errorf("unknown plural form %s: %s", form, id)
					}
					texts = append(texts, text)
				}
				if signature["count"] != "number" {
					return fmt.Errorf("plural count must be numeric: %s", id)
				}
			}
			for name, kind := range signature {
				if name == "" || (kind != "string" && kind != "number" && kind != "boolean") {
					return fmt.Errorf("invalid argument schema: %s", id)
				}
			}
			for _, text := range texts {
				found := map[string]bool{}
				for _, match := range placeholders.FindAllStringSubmatch(text, -1) {
					found[match[1]] = true
				}
				if len(found) != len(signature) {
					return fmt.Errorf("placeholder mismatch: %s %s", language, id)
				}
				for name := range found {
					if _, ok := signature[name]; !ok {
						return fmt.Errorf("unknown placeholder: %s %s", id, name)
					}
				}
			}
		}
	}
	return nil
}
