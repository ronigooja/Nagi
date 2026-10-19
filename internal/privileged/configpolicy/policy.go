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

	"github.com/ronigooja/Nagi/internal/i18n"
)

const maxConfigBytes = 8 << 20

var ErrUnsupported = i18n.Errorf("system.privileged.configuration.is.not.supported.for.privileged.mihomo", "configuration is not supported for privileged mihomo")

// Validate permits the inline configuration used by normal subscriptions.
// It rejects features that let a root mihomo process read arbitrary local
// files, execute scripts, or expose its controller. Unknown top-level features
// fail closed and must be reviewed against the pinned mihomo version.
func Validate(data []byte) error {
	if len(data) == 0 || len(data) > maxConfigBytes {
		return policyErrorf("system.privileged.configuration.must.be.1.bytes", "%w: configuration must be 1..%d bytes", ErrUnsupported, maxConfigBytes)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return policyErrorf("system.privileged.invalid.yaml", "%w: invalid YAML: %v", ErrUnsupported, err)
	}
	var next yaml.Node
	if err := decoder.Decode(&next); err != io.EOF {
		return policyErrorf("system.privileged.multiple.yaml.documents.are.not.allowed", "%w: multiple YAML documents are not allowed", ErrUnsupported)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return policyErrorf("system.privileged.expected.a.yaml.mapping", "%w: expected a YAML mapping", ErrUnsupported)
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
		case "rule-providers", "proxy-providers":
			if err := validateProviders(value, key); err != nil {
				return fieldError(key, err)
			}
		case "geo-auto-update":
			if err := falseValue(value); err != nil {
				return fieldError(key, err)
			}
		case "proxy-groups", "rules", "sub-rules", "dns", "hosts", "sniffer", "profile", "authentication", "skip-auth-prefixes", "lan-allowed-ips", "lan-disallowed-ips", "unified-delay", "tcp-concurrent", "global-client-fingerprint", "find-process-mode", "geodata-mode", "geodata-loader", "geosite-matcher", "geo-update-interval", "interface-name", "keep-alive-idle", "keep-alive-interval", "disable-keep-alive", "preserve-proxy-hostname", "listeners":
			if err := validateInline(value, key); err != nil {
				return err
			}
		default:
			return policyErrorf("system.privileged.key.is.outside.the.privileged.allowlist", "%w: key %q is outside the privileged allowlist", ErrUnsupported, key)
		}
	}
	return nil
}

func port(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return i18n.Errorf("system.privileged.expected.integer.port", "expected integer port")
	}
	var value int
	if err := node.Decode(&value); err != nil || value < 0 || value > 65535 || value != 0 && value < 1024 {
		return i18n.Errorf("system.privileged.expected.port.0.or.1024.65535", "expected port 0 or 1024..65535")
	}
	return nil
}

