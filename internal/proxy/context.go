package proxy

import (
	"context"
	"github.com/ronigooja/Nagi/internal/i18n"
	"net/url"
	"sort"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type flexibleInt int

func (v *flexibleInt) UnmarshalYAML(n *yaml.Node) error {
	i, e := strconv.Atoi(n.Value)
	*v = flexibleInt(i)
	return e
}

type checkConfig struct {
	URL      string      `yaml:"url"`
	Timeout  flexibleInt `yaml:"timeout"`
	Expected string      `yaml:"expected-status"`
}
type groupConfig struct {
	Name                string   `yaml:"name"`
	Proxies             []string `yaml:"proxies"`
	Use                 []string `yaml:"use"`
	IncludeAll          bool     `yaml:"include-all"`
	IncludeAllProxies   bool     `yaml:"include-all-proxies"`
	IncludeAllProviders bool     `yaml:"include-all-providers"`
	checkConfig         `yaml:",inline"`
}
type effectiveConfig struct {
	Groups    []groupConfig `yaml:"proxy-groups"`
	Providers map[string]struct {
		HealthCheck checkConfig `yaml:"health-check"`
		Type        string      `yaml:"type"`
		URL         string      `yaml:"url"`
		Path        string      `yaml:"path"`
	} `yaml:"proxy-providers"`
}

func (s *Service) RequireEffectiveConfig() { s.requireConfig = true }

func (s *Service) Configure(data []byte) error {
	s.requireConfig = true
	s.config = nil
	var c effectiveConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return err
	}
	s.config = &c
	return nil
}
func (s *Service) groupConfig(name string) (groupConfig, bool) {
	if s.config != nil {
		for _, g := range s.config.Groups {
			if g.Name == name {
				return g, true
			}
		}
	}
	return groupConfig{}, false
}
func (s *Service) conditions(kind, name string) (checkConfig, error) {
	c := checkConfig{}
	if kind == "group" {
		if g, ok := s.groupConfig(name); ok {
			c = g.checkConfig
			if c.URL == "" && s.config != nil {
				providers := g.Use
				if g.IncludeAll || g.IncludeAllProviders {
					providers = nil
					for name := range s.config.Providers {
						providers = append(providers, name)
					}
					sort.Strings(providers)
				}
				for _, provider := range providers {
					if candidate := s.config.Providers[provider].HealthCheck.URL; candidate != "" {
						c.URL = candidate
						break
					}
				}
			}
		}
	} else if s.config != nil {
		c = s.config.Providers[name].HealthCheck
	}
	// mihomo uses this URL and five-second timeout for omitted settings.
	if c.URL == "" {
		c.URL = "https://www.gstatic.com/generate_204"
	}
	if c.Timeout == 0 {
		c.Timeout = 5000
	}
	return c, validateDelay(c.URL, int(c.Timeout))
}
func (s *Service) GroupTestConditions(name string) (string, int, error) {
	if s.requireConfig && s.config == nil {
		return "", 0, i18n.Errorf("backend.proxy.effective_profile_unavailable_inspect_the_selected_profile_or_supply_explicit_ur", "effective profile unavailable; inspect the selected profile or supply explicit URL and timeout")
	}
	c, e := s.conditions("group", name)
	return c.URL, int(c.Timeout), e
}

