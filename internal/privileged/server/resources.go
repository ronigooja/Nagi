package server

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/privileged/configpolicy"
	"gopkg.in/yaml.v3"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// prepareConfig copies every permitted local reference before the root child
// receives the profile. Relative references are anchored at the source profile.
func (h *RuntimeHandler) prepareConfig(data []byte, source string) (string, []string, error) {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) != 1 {
		return "", nil, i18n.Errorf("system.privileged.invalid.configuration.yaml", "invalid configuration YAML")
	}
	root := doc.Content[0]
	// Generated HTTP providers carry a user-mode cache path. Remove only the
	// exact generated path before policy validation; root never opens that path.
	type generatedCache struct {
		provider *yaml.Node
		name     string
	}
	var generated []generatedCache
	profileName := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	profileName = strings.TrimPrefix(profileName, ".effective-")
	profileName = strings.TrimPrefix(profileName, ".restore-")
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "proxy-providers" {
			continue
		}
		mapping := root.Content[i+1]
		for j := 0; j+1 < len(mapping.Content); j += 2 {
			name := mapping.Content[j].Value
			p := mapping.Content[j+1]
			if field(p, "type") != "http" {
				continue
			}
			path := field(p, "path")
			if path == "" {
				continue
			}
			if !profile.IsProviderChainCache(name, path, filepath.Dir(source), profileName) {
				continue
			}
			for k := 0; k+1 < len(p.Content); k += 2 {
				if p.Content[k].Value == "path" {
					p.Content = append(p.Content[:k], p.Content[k+2:]...)
					break
				}
			}
			generated = append(generated, generatedCache{p, name})
		}
	}
	sanitized, err := yaml.Marshal(&doc)
	if err != nil {
		return "", nil, err
	}
	if err = configpolicy.Validate(sanitized); err != nil {
		return "", nil, err
	}
	var resources []string
	fail := func() (string, []string, error) {
		removeSnapshots("", resources)
		return "", nil, i18n.Errorf("system.privileged.unsafe.configuration.resource", "unsafe configuration resource")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "proxy-providers", "rule-providers":
			for _, provider := range mappingValues(root.Content[i+1]) {
				if root.Content[i].Value == "proxy-providers" && field(provider, "type") == "inline" {
					if err := h.snapshotProxyResources(source, fieldNode(provider, "payload"), &resources); err != nil {
						return fail()
					}
				}
				if field(provider, "type") != "file" {
					continue
				}
				path := fieldNode(provider, "path")
				if path == nil || h.snapshotReference(source, path, &resources) != nil {
					return fail()
				}
			}
		case "proxies":
			name := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
			name = strings.TrimPrefix(name, ".effective-")
			name = strings.TrimPrefix(name, ".restore-")
			for _, proxy := range root.Content[i+1].Content {
				if field(proxy, "type") != "tailscale" {
					continue
				}
				path, err := nagiruntime.TailscaleStatePath(h.PrivateDir, name, field(proxy, "name"), field(proxy, "control-url"))
				if err != nil {
					return fail()
				}
				state := fieldNode(proxy, "state-dir")
				if state == nil {
					proxy.Content = append(proxy.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "state-dir"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path})
				} else {
					state.Kind = yaml.ScalarNode
					state.Tag = "!!str"
					state.Value = path
					state.Content = nil
				}
			}
			if err := h.snapshotProxyResources(source, root.Content[i+1], &resources); err != nil {
				return fail()
			}
		}
	}
	// Allocate a private, stable cache independent of the user path. The
	// URL participates in identity so a different source never inherits old data.
	for _, g := range generated {
		digest := sha256.Sum256([]byte(profileName + "\x00" + g.name + "\x00" + field(g.provider, "url")))
		path := filepath.Join(h.PrivateDir, ".chain-cache", fmt.Sprintf("%x.yaml", digest[:16]))
		if _, err := profile.PreparePrivateChainCache(h.PrivateDir, path); err != nil {
			return fail()
		}
		g.provider.Content = append(g.provider.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "path"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path})
	}
	var out bytes.Buffer
	if yaml.NewEncoder(&out).Encode(&doc) != nil {
		return fail()
	}
	snapshot, err := configpolicy.SnapshotWithProviderCaches(h.PrivateDir, out.Bytes())
	if err != nil {
		return fail()
	}
	return snapshot, resources, nil
}

func (h *RuntimeHandler) snapshotProxyResources(source string, list *yaml.Node, resources *[]string) error {
	if list == nil || list.Kind != yaml.SequenceNode {
		return i18n.Errorf("system.privileged.invalid.proxy.list", "invalid proxy list")
	}
	for _, proxy := range list.Content {
		for _, key := range []string{"certificate", "private-key"} {
			if key == "private-key" && field(proxy, "type") == "wireguard" {
				continue
			}
			path := fieldNode(proxy, key)
			if path == nil || strings.HasPrefix(path.Value, "-----BEGIN ") && strings.Contains(path.Value, "-----END ") {
				continue
			}
			if err := h.snapshotReference(source, path, resources); err != nil {
				return err
			}
		}
	}
	return nil
}

func mappingValues(mapping *yaml.Node) []*yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	values := make([]*yaml.Node, 0, len(mapping.Content)/2)
	for i := 1; i < len(mapping.Content); i += 2 {
		values = append(values, mapping.Content[i])
	}
	return values
}
func fieldNode(mapping *yaml.Node, name string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return mapping.Content[i+1]
		}
	}
	return nil
}
func field(mapping *yaml.Node, name string) string {
	if n := fieldNode(mapping, name); n != nil {
		return n.Value
	}
	return ""
}

func (h *RuntimeHandler) snapshotReference(source string, node *yaml.Node, resources *[]string) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || node.Value == "" {
		return i18n.Errorf("system.privileged.invalid.resource.path", "invalid resource path")
	}
	path := node.Value
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(source), path)
	}
	if filepath.Clean(path) != path {
		return i18n.Errorf("system.privileged.unclean.resource.path", "unclean resource path")
	}
	data, err := readUserConfig(h.SourceDir, path, h.SessionUID)
	if err != nil {
		return i18n.Errorf("system.privileged.unsafe.resource.path", "unsafe resource path")
	}
	snapshot, err := configpolicy.SnapshotResource(h.PrivateDir, data)
	if err != nil {
		return i18n.Errorf("system.privileged.resource.snapshot.failed", "resource snapshot failed")
	}
	*resources = append(*resources, snapshot)
	node.Value = snapshot
	return nil
}
