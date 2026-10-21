package command

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
	"gopkg.in/yaml.v3"
)

var rejectedRulePath = regexp.MustCompile(`^rules\[([0-9]+)\]$`)

// annotateMissingRuleProxy supplements an otherwise generic engine rejection
// with candidate configuration evidence. It neither validates nor matches rules.
// Provider nodes may be renamed or loaded externally; leave those to mihomo.
func annotateMissingRuleProxy(candidate []byte, diagnostic *profile.ValidationError) {
	if diagnostic.Code != "invalid_config" || diagnostic.Message != "configuration validation failed" {
		return
	}
	match := rejectedRulePath.FindStringSubmatch(diagnostic.Path)
	if match == nil {
		return
	}
	index, err := strconv.Atoi(match[1])
	if err != nil {
		return
	}
	var doc struct {
		Rules          []string                `yaml:"rules"`
		Proxies        []struct{ Name string } `yaml:"proxies"`
		Groups         []struct{ Name string } `yaml:"proxy-groups"`
		ProxyProviders map[string]any          `yaml:"proxy-providers"`
	}
	if yaml.Unmarshal(candidate, &doc) != nil || index >= len(doc.Rules) || len(doc.ProxyProviders) != 0 {
		return
	}
	line := doc.Rules[index]
	typ := strings.ToUpper(strings.Trim(strings.SplitN(line, ",", 2)[0], " "))
	// Restrict supplementary advice to unambiguous native target positions.
	switch typ {
	case "MATCH", "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6":
	default:
		return
	}
	targets := rules.Targets(rules.Entry{Kind: "rules", Items: []string{line}})
	if len(targets) != 1 {
		return
	}
	target := targets[0]
	switch target {
	case "DIRECT", "REJECT", "REJECT-DROP", "COMPATIBLE", "PASS", "PASS-RULE", "GLOBAL":
		return
	}
	for _, node := range doc.Proxies {
		if node.Name == target {
			return
		}
	}
	for _, group := range doc.Groups {
		if group.Name == target {
			return
		}
	}
	diagnostic.MissingProxy = target
}
