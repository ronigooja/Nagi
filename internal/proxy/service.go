package proxy

import (
	"context"
	"errors"
	"net/url"
	"sort"
)

type Client interface {
	Get(context.Context, string, any) error
	Put(context.Context, string, any, any) error
}

type Service struct{ Client Client }

func NewService(client Client) *Service { return &Service{Client: client} }

type Group struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	Now   string   `json:"now,omitempty"`
	All   []string `json:"all,omitempty"`
	Alive bool     `json:"alive"`
}

type Connection struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Chains   []string       `json:"chains,omitempty"`
	Upload   int64          `json:"upload"`
	Download int64          `json:"download"`
}

func (s *Service) Groups(ctx context.Context) ([]Group, error) {
	if s == nil || s.Client == nil {
		return nil, errors.New("proxy control client unavailable")
	}
	var response struct {
		Proxies map[string]Group `json:"proxies"`
	}
	if err := s.Client.Get(ctx, "/proxies", &response); err != nil {
		return nil, err
	}
	groups := make([]Group, 0)
	for name, group := range response.Proxies {
		if len(group.All) == 0 {
			continue
		}
		group.Name = name
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

func (s *Service) Show(ctx context.Context, group string) (Group, error) {
	var result Group
	if s == nil || s.Client == nil {
		return result, errors.New("proxy control client unavailable")
	}
	if group == "" {
		return result, errors.New("proxy group required")
	}
	if err := s.Client.Get(ctx, "/proxies/"+url.PathEscape(group), &result); err != nil {
		return result, err
	}
	if result.Name == "" {
		result.Name = group
	}
	return result, nil
}

func (s *Service) Select(ctx context.Context, group, node string) error {
	if node == "" {
		return errors.New("proxy node required")
	}
	g, err := s.Show(ctx, group)
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range g.All {
		if candidate == node {
			found = true
			break
		}
	}
	if !found {
		return errors.New("node is not in proxy group")
	}
	return s.Client.Put(ctx, "/proxies/"+url.PathEscape(group), map[string]string{"name": node}, nil)
}

func (s *Service) Connections(ctx context.Context) ([]Connection, error) {
	if s == nil || s.Client == nil {
		return nil, errors.New("proxy control client unavailable")
	}
	var response struct {
		Connections []Connection `json:"connections"`
	}
	if err := s.Client.Get(ctx, "/connections", &response); err != nil {
		return nil, err
	}
	if response.Connections == nil {
		return []Connection{}, nil
	}
	return response.Connections, nil
}
