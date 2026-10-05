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
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 8 << 20

var ErrUnsupported = errors.New("configuration is not supported for privileged mihomo")

// Validate permits the inline configuration used by normal subscriptions.
// It rejects features that let a root mihomo process read arbitrary local
// files, execute scripts, or expose its controller. Unknown top-level features
// fail closed and must be reviewed against the pinned mihomo version.
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
			if err := literal(value, "direct", "rule", "global"); err != nil {
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
		case "port", "socks-port", "mixed-port":
			if err := port(value); err != nil {
				return fieldError(key, err)
			}
		case "allow-lan":
			if err := falseValue(value); err != nil {
				return fieldError(key, err)
			}
		case "bind-address":
			if err := literal(value, "127.0.0.1", "localhost", "::1"); err != nil {
				return fieldError(key, err)
			}
		case "proxies":
			if err := validateProxies(value); err != nil {
				return fieldError(key, err)
			}
		case "rule-providers":
			if err := validateInlineRuleProviders(value); err != nil {
				return fieldError(key, err)
			}
		case "geo-auto-update":
			if err := falseValue(value); err != nil {
				return fieldError(key, err)
			}
		case "proxy-groups", "rules", "sub-rules", "dns", "hosts", "sniffer", "profile", "authentication", "skip-auth-prefixes", "lan-allowed-ips", "lan-disallowed-ips", "unified-delay", "tcp-concurrent", "global-client-fingerprint", "find-process-mode", "geodata-mode", "geodata-loader", "geosite-matcher", "geo-update-interval", "interface-name", "keep-alive-idle", "keep-alive-interval", "disable-keep-alive":
			if err := validateInline(value, key); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: key %q is outside the privileged allowlist", ErrUnsupported, key)
		}
	}
	return nil
}

func port(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return errors.New("expected integer port")
	}
	var value int
	if err := node.Decode(&value); err != nil || value < 0 || value > 65535 || value != 0 && value < 1024 {
		return errors.New("expected port 0 or 1024..65535")
	}
	return nil
}

func validateInlineRuleProviders(node *yaml.Node) error {
	providers, err := mapping(node)
	if err != nil {
		return err
	}
	for name, item := range providers {
		fields, err := mapping(item)
		if err != nil {
			return fieldError(name, err)
		}
		if len(fields) != 3 {
			return fmt.Errorf("%w: rule provider %q must be inline", ErrUnsupported, name)
		}
		kind, ok := fields["type"]
		if !ok {
			return fmt.Errorf("%w: rule provider %q has no type", ErrUnsupported, name)
		}
		if err := literal(kind, "inline"); err != nil {
			return fieldError(name+".type", err)
		}
		behavior, ok := fields["behavior"]
		if !ok {
			return fmt.Errorf("%w: rule provider %q has no behavior", ErrUnsupported, name)
		}
		if err := literal(behavior, "classical", "domain", "ipcidr"); err != nil {
			return fieldError(name+".behavior", err)
		}
		payload, ok := fields["payload"]
		if !ok || payload.Kind != yaml.SequenceNode {
			return fmt.Errorf("%w: rule provider %q needs inline payload", ErrUnsupported, name)
		}
		for _, line := range payload.Content {
			if line.Kind != yaml.ScalarNode || line.Tag != "!!str" {
				return fmt.Errorf("%w: rule provider %q payload must be strings", ErrUnsupported, name)
			}
		}
	}
	return nil
}

func validateProxies(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("%w: expected proxy list", ErrUnsupported)
	}
	for _, proxy := range node.Content {
		fields, err := mapping(proxy)
		if err != nil {
			return err
		}
		kind, ok := fields["type"]
		if !ok {
			return fmt.Errorf("%w: proxy type is required", ErrUnsupported)
		}
		if err := literal(kind, "ss", "ssr", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "http", "socks5"); err != nil {
			return fieldError("proxies.type", err)
		}
		if err := validateInline(proxy, "proxies"); err != nil {
			return err
		}
	}
	return nil
}

// Paths used as HTTP/WebSocket request targets are allowed in their known
// locations. Other path-bearing options are rejected, including future
// options that might otherwise silently become filesystem access.
func validateInline(node *yaml.Node, context string) error {
	switch node.Kind {
	case yaml.MappingNode:
		fields, err := mapping(node)
		if err != nil {
			return err
		}
		for key, value := range fields {
			lower := strings.ToLower(key)
			child := context + "." + key
			if context == "proxies" && lower == "tls" {
				if err := boolean(value); err != nil {
					return fieldError(child, err)
				}
				continue
			}
			if context == "dns" && lower == "listen" {
				return fmt.Errorf("%w: DNS listeners are not supported in privileged profiles", ErrUnsupported)
			}
			if forbiddenInlineKey(lower) || lower == "path" && context != "proxies.ws-opts" && context != "proxies.h2-opts" && context != "proxies.http-opts" {
				return fmt.Errorf("%w: %s may access a local file or execute code", ErrUnsupported, child)
			}
			if err := validateInline(value, child); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if err := validateInline(item, context); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag == "!!str" && (strings.HasPrefix(node.Value, "file:") || strings.HasPrefix(node.Value, "file://")) {
			return fmt.Errorf("%w: %s contains a file URI", ErrUnsupported, context)
		}
	}
	return nil
}

func forbiddenInlineKey(key string) bool {
	switch key {
	case "script", "exec", "command", "plugin", "plugin-opts", "file", "filename", "filepath", "certificate", "private-key", "client-cert", "client-key", "ca-cert", "custom-certifactes", "tls", "external-controller", "external-controller-unix", "external-controller-tls", "external-ui", "external-ui-url", "secret", "path-url":
		return true
	}
	return strings.HasSuffix(key, "-path") || strings.HasSuffix(key, "-file")
}

func validateTun(node *yaml.Node) error {
	fields, err := mapping(node)
	if err != nil {
		return err
	}
	for key, value := range fields {
		switch key {
		case "enable", "auto-route", "auto-detect-interface", "strict-route", "recvmsgx", "sendmsgx":
			err = boolean(value)
		case "stack":
			err = literal(value, "system", "gvisor", "mixed")
		case "dns-hijack", "route-address", "route-exclude-address", "include-interface", "exclude-interface":
			err = validateInline(value, "tun."+key)
		case "mtu":
			err = nonnegativeInteger(value)
		default:
			return fmt.Errorf("%w: tun.%s is outside the privileged allowlist", ErrUnsupported, key)
		}
		if err != nil {
			return fieldError("tun."+key, err)
		}
	}
	return nil
}

func nonnegativeInteger(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return errors.New("expected nonnegative integer")
	}
	var value int
	if err := node.Decode(&value); err != nil || value < 0 {
		return errors.New("expected nonnegative integer")
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
	case "", "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!float", "!!null":
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

func falseValue(node *yaml.Node) error {
	if err := boolean(node); err != nil {
		return err
	}
	if node.Value != "false" {
		return errors.New("must be false")
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
		if runtime.GOOS == "darwin" {
			if err := rejectDarwinACL(path); err != nil {
				return err
			}
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
