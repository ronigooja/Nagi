package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
)

// Provider contains only public operational fields. The native response also
// contains test URLs and full proxy definitions, which must not be returned.
type Provider struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	VehicleType string   `json:"vehicle_type"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Nodes       []string `json:"nodes"`
}

func (s *Service) Providers(ctx context.Context) ([]Provider, error) {
	if s == nil || s.Client == nil {
		return nil, errors.New("proxy control client unavailable")
	}
	var response struct {
		Providers map[string]struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			VehicleType string `json:"vehicleType"`
			UpdatedAt   string `json:"updatedAt"`
			Proxies     []struct {
				Name string `json:"name"`
			} `json:"proxies"`
		} `json:"providers"`
	}
	if err := s.Client.Get(ctx, "/providers/proxies", &response); err != nil {
		return nil, errors.New("cannot read proxy providers from mihomo")
	}
	result := make([]Provider, 0, len(response.Providers))
	for name, raw := range response.Providers {
		if name == "COMPATIBLE" || raw.VehicleType == "Compatible" {
			continue
		}
		p := Provider{Name: name, Type: raw.Type, VehicleType: raw.VehicleType, UpdatedAt: raw.UpdatedAt, Nodes: []string{}}
		for _, node := range raw.Proxies {
			p.Nodes = append(p.Nodes, node.Name)
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
	return Provider{}, errors.New("proxy provider not found; run `nagi proxy providers`")
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
		return "", false, errors.New("cannot resolve proxy node ownership")
	}
	if _, ok := top.Proxies[node]; ok {
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, errors.New("proxy node belongs to multiple providers; choose a unique node name")
	}
	return matches[0], true, nil
}

func (s *Service) ProviderUpdate(ctx context.Context, name string) error {
	if _, err := s.Provider(ctx, name); err != nil {
		return err
	}
	if err := s.Client.Put(ctx, "/providers/proxies/"+url.PathEscape(name), nil, nil); err != nil {
		return errors.New("proxy provider update failed; inspect mihomo logs")
	}
	return nil
}

func (s *Service) ProviderHealthcheck(ctx context.Context, name string) error {
	if _, err := s.Provider(ctx, name); err != nil {
		return err
	}
	if err := s.Client.Get(ctx, "/providers/proxies/"+url.PathEscape(name)+"/healthcheck", nil); err != nil {
		return errors.New("proxy provider healthcheck failed; inspect mihomo logs")
	}
	return nil
}
