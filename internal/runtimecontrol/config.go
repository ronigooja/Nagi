package runtimecontrol

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func ValidMode(mode string) bool { return mode == "rule" || mode == "global" || mode == "direct" }
func Mode(data []byte) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", errors.New("selected profile is invalid YAML")
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return "", errors.New("selected profile must be a YAML mapping")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "mode" {
			continue
		}
		mode := strings.ToLower(strings.Trim(strings.TrimSpace(strings.SplitN(value, "#", 2)[0]), "'\""))
		if !ValidMode(mode) {
			return "", fmt.Errorf("invalid mode in selected profile: %q", mode)
		}
		return mode, nil
	}
	return "rule", nil
}
func SetMode(data []byte, mode string) ([]byte, error) {
	if !ValidMode(mode) {
		return nil, errors.New("mode must be rule, global, or direct")
	}
	if _, err := Mode(data); err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		key, _, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "mode" {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "mode: " + mode
			found = true
		}
	}
	if !found {
		lines = append([]string{"mode: " + mode}, lines...)
	}
	return []byte(strings.Join(lines, "\n")), nil
}
