package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

type Client interface {
	Get(context.Context, string, any) error
	Put(context.Context, string, any, any) error
}

type extendedClient interface {
	Client
	Delete(context.Context, string, any) error
	Patch(context.Context, string, any, any) error
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

type DelayResult struct {
	Proxy     string `json:"proxy"`
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeout_ms"`
	DelayMS   int    `json:"delay_ms"`
}

func (s *Service) Delay(ctx context.Context, name, target string, timeoutMS int) (DelayResult, error) {
	var result DelayResult
	if s == nil || s.Client == nil {
		return result, errors.New("proxy control client unavailable")
	}
	if name == "" {
		return result, errors.New("proxy node required")
	}
	u := url.Values{}
	u.Set("url", target)
	u.Set("timeout", strconv.Itoa(timeoutMS))
	var response struct {
		Delay int `json:"delay"`
	}
	if err := s.Client.Get(ctx, "/proxies/"+url.PathEscape(name)+"/delay?"+u.Encode(), &response); err != nil {
		return result, err
	}
	result = DelayResult{Proxy: name, URL: target, TimeoutMS: timeoutMS, DelayMS: response.Delay}
	return result, nil
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

func (s *Service) Close(ctx context.Context, id string) error {
	c, ok := s.Client.(interface {
		Delete(context.Context, string, any) error
	})
	if !ok {
		return errors.New("mihomo control client does not support deleting connections")
	}
	path := "/connections"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	return c.Delete(ctx, path, nil)
}

func (s *Service) Mode(ctx context.Context, mode *string) (map[string]any, error) {
	if s == nil || s.Client == nil {
		return nil, errors.New("proxy control client unavailable")
	}
	if mode == nil {
		var cfg map[string]any
		if err := s.Client.Get(ctx, "/configs", &cfg); err != nil {
			return nil, err
		}
		value, _ := cfg["mode"].(string)
		return map[string]any{"mode": value}, nil
	}
	if _, ok := map[string]bool{"rule": true, "global": true, "direct": true}[*mode]; !ok {
		return nil, fmt.Errorf("invalid mode %q", *mode)
	}
	c, ok := s.Client.(interface {
		Patch(context.Context, string, any, any) error
	})
	if !ok {
		return nil, errors.New("mihomo control client does not support configuration updates")
	}
	if err := c.Patch(ctx, "/configs", map[string]string{"mode": *mode}, nil); err != nil {
		return nil, err
	}
	return map[string]any{"mode": *mode, "changed": true}, nil
}
