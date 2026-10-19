package i18n

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSharedLocalizationContract(t *testing.T) {
	data, err := os.ReadFile("../../docs/design/localization-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		IDs map[string]struct {
			Args    map[string]string `json:"args"`
			English string            `json:"en"`
			Chinese string            `json:"zh-Hans"`
		} `json:"initial_ids"`
		Valid []struct {
			Descriptor Descriptor `json:"descriptor"`
		} `json:"valid_descriptors"`
		Invalid []struct {
			Descriptor *Descriptor `json:"descriptor"`
		} `json:"invalid_descriptors"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for id, spec := range fixture.IDs {
		if !reflect.DeepEqual(argumentSchemas[id], spec.Args) {
			t.Fatalf("signature drift: %s", id)
		}
		if catalogs[English][id].literal != spec.English || catalogs[SimplifiedChinese][id].literal != spec.Chinese {
			t.Fatalf("translation drift: %s", id)
		}
	}
	for _, item := range fixture.Valid {
		if New(SimplifiedChinese).RenderDescriptor(item.Descriptor, "ORIGINAL") == "ORIGINAL" {
			t.Fatalf("valid descriptor rejected: %+v", item)
		}
	}
	for _, item := range fixture.Invalid {
		if item.Descriptor != nil && New(SimplifiedChinese).RenderDescriptor(*item.Descriptor, "ORIGINAL") != "ORIGINAL" {
			t.Fatalf("invalid descriptor accepted: %+v", item)
		}
	}
}

func TestCatalogValidation(t *testing.T) {
	for _, raw := range []string{`{"id":"a","id":"b"}`, `{"id":{"other":"a","other":"b"}}`, `{} {}`} {
		if uniqueJSON([]byte(raw)) == nil {
			t.Fatalf("accepted duplicate/trailing JSON: %s", raw)
		}
	}
	for _, kind := range []string{"missing-language", "unknown-id", "placeholder", "plural-count"} {
		t.Run(kind, func(t *testing.T) {
			all := map[Language]map[string]entry{English: {"example": {literal: "{name}"}}, SimplifiedChinese: {"example": {literal: "{name}"}}}
			schemas := map[string]map[string]string{"example": {"name": "string"}}
			switch kind {
			case "missing-language":
				delete(all[SimplifiedChinese], "example")
			case "unknown-id":
				all[SimplifiedChinese]["extra"] = entry{literal: "extra"}
			case "placeholder":
				all[SimplifiedChinese]["example"] = entry{literal: "{wrong}"}
			case "plural-count":
				all[English]["example"] = entry{forms: map[string]string{"other": "{name}"}}
			}
			if validateCatalogs(all, schemas) == nil {
				t.Fatal("accepted inconsistent resources")
			}
		})
	}
	for _, value := range []any{nil, map[string]any{"nested": "value"}, []string{"value"}} {
		for _, id := range []string{"cli.language.invalid", "cli.language.missing"} {
			key := "value"
			if id == "cli.language.missing" {
				key = "extra"
			}
			message := NewMessage(id, map[string]any{key: value}, "ORIGINAL", nil)
			if got := New(SimplifiedChinese).Render(message); got != "ORIGINAL" {
				t.Fatalf("invalid argument translated: %s", got)
			}
		}
	}
}