// TestNode resolves identity before sending a native measurement request.
func (s *Service) TestNode(ctx context.Context, kind, owner, name string) (DelayResult, error) {
	if s.config == nil {
		return DelayResult{}, i18n.Errorf("backend.proxy.effective_profile_unavailable_inspect_the_selected_profile_before_testing", "effective profile unavailable; inspect the selected profile before testing")
	}
	if kind == "group" {
		if _, ok := s.groupConfig(owner); !ok {
			return DelayResult{}, i18n.Errorf("backend.proxy.group_missing_from_effective_profile", "group missing from effective profile")
		}
	} else {
		if _, ok := s.config.Providers[owner]; !ok {
			return DelayResult{}, i18n.Errorf("backend.proxy.provider_missing_from_effective_profile", "provider missing from effective profile")
		}
	}
	var p proxyHistory
	var provider string
	var ok bool
	r, err := s.historyReader(ctx)
	if err != nil {
		return DelayResult{}, err
	}
	if kind == "provider" {
		for _, n := range r.providerNodes(ctx, s)[owner] {
			if n.Name == name {
				if ok {
					return DelayResult{}, i18n.Errorf("backend.proxy.ambiguous_node_ownership", "ambiguous node ownership")
				}
				p = n
				ok = true
				provider = owner
			}
		}
	} else {
		g, exists := r.top[owner]
		if !exists || len(g.All) == 0 {
			return DelayResult{}, i18n.Errorf("backend.proxy.proxy_group_not_found", "proxy group not found")
		}
		member := false
		for _, n := range g.All {
			if n == name {
				member = true
			}
		}
		if member {
			p, provider, ok = r.resolve(ctx, s, owner, name)
		}
	}
	if !ok {
		return DelayResult{}, i18n.Errorf("backend.proxy.node_unavailable_or_ownership_ambiguous_inspect_the_group_or_provider_before_tes", "node unavailable or ownership ambiguous; inspect the group or provider before testing")
	}
	if scopedNodeKind(name, p, provider) != "node" {
		return DelayResult{}, i18n.Errorf("backend.proxy.latency_test_requires_an_individual_proxy_node", "latency test requires an individual proxy node")
	}
	c, err := s.conditions(kind, owner)
	if err != nil {
		return DelayResult{}, err
	}
	return s.delayResolved(ctx, name, provider, c, p)
}
func (s *Service) delayResolved(ctx context.Context, name, provider string, c checkConfig, before proxyHistory) (DelayResult, error) {
	result := DelayResult{Proxy: name, URL: c.URL, TimeoutMS: int(c.Timeout)}
	q := delayQuery(c.URL, int(c.Timeout))
	if c.Expected != "" {
		q.Set("expected", c.Expected)
	}
	path := "/proxies/" + url.PathEscape(name) + "/delay?" + q.Encode()
	if provider != "" {
		path = "/providers/proxies/" + url.PathEscape(provider) + "/" + url.PathEscape(name) + "/healthcheck?" + q.Encode()
	}
	var response struct {
		Delay int `json:"delay"`
	}
	started := time.Now()
	err := s.Client.Get(ctx, path, &response)
	// The pinned engine validates expected status before returning success;
	// shared results retain their original history timestamp.
	if err != nil {
		after, readErr := s.scopedHistory(ctx, name, provider)
		state, exists := after.Extra[c.URL]
		fresh := latestURLTime(after, c.URL).After(latestURLTime(before, c.URL)) && !latestURLTime(after, c.URL).Before(started.Add(-time.Second))
		if readErr == nil && exists && fresh && state.Alive != nil {
			if !*state.Alive {
				return result, i18n.Errorf("backend.proxy.latency_test_failed_the_configured_availability_check", "latency test failed the configured availability check")
			}
			if len(state.History) > 0 {
				result.DelayMS = state.History[len(state.History)-1].Delay
				return result, nil
			}
		}
		if err != nil {
			return result, err
		}
		return result, i18n.Errorf("backend.proxy.latency_test_completed_but_its_configured_availability_result_could_not_be_verif", "latency test completed but its configured availability result could not be verified")
	}

	result.DelayMS = response.Delay
	return result, nil
}
func latestURLTime(p proxyHistory, target string) time.Time {
	var t time.Time
	for _, h := range p.Extra[target].History {
		if h.Time.After(t) {
			t = h.Time
		}
	}
	return t
}

func (s *Service) scopedHistory(ctx context.Context, name, provider string) (proxyHistory, error) {
	var after proxyHistory
	if provider == "" {
		err := s.Client.Get(ctx, "/proxies/"+url.PathEscape(name), &after)
		return after, err
	}
	var raw struct {
		Proxies []proxyHistory `json:"proxies"`
	}
	if err := s.Client.Get(ctx, "/providers/proxies/"+url.PathEscape(provider), &raw); err != nil {
		return after, err
	}
	count := 0
	for _, p := range raw.Proxies {
		if p.Name == name {
			after = p
			count++
		}
	}
	if count != 1 {
		return after, i18n.Errorf("backend.proxy.node_ownership_unavailable_or_ambiguous", "node ownership unavailable or ambiguous")
	}
	return after, nil
}