func validateProviders(node *yaml.Node, context string) error {
	providers, err := mapping(node)
	if err != nil {
		return err
	}
	for name, item := range providers {
		fields, err := mapping(item)
		if err != nil {
			return fieldError(name, err)
		}
		kind, ok := fields["type"]
		if !ok {
			return policyErrorf("system.privileged.rule.provider.has.no.type", "%w: rule provider %q has no type", ErrUnsupported, name)
		}
		if err := literal(kind, "inline", "http", "file"); err != nil {
			return fieldError(name+".type", err)
		}
		if context == "rule-providers" {
			if behavior, ok := fields["behavior"]; ok {
				if err := literal(behavior, "classical", "domain", "ipcidr"); err != nil {
					return fieldError(name+".behavior", err)
				}
			}
		}
		for key, value := range fields {
			if key == "path" && kind.Value == "file" {
				continue
			}
			if key == "path" && kind.Value == "http" {
				return policyErrorf("system.privileged.path.is.managed.by.mihomo", "%w: %s.path is managed by mihomo", ErrUnsupported, context)
			}
			if key == "path" || key == "path-in-bundle" || key == "age-secret-key" {
				return policyErrorf("system.privileged.is.not.supported", "%w: %s.%s is not supported", ErrUnsupported, context, key)
			}
			if key == "payload" && context == "proxy-providers" {
				if err := validateProviderProxies(value); err != nil {
					return err
				}
				continue
			}
			if err := validateInline(value, context+"."+name+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProxies(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return policyErrorf("system.privileged.expected.proxy.list", "%w: expected proxy list", ErrUnsupported)
	}
	for _, proxy := range node.Content {
		fields, err := mapping(proxy)
		if err != nil {
			return err
		}
		kind, ok := fields["type"]
		if !ok {
			return policyErrorf("system.privileged.proxy.type.is.required", "%w: proxy type is required", ErrUnsupported)
		}
		if err := literal(kind, "ss", "ssr", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "http", "socks5", "snell", "wireguard", "anytls", "tailscale"); err != nil {
			return fieldError("proxies.type", err)
		}
		if plugin := fields["plugin"]; plugin != nil {
			if kind.Value != "ss" || literal(plugin, "obfs", "v2ray-plugin", "gost-plugin", "shadow-tls", "restls", "kcptun-plugin") != nil {
				return policyErrorf("system.privileged.proxies.plugin.is.not.an.audited.built.in.plugin", "%w: proxies.plugin is not an audited built-in plugin", ErrUnsupported)
			}
		}
		for key, value := range fields {
			if key == "certificate" || key == "private-key" && kind.Value != "wireguard" {
				if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
					return policyErrorf("system.privileged.proxies.requires.inline.pem.or.a.file.snapshot", "%w: proxies.%s requires inline PEM or a file snapshot", ErrUnsupported, key)
				}
				continue
			}
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
			if context == "proxies" && (lower == "plugin" || lower == "plugin-opts" || lower == "certificate" || lower == "private-key") {
				continue
			}
			if context == "dns" && lower == "listen" {
				continue
			}
			if forbiddenInlineKey(lower) || lower == "path" && context != "proxies.ws-opts" && context != "proxies.h2-opts" && context != "proxies.http-opts" {
				return policyErrorf("system.privileged.may.access.a.local.file.or.execute.code", "%w: %s may access a local file or execute code", ErrUnsupported, child)
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
			return policyErrorf("system.privileged.contains.a.file.uri", "%w: %s contains a file URI", ErrUnsupported, context)
		}
	}
	return nil
}

func forbiddenInlineKey(key string) bool {
	switch key {
	case "script", "exec", "command", "file", "filename", "filepath", "certificate", "private-key", "client-cert", "client-key", "ca-cert", "custom-certifactes", "tls", "external-controller", "external-controller-unix", "external-controller-tls", "external-ui", "external-ui-url", "secret", "path-url":
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
			return policyErrorf("system.privileged.tun.is.outside.the.privileged.allowlist", "%w: tun.%s is outside the privileged allowlist", ErrUnsupported, key)
		}
		if err != nil {
			return fieldError("tun."+key, err)
		}
	}
	return nil
}

func nonnegativeInteger(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return i18n.Errorf("system.privileged.expected.nonnegative.integer", "expected nonnegative integer")
	}
	var value int
	if err := node.Decode(&value); err != nil || value < 0 {
		return i18n.Errorf("system.privileged.expected.nonnegative.integer", "expected nonnegative integer")
	}
	return nil
}

func mapping(node *yaml.Node) (map[string]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode || len(node.Content)%2 != 0 {
		return nil, policyErrorf("system.privileged.expected.mapping", "%w: expected mapping", ErrUnsupported)
	}
	result := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
			return nil, policyErrorf("system.privileged.mapping.keys.must.be.strings", "%w: mapping keys must be strings", ErrUnsupported)
		}
		if _, exists := result[key.Value]; exists {
			return nil, policyErrorf("system.privileged.duplicate.key", "%w: duplicate key %q", ErrUnsupported, key.Value)
		}
		result[key.Value] = node.Content[i+1]
	}
	return result, nil
}

func checkNode(node *yaml.Node, depth int) error {
	if depth > 32 {
		return policyErrorf("system.privileged.yaml.nesting.is.too.deep", "%w: YAML nesting is too deep", ErrUnsupported)
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return policyErrorf("system.privileged.yaml.aliases.and.anchors.are.not.allowed", "%w: YAML aliases and anchors are not allowed", ErrUnsupported)
	}
	switch node.Tag {
	case "", "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!float", "!!null":
	default:
		return policyErrorf("system.privileged.yaml.tag.is.not.allowed", "%w: YAML tag %q is not allowed", ErrUnsupported, node.Tag)
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
		return i18n.Errorf("system.privileged.expected.a.literal.string", "expected a literal string")
	}
	for _, value := range values {
		if node.Value == value {
			return nil
		}
	}
	// Values may be credentials or provider URLs. The caller adds the field
	// name, which is sufficient to locate the rejected setting.
	return i18n.Errorf("system.privileged.unsupported.value", "unsupported value")
}

