package command

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// profileDiff reports changed lines after trimming the shared prefix and suffix.
// It deliberately avoids quadratic work on large subscription files.
func profileDiff(before, after []byte) string {
	if string(before) == string(after) {
		return ""
	}
	a, b := strings.SplitAfter(string(before), "\n"), strings.SplitAfter(string(after), "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	var out strings.Builder
	out.WriteString("--- current\n+++ comparison\n")
	for _, line := range a[start:endA] {
		out.WriteByte('-')
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	for _, line := range b[start:endB] {
		out.WriteByte('+')
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// StructuralChange describes a semantic YAML path changed between profiles.
type StructuralChange struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

func structuralDiff(before, after []byte) ([]StructuralChange, []string, error) {
	var oldTree, newTree any
	if err := yaml.Unmarshal(before, &oldTree); err != nil {
		return nil, nil, fmt.Errorf("current profile is not valid YAML: %w", err)
	}
	if err := yaml.Unmarshal(after, &newTree); err != nil {
		return nil, nil, fmt.Errorf("comparison profile is not valid YAML: %w", err)
	}
	changes := []StructuralChange{}
	walkStructure("", oldTree, newTree, &changes)
	warnings := []string{}
	for _, change := range changes {
		switch {
		case change.Kind == "type_change":
			warnings = append(warnings, "YAML type changed at "+change.Path+"; verify the target profile accepts it.")
		case change.Path == "/rules" || strings.HasPrefix(change.Path, "/rules/"):
			warnings = append(warnings, "Rule order or contents changed; verify routing priority.")
		case change.Path == "/dns" || strings.HasPrefix(change.Path, "/dns/"):
			warnings = append(warnings, "DNS policy changed; verify encrypted routing and leak protection.")
		case change.Kind == "removed" && (change.Path == "/proxies" || change.Path == "/proxy-groups" || strings.HasPrefix(change.Path, "/proxies/") || strings.HasPrefix(change.Path, "/proxy-groups/")):
			warnings = append(warnings, "A proxy or group was removed; check selections and rule targets.")
		case change.Kind == "order_changed" && (change.Path == "/proxy-groups" || change.Path == "/proxies"):
			warnings = append(warnings, "Proxy or group order changed; check first-choice and display order.")
		case strings.HasPrefix(change.Path, "/external-controller") || change.Path == "/allow-lan" || change.Path == "/bind-address" || change.Path == "/secret" || change.Path == "/authentication" || change.Path == "/lan-allowed-ips":
			warnings = append(warnings, "Network exposure setting changed; check bind and authentication settings.")
		}
	}
	warnings = uniqueStrings(warnings)
	if _, err := json.Marshal(changes); err != nil {
		return nil, nil, fmt.Errorf("structural YAML values cannot be represented in JSON: %w", err)
	}
	return changes, warnings, nil
}
func walkStructure(path string, before, after any, out *[]StructuralChange) {
	if reflect.DeepEqual(before, after) {
		return
	}
	aMap, aOK := before.(map[string]any)
	bMap, bOK := after.(map[string]any)
	if aOK && bOK {
		keys := map[string]bool{}
		for k := range aMap {
			keys[k] = true
		}
		for k := range bMap {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			next := path + "/" + escapePointer(k)
			a, aExists := aMap[k]
			b, bExists := bMap[k]
			switch {
			case !aExists:
				*out = append(*out, StructuralChange{Path: next, Kind: "added", After: b})
			case !bExists:
				*out = append(*out, StructuralChange{Path: next, Kind: "removed", Before: a})
			default:
				walkStructure(next, a, b, out)
			}
		}
		return
	}
	aSeq, aOK := before.([]any)
	bSeq, bOK := after.([]any)
	if aOK && bOK {
		if namedSequence(aSeq) && namedSequence(bSeq) {
			aOrder, bOrder := namedOrder(aSeq), namedOrder(bSeq)
			if !reflect.DeepEqual(aOrder, bOrder) {
				*out = append(*out, StructuralChange{Path: path, Kind: "order_changed", Before: aOrder, After: bOrder})
			}
			aNames := namedValues(aSeq)
			bNames := namedValues(bSeq)
			keys := map[string]bool{}
			for k := range aNames {
				keys[k] = true
			}
			for k := range bNames {
				keys[k] = true
			}
			sorted := make([]string, 0, len(keys))
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				next := path + "/" + escapePointer(k)
				a, aExists := aNames[k]
				b, bExists := bNames[k]
				switch {
				case !aExists:
					*out = append(*out, StructuralChange{Path: next, Kind: "added", After: b})
				case !bExists:
					*out = append(*out, StructuralChange{Path: next, Kind: "removed", Before: a})
				default:
					walkStructure(next, a, b, out)
				}
			}
			return
		}
		max := len(aSeq)
		if len(bSeq) > max {
			max = len(bSeq)
		}
		for i := 0; i < max; i++ {
			next := fmt.Sprintf("%s/%d", path, i)
			switch {
			case i >= len(aSeq):
				*out = append(*out, StructuralChange{Path: next, Kind: "added", After: bSeq[i]})
			case i >= len(bSeq):
				*out = append(*out, StructuralChange{Path: next, Kind: "removed", Before: aSeq[i]})
			default:
				walkStructure(next, aSeq[i], bSeq[i], out)
			}
		}
		return
	}
	kind := "modified"
	if reflect.TypeOf(before) != reflect.TypeOf(after) {
		kind = "type_change"
	}
	*out = append(*out, StructuralChange{Path: path, Kind: kind, Before: before, After: after})
}
func namedSequence(items []any) bool {
	if len(items) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return false
		}
		name, ok := m["name"].(string)
		if !ok || name == "" || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
func namedValues(items []any) map[string]any {
	out := map[string]any{}
	for _, item := range items {
		m := item.(map[string]any)
		out[m["name"].(string)] = item
	}
	return out
}
func namedOrder(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(map[string]any)["name"].(string))
	}
	return out
}
func escapePointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func uniqueStrings(items []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
