package proxy

import (
	"context"
	"encoding/json"
	"github.com/ronigooja/Nagi/internal/i18n"
	"net/url"
	"sort"
)

// Provider contains only public operational fields. The native response also
// contains test URLs and full proxy definitions, which must not be returned.
type Provider struct {
	NodeInfo    map[string]NodeInfo  `json:"node_info"`
	Name        string               `json:"name"`
	NodeDelays  map[string]NodeDelay `json:"node_delays"`
	Type        string               `json:"type"`
	VehicleType string               `json:"vehicle_type"`
	UpdatedAt   string               `json:"updated_at,omitempty"`
	Nodes       []string             `json:"nodes"`
}

func (s *Service) Providers(ctx context.Context) ([]Provider, error) {
	if s == nil || s.Client == nil {
		return nil, i18n.Errorf("backend.proxy.proxy_control_client_unavailable", "proxy control client unavailable")
	}
	var response struct {
		Providers map[string]struct {
			Name        string         `json:"name"`
			Type        string         `json:"type"`
			VehicleType string         `json:"vehicleType"`
			UpdatedAt   string         `json:"updatedAt"`
			TestURL     string         `json:"testUrl"`
			Proxies     []proxyHistory `json:"proxies"`
		} `json:"providers"`
	}
	if err := s.Client.Get(ctx, "/providers/proxies", &response); err != nil {
		return nil, i18n.Errorf("backend.proxy.cannot_read_proxy_providers_from_mihomo", "cannot read proxy providers from mihomo")
	}
	result := make([]Provider, 0, len(response.Providers))
	for name, raw := range response.Providers {
		if name == "COMPATIBLE" || raw.VehicleType == "Compatible" || s.generatedProvider(name) {
			continue
		}
		p := Provider{Name: name, Type: raw.Type, VehicleType: raw.VehicleType, UpdatedAt: raw.UpdatedAt, Nodes: []string{}, NodeDelays: map[string]NodeDelay{}, NodeInfo: map[string]NodeInfo{}}
		for _, node := range raw.Proxies {
			p.Nodes = append(p.Nodes, node.Name)
			kind := scopedNodeKind(node.Name, node, name)
			info := NodeInfo{Kind: kind}
			if kind == "node" {
				p.NodeDelays[node.Name] = latestDelay(node, raw.TestURL)
				info.Status = delayStatus(p.NodeDelays[node.Name])
			}
			p.NodeInfo[node.Name] = info
		}
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *Service) Provider(ctx context.Context, name string) (Provider, error) {
	providers, err := s.Providers(ctx)
	if err != nil {
		return Provider{}, err
	}
	for _, provider := range providers {
		if provider.Name == "COMPATIBLE" || provider.VehicleType == "Compatible" {
			continue
		}
		if provider.Name == name {
			return provider, nil
		}
	}
	return Provider{}, i18n.Errorf("backend.proxy.proxy_provider_not_found_run_nagi_proxy_providers", "proxy provider not found; run `nagi proxy providers`")
}

func (s *Service) providerForNode(ctx context.Context, node string) (string, bool, error) {
	providers, err := s.Providers(ctx)
	if err != nil {
		return "", false, err
	}
	var matches []string
	for _, provider := range providers {
		for _, candidate := range provider.Nodes {
			if candidate == node {
				matches = append(matches, provider.Name)
			}
		}
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	var top struct {
		Proxies map[string]json.RawMessage `json:"proxies"`
	}
	if err := s.Client.Get(ctx, "/proxies", &top); err != nil {
		return "", false, i18n.Errorf("backend.proxy.cannot_resolve_proxy_node_ownership", "cannot resolve proxy node ownership")
	}
	if _, ok := top.Proxies[node]; ok {
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, i18n.Errorf("backend.proxy.proxy_node_belongs_to_multiple_providers_choose_a_unique_node_name", "proxy node belongs to multiple providers; choose a unique node name")
	}
	return matches[0], true, nil
}

func (s *Service) ProviderUpdate(ctx context.Context, name string) error {
	if _, err := s.Provider(ctx, name); err != nil {
		return err
	}
	if err := s.Client.Put(ctx, "/providers/proxies/"+url.PathEscape(name), nil, nil); err != nil {
		return i18n.Errorf("backend.proxy.proxy_provider_update_failed_inspect_mihomo_logs", "proxy provider update failed; inspect mihomo logs")
	}
	failed := false
	for _, derived := range s.ProviderChainSources[name] {
		if err := s.Client.Put(ctx, "/providers/proxies/"+url.PathEscape(derived), nil, nil); err != nil {
			failed = true
		}
	}

	if failed {
		return i18n.Errorf("backend.proxy.provider_chain_refresh_failed", "provider updated, but a selected chain node refresh failed; mihomo retained its last good node; inspect mihomo logs")
	}
	return nil
}
func (s *Service) generatedProvider(name string) bool {
	for _, names := range s.ProviderChainSources {
		for _, n := range names {
			if name == n {
				return true
			}
		}
	}
	return false
}

func (s *Service) ProviderHealthcheck(ctx context.Context, name string) error {
	if _, err := s.Provider(ctx, name); err != nil {
		return err
	}
	if err := s.Client.Get(ctx, "/providers/proxies/"+url.PathEscape(name)+"/healthcheck", nil); err != nil {
		return i18n.Errorf("backend.proxy.proxy_provider_healthcheck_failed_inspect_mihomo_logs", "proxy provider healthcheck failed; inspect mihomo logs")
	}
	return nil
}