func boolean(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" || (node.Value != "true" && node.Value != "false") {
		return i18n.Errorf("system.privileged.expected.true.or.false", "expected true or false")
	}
	return nil
}

func falseValue(node *yaml.Node) error {
	if err := boolean(node); err != nil {
		return err
	}
	if node.Value != "false" {
		return i18n.Errorf("system.privileged.must.be.false", "must be false")
	}
	return nil
}

func policyErrorf(id, format string, values ...any) *i18n.Message {
	message := i18n.Errorf(id, format, values...)
	descriptor := message.Descriptor()
	delete(descriptor.Args, "arg0") // The first argument is the fixed ErrUnsupported sentinel.
	return i18n.NewMessage(id, descriptor.Args, message.Error(), message)
}

func fieldError(field string, err error) error {
	original := fmt.Errorf("%w: %s: %v", ErrUnsupported, field, err)
	var message *i18n.Message
	if errors.As(err, &message) {
		descriptor := message.Descriptor()
		if inner, ok := descriptor.Args["field"].(string); ok && strings.HasSuffix(descriptor.ID, ".field") {
			descriptor.Args["field"] = field + "." + inner
			return i18n.NewMessage(descriptor.ID, descriptor.Args, original.Error(), original)
		}
		// Compose a registered event variant from the source ID, never error text.
		if i18n.ValidDescriptor(i18n.Descriptor{ID: descriptor.ID + ".field", Args: withField(descriptor.Args, field)}) {
			return i18n.NewMessage(descriptor.ID+".field", withField(descriptor.Args, field), original.Error(), original)
		}
	}
	return i18n.NewMessage("system.privileged.field.invalid", map[string]any{"field": field}, original.Error(), original)
}
func withField(args map[string]any, field string) map[string]any {
	copied := make(map[string]any, len(args)+1)
	for key, value := range args {
		copied[key] = value
	}
	copied["field"] = field
	return copied
}

// Snapshot copies validated bytes into an existing root-owned private directory.
// The caller must use the returned file in a root-owned work directory. It must
// never pass a user-writable profile path to root mihomo.
func Snapshot(rootDir string, data []byte) (string, error) {
	if os.Geteuid() != 0 {
		return "", i18n.Errorf("system.privileged.root.privileges.required.for.configuration.snapshot", "root privileges required for configuration snapshot")
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

// SnapshotResource stores bytes already read through the helper's guarded
// user-file walk. It never accepts a source path for the root process to open.
func SnapshotResource(rootDir string, data []byte) (string, error) {
	if os.Geteuid() != 0 {
		return "", i18n.Errorf("system.privileged.root.privileges.required.for.resource.snapshot", "root privileges required for resource snapshot")
	}
	if len(data) == 0 || len(data) > maxConfigBytes {
		return "", policyErrorf("system.privileged.resource.size.is.outside.1.bytes", "%w: resource size is outside 1..%d bytes", ErrUnsupported, maxConfigBytes)
	}
	if err := securePrivateDirectory(rootDir); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(rootDir, "resource-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
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
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func securePrivateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return i18n.Errorf("system.privileged.snapshot.directory.must.be.an.absolute.clean.path", "snapshot directory must be an absolute clean path")
	}
	first := true
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return i18n.Errorf("system.privileged.is.not.a.directory", "%s is not a directory", path)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return i18n.Errorf("system.privileged.is.not.root.owned", "%s is not root-owned", path)
		}
		if info.Mode().Perm()&0022 != 0 || first && info.Mode().Perm()&0077 != 0 {
			return i18n.Errorf("system.privileged.is.writable.or.readable.by.other.users", "%s is writable or readable by other users", path)
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

func validateProviderProxies(node *yaml.Node) error {
	if node != nil && node.Kind == yaml.SequenceNode {
		for _, proxy := range node.Content {
			fields, err := mapping(proxy)
			if err != nil {
				return err
			}
			if kind := fields["type"]; kind != nil && kind.Value == "tailscale" {
				return policyErrorf("system.privileged.provider.tailscale.nodes.are.not.supported", "%w: provider Tailscale nodes are not supported", ErrUnsupported)
			}
		}
	}
	return validateProxies(node)
}
