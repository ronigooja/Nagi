package profile

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"gopkg.in/yaml.v3"
)

// ProviderChainMembersKey stores source identities and native dialer edges,
// never copies of provider credentials or payload definitions.
const ProviderChainMembersKey = "nagi-provider-chain-members"

type ProviderChainMember struct {
	Provider string `json:"provider" yaml:"provider"`
	Node     string `json:"node" yaml:"node"`
	RawName  string `json:"-" yaml:"raw-name"`
	Type     string `json:"type" yaml:"type"`
	Dialer   string `json:"dialer" yaml:"dialer-proxy"`
	Outlet   bool   `json:"outlet" yaml:"outlet,omitempty"`
}

func (m ProviderChainMember) ID() string {
	digest := sha256.Sum256([]byte(m.Provider + "\x00" + m.Node))
	return fmt.Sprintf("nagi-chain-%x", digest[:12])
}

func (m ProviderChainMember) OutletName() string { return "Chain: " + m.Provider + " / " + m.Node }

func (m ProviderChainMember) providerID() string { return m.ID() + "-source" }

func providerChainMembers(cfg map[string]any) ([]ProviderChainMember, error) {
	value, exists := cfg[ProviderChainMembersKey]
	if !exists {
		return nil, nil
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var members []ProviderChainMember
	if err := yaml.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("provider chain members must be a list")
	}
	seen := map[string]bool{}
	for _, m := range members {
		if m.Provider == "" || m.Node == "" || m.RawName == "" || !chainHop(m.Type) || m.Type == "tailscale" {
			return nil, fmt.Errorf("provider chain members require a supported node identity; provider Tailscale identities cannot be duplicated")
		}
		if seen[m.ID()] {
			return nil, fmt.Errorf("duplicate provider chain member")
		}
		seen[m.ID()] = true
	}
	return members, nil
}

func exactProviderName(name string) string {
	// Both provider and group parsers split expressions on literal backticks.
	return "\\A" + strings.ReplaceAll(regexp.QuoteMeta(name), "`", "\\x60") + "\\z"
}

func providerNameAfterOverride(name string, override map[string]any) (string, error) {
	rules, _ := override["proxy-name"].([]any)
	for _, value := range rules {
		rule, _ := value.(map[string]any)
		pattern, err := regexp2.Compile(tsString(rule, "pattern"), regexp2.None)
		if err != nil {
			return "", fmt.Errorf("provider name override has an invalid pattern")
		}
		pattern.MatchTimeout = 100 * time.Millisecond
		name, err = pattern.Replace(name, tsString(rule, "target"), 0, -1)
		if err != nil {
			return "", fmt.Errorf("provider name override could not be evaluated")
		}
	}
	return tsString(override, "additional-prefix") + name + tsString(override, "additional-suffix"), nil
}

