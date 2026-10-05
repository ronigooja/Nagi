// Package configpolicy provides a deliberately narrow policy for configuration
// that a root mihomo process could read. It is not a general mihomo validator.
package configpolicy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 8 << 20

var ErrUnsupported = errors.New("configuration is not supported for privileged mihomo")

// Validate accepts only a small, literal configuration subset. In particular,
// proxy nodes, providers, listeners, external controllers, file references,
// and arbitrary mihomo extensions are rejected. This policy cannot yet run a
// subscription-backed profile; callers must not relax it by filtering keys.
func Validate(data []byte) error {
	if len(data) == 0 || len(data) > maxConfigBytes {
		return fmt.Errorf("%w: configuration must be 1..%d bytes", ErrUnsupported, maxConfigBytes)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("%w: invalid YAML: %v", ErrUnsupported, err)
	}
	var next yaml.Node
	if err := decoder.Decode(&next); err != io.EOF {
		return fmt.Errorf("%w: multiple YAML documents are not allowed", ErrUnsupported)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%w: expected a YAML mapping", ErrUnsupported)
	}
	if err := checkNode(&document, 0); err != nil {
		return err
	}
	fields, err := mapping(document.Content[0])
	if err != nil {
		return err
	}
	for key, value := range fields {
		switch key {
		case "mode":
			if err := literal(value, "direct"); err != nil {
				return fieldError(key, err)
			}
		case "log-level":
			if err := literal(value, "silent", "error", "warning", "info"); err != nil {
				return fieldError(key, err)
			}
		case "ipv6":
			if err := boolean(value); err != nil {
				return fieldError(key, err)
			}
		case "tun":
			if err := validateTun(value); err != nil {
				return fieldError(key, err)
			}
		default:
			return fmt.Errorf("%w: key %q is outside the privileged allowlist", ErrUnsupported, key)
		}
	}
	if _, ok := fields["mode"]; !ok {
		return fmt.Errorf("%w: mode: direct is required", ErrUnsupported)
	}
	return nil
}

func validateTun(node *yaml.Node) error {
	fields, err := mapping(node)
	if err != nil {
		return err
	}
	for key, value := range fields {
		switch key {
		case "enable", "auto-route", "auto-detect-interface", "strict-route":
			err = boolean(value)
		case "stack":
			err = literal(value, "system", "gvisor", "mixed")
		default:
			return fmt.Errorf("%w: tun.%s is outside the privileged allowlist", ErrUnsupported, key)
		}
		if err != nil {
			return fieldError("tun."+key, err)
		}
	}
	return nil
}

func mapping(node *yaml.Node) (map[string]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode || len(node.Content)%2 != 0 {
		return nil, fmt.Errorf("%w: expected mapping", ErrUnsupported)
	}
	result := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
			return nil, fmt.Errorf("%w: mapping keys must be strings", ErrUnsupported)
		}
		if _, exists := result[key.Value]; exists {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrUnsupported, key.Value)
		}
		result[key.Value] = node.Content[i+1]
	}
	return result, nil
}

func checkNode(node *yaml.Node, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%w: YAML nesting is too deep", ErrUnsupported)
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%w: YAML aliases and anchors are not allowed", ErrUnsupported)
	}
	switch node.Tag {
	case "", "!!map", "!!seq", "!!str", "!!bool":
	default:
		return fmt.Errorf("%w: YAML tag %q is not allowed", ErrUnsupported, node.Tag)
	}
	for _, child := range node.Content {
		if err := checkNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func literal(node *yaml.Node, values ...string) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return errors.New("expected a literal string")
	}
	for _, value := range values {
		if node.Value == value {
			return nil
		}
	}
	return fmt.Errorf("unsupported value %q", node.Value)
}

func boolean(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" || (node.Value != "true" && node.Value != "false") {
		return errors.New("expected true or false")
	}
	return nil
}

func fieldError(field string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrUnsupported, field, err)
}

// Snapshot copies validated bytes into an existing root-owned private directory.
// The caller must use the returned file in a root-owned work directory. It must
// never pass a user-writable profile path to root mihomo.
func Snapshot(rootDir string, data []byte) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("root privileges required for configuration snapshot")
	}
	if runtime.GOOS == "darwin" {
		return "", errors.New("macOS privileged snapshots are not enabled until ACL verification is implemented")
	}
	if err := Validate(data); err != nil {
		return "", err
	}
	if err := securePrivateDirectory(rootDir); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(rootDir, "config-*.yaml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer func() {
		if err != nil {
			os.Remove(path)
		}
	}()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

func securePrivateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("snapshot directory must be an absolute clean path")
	}
	first := true
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return fmt.Errorf("%s is not root-owned", path)
		}
		if info.Mode().Perm()&0022 != 0 || first && info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("%s is writable or readable by other users", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
		first = false
	}
}

// IsUnsupported distinguishes a policy refusal from an I/O failure.
func IsUnsupported(err error) bool { return errors.Is(err, ErrUnsupported) }
