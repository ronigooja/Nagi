package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Client interface {
	Get(context.Context, string, any) error
	Put(context.Context, string, any, any) error
}

type Service struct{ Client Client }
type Candidates []string

func NewService(client Client) *Service { return &Service{Client: client} }

type Group struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Now            string   `json:"now,omitempty"`
	All            []string `json:"all,omitempty"`
	Alive          bool     `json:"alive"`
	SelectedStatus string   `json:"selected_status,omitempty"`
}

type Connection struct {
	ID          string         `json:"id"`
	Rule        string         `json:"rule,omitempty"`
	RulePayload string         `json:"rulePayload,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Chains      []string       `json:"chains,omitempty"`
	Upload      int64          `json:"upload"`
	Download    int64          `json:"download"`
}

type DelayResult struct {
	Proxy     string `json:"proxy"`
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeout_ms"`
	DelayMS   int    `json:"delay_ms"`
}

type BatchDelayItem struct {
	Proxy   string `json:"proxy"`
	DelayMS *int   `json:"delay_ms,omitempty"`
	Error   string `json:"error,omitempty"`
}
type BatchDelayResult struct {
	Group     string           `json:"group"`
	URL       string           `json:"url"`
	TimeoutMS int              `json:"timeout_ms"`
	Results   []BatchDelayItem `json:"results"`
}

func (s *Service) Delays(ctx context.Context, group, target string, timeoutMS int) (BatchDelayResult, error) {
	result := BatchDelayResult{Group: group, URL: target, TimeoutMS: timeoutMS, Results: []BatchDelayItem{}}
	g, err := s.Show(ctx, group)
	if err != nil {
		return result, err
	}
	if len(g.All) == 0 {
		return result, nil
	}
	result.Results = make([]BatchDelayItem, len(g.All))
	semaphore := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, name := range g.All {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				result.Results[i] = BatchDelayItem{Proxy: name, Error: "request canceled"}
				return
			}
			defer func() { <-semaphore }()
			delay, err := s.Delay(ctx, name, target, timeoutMS)
			item := BatchDelayItem{Proxy: name}
			if err != nil {
				item.Error = "latency test failed"
			} else {
				item.DelayMS = &delay.DelayMS
			}
			result.Results[i] = item
		}(i, name)
	}
	wg.Wait()
	sort.SliceStable(result.Results, func(i, j int) bool {
		a, b := result.Results[i], result.Results[j]
		if a.Error != "" {
			return false
		}
		if b.Error != "" {
			return true
		}
		if *a.DelayMS != *b.DelayMS {
			return *a.DelayMS < *b.DelayMS
		}
		return a.Proxy < b.Proxy
	})
	return result, nil
}

func (s *Service) Search(ctx context.Context, query string) ([]Group, error) {
	groups, err := s.Groups(ctx)
	if err != nil {
		return nil, err
	}
	found := make([]Group, 0)
	for _, g := range groups {
		nodes := make([]string, 0)
		for _, name := range g.All {
			if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
				nodes = append(nodes, name)
			}
		}
		if len(nodes) > 0 {
			g.All = nodes
			found = append(found, g)
		}
	}
	return found, nil
}

func (s *Service) Delay(ctx context.Context, name, target string, timeoutMS int) (DelayResult, error) {
	var result DelayResult
	if s == nil || s.Client == nil {
		return result, errors.New("proxy control client unavailable")
	}
	if strings.TrimSpace(name) == "" {
		return result, errors.New("proxy node required")
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return result, errors.New("URL must be an absolute HTTP(S) URL without userinfo")
	}
	if timeoutMS < 1 || timeoutMS > 30000 {
		return result, errors.New("timeout must be an integer from 1 to 30000 milliseconds")
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
		group.SelectedStatus = selectedStatus(group)
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
	result.SelectedStatus = selectedStatus(result)
	return result, nil
}

func selectedStatus(g Group) string {
	if g.Now == "" {
		return "none"
	}
	for _, node := range g.All {
		if node == g.Now {
			if !g.Alive {
				return "unavailable"
			}
			return "available"
		}
	}
	return "removed"
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
		return errors.New("node is unavailable or was removed from the proxy group; run `nagi proxy show GROUP` to see current nodes")
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
	if s == nil || s.Client == nil {
		return errors.New("proxy control client unavailable")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("connection ID required")
	}
	c, ok := s.Client.(interface {
		Delete(context.Context, string, any) error
	})
	if !ok {
		return errors.New("mihomo control client does not support deleting connections")
	}
	path := "/connections/" + url.PathEscape(id)
	return c.Delete(ctx, path, nil)
}

func (s *Service) CloseAll(ctx context.Context) error {
	if s == nil || s.Client == nil {
		return errors.New("proxy control client unavailable")
	}
	c, ok := s.Client.(interface {
		Delete(context.Context, string, any) error
	})
	if !ok {
		return errors.New("mihomo control client does not support deleting connections")
	}
	return c.Delete(ctx, "/connections", nil)
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
		if value != "rule" && value != "global" && value != "direct" {
			return nil, errors.New("mihomo returned an invalid mode")
		}
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