func providerPatternMatches(pattern, name string, fallback bool) (bool, error) {
	if pattern == "" {
		return fallback, nil
	}
	for _, expression := range strings.Split(pattern, "`") {
		r, err := regexp2.Compile(expression, regexp2.None)
		if err != nil {
			return false, fmt.Errorf("provider has an invalid name filter")
		}
		r.MatchTimeout = 100 * time.Millisecond
		matched, err := r.MatchString(name)
		if err != nil {
			return false, fmt.Errorf("provider name filter could not be evaluated")
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func providerMemberAllowed(source map[string]any, m ProviderChainMember) error {
	matched, err := providerPatternMatches(tsString(source, "filter"), m.RawName, true)
	if err != nil {
		return err
	}
	excluded, err := providerPatternMatches(tsString(source, "exclude-filter"), m.RawName, false)
	if err != nil {
		return err
	}
	for _, kind := range strings.Split(tsString(source, "exclude-type"), "|") {
		if strings.EqualFold(kind, m.Type) {
			excluded = true
		}
	}
	if !matched || excluded {
		return fmt.Errorf("provider chain node no longer satisfies its source filters")
	}
	override, _ := source["override"].(map[string]any)
	effectiveName, err := providerNameAfterOverride(m.RawName, override)
	if err != nil {
		return err
	}
	if effectiveName != m.Node {
		return fmt.Errorf("provider chain node name mapping changed; clear and recreate its chain explicitly")
	}
	return nil
}

// applyProviderChainMembers composes independent native providers and strict
// bridges. The original provider remains untouched. Every effective generation
// follows its current source definition, rather than persisting a credential or
// source URL snapshot in the chain metadata.
func (s *Store) applyProviderChainMembers(profileName string, cfg map[string]any) error {
	members, err := providerChainMembers(cfg)
	if err != nil {
		return err
	}
	delete(cfg, ProviderChainMembersKey)
	if len(members) == 0 {
		return nil
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	groups, _ := cfg["proxy-groups"].([]any)
	originalProviders := make([]string, 0, len(providers))
	for name := range providers {
		originalProviders = append(originalProviders, name)
	}
	sort.Strings(originalProviders) // mihomo's include-all provider order.
	// Keep generated payloads out of include-all groups. Expand only the
	// provider part; top-level include-all-proxies retains its native behavior.
	for _, value := range groups {
		group, _ := value.(map[string]any)
		all, _ := group["include-all"].(bool)
		allProviders, _ := group["include-all-providers"].(bool)
		if all || allProviders {
			use := make([]any, len(originalProviders))
			for i, name := range originalProviders {
				use[i] = name
			}
			group["use"] = use
			group["include-all-providers"] = false
			if all {
				group["include-all"] = false
				group["include-all-proxies"] = true
			}
		}
	}
	for _, member := range members {
		source, _ := providers[member.Provider].(map[string]any)
		if source == nil {
			return fmt.Errorf("provider chain source is missing; clear its dependent chain before removing the provider")
		}
		if err := providerMemberAllowed(source, member); err != nil {
			return err
		}
		for _, generated := range []string{member.ID(), member.providerID()} {
			if _, exists := providers[generated]; exists {
				return fmt.Errorf("generated provider chain resource conflicts with an existing provider")
			}
		}
		for _, value := range groups {
			group, _ := value.(map[string]any)
			if tsString(group, "name") == member.ID() || tsString(group, "name") == member.providerID() {
				return fmt.Errorf("generated provider chain resource conflicts with an existing group")
			}
		}
		if tsFind(cfg, member.ID()) != nil {
			return fmt.Errorf("generated provider chain resource conflicts with an existing node")
		}
		data, err := yaml.Marshal(source)
		if err != nil {
			return err
		}
		var derived map[string]any
		if err := yaml.Unmarshal(data, &derived); err != nil {
			return err
		}
		derived["filter"] = exactProviderName(member.RawName)
		// A source may change a selected ordinary node into Tailscale during a
		// native refresh. Never instantiate a second provider Tailscale identity.
		excludedTypes := tsString(derived, "exclude-type")
		if excludedTypes != "" {
			excludedTypes += "|"
		}
		derived["exclude-type"] = excludedTypes + "tailscale"
		// A distinct cache prevents one provider's parsed subset or last-good
		// generation from overwriting the source provider's cache.
		if tsString(derived, "type") == "http" {
			derived["path"] = filepath.Join(s.Dir, "profiles", ".chain-cache", profileName, member.providerID()+".yaml")
		}
		override, _ := derived["override"].(map[string]any)
		if override == nil {
			override = map[string]any{}
			derived["override"] = override
		}
		override["dialer-proxy"] = member.Dialer
		healthcheck, _ := derived["health-check"].(map[string]any)
		if healthcheck == nil {
			healthcheck = map[string]any{}
			derived["health-check"] = healthcheck
		}
		healthcheck["enable"] = false
		providers[member.providerID()] = derived
		groups = append(groups, map[string]any{
			"name": member.ID(), "type": "select", "use": []any{member.providerID()},
			"filter": exactProviderName(member.Node), "empty-fallback": "REJECT", "hidden": true,
		})
	}
	for _, member := range members {
		if !member.Outlet {
			continue
		}
		outlet := member.OutletName()
		if tsFind(cfg, outlet) != nil {
			return fmt.Errorf("chain outlet name conflicts with an existing node")
		}
		for _, value := range groups {
			g, _ := value.(map[string]any)
			if tsString(g, "name") == outlet {
				return fmt.Errorf("chain outlet name conflicts with an existing group")
			}
		}
		for _, value := range groups {
			g, _ := value.(map[string]any)
			if tsString(g, "type") != "select" || tsString(g, "name") == member.ID() {
				continue
			}
			uses, _ := g["use"].([]any)
			for _, use := range uses {
				if use == member.Provider {
					ps, _ := g["proxies"].([]any)
					g["proxies"] = append(ps, outlet)
					break
				}
			}
		}
		groups = append(groups, map[string]any{"name": outlet, "type": "select", "proxies": []any{member.ID()}})
	}
	cfg["proxy-groups"] = groups
	return nil
}
