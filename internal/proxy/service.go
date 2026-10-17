package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
)

type Client interface {
	Get(context.Context, string, any) error
	Put(context.Context, string, any, any) error
}

type Service struct {
	Client     Client
	GroupOrder []string
}
type Candidates []string

func NewService(client Client) *Service { return &Service{Client: client} }

type Group struct {
	Name           string               `json:"name"`
	NodeDelays     map[string]NodeDelay `json:"node_delays,omitempty"`
	Type           string               `json:"type"`
	Now            string               `json:"now,omitempty"`
	Fixed          string               `json:"fixed,omitempty"`
	All            []string             `json:"all,omitempty"`
	Alive          bool                 `json:"alive"`
	SelectedStatus string               `json:"selected_status,omitempty"`
	Hidden         bool                 `json:"hidden,omitempty"`
	Icon           string               `json:"icon,omitempty"`
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

func (s *Service) Connection(ctx context.Context, id string) (Connection, error) {
	var result Connection
	if s == nil || s.Client == nil {
		return result, errors.New("proxy control client unavailable")
	}
	if strings.TrimSpace(id) == "" {
		return result, errors.New("connection ID required")
	}
	connections, err := s.Connections(ctx)
	if err != nil {
		return result, err
	}
	for _, connection := range connections {
		if connection.ID == id {
			return connection, nil
		}
	}
	return result, errors.New("connection not found; run `nagi connections list` to see current IDs")
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
	Group                 string           `json:"group"`
	URL                   string           `json:"url"`
	TimeoutMS             int              `json:"timeout_ms"`
	Results               []BatchDelayItem `json:"results"`
	AutomaticReset        bool             `json:"automatic_reset,omitempty"`
	SavedSelectionRemoved bool             `json:"saved_selection_removed,omitempty"`
}

func (s *Service) Delays(ctx context.Context, group, target string, timeoutMS int) (BatchDelayResult, error) {
	g, err := s.ShowState(ctx, group)
	if err != nil {
		return BatchDelayResult{Group: group, URL: target, TimeoutMS: timeoutMS, Results: []BatchDelayItem{}}, err
	}
	result, _, err := s.DelaysKnown(ctx, g, target, timeoutMS)
	return result, err
}

// DelayRequestState records what is known about mihomo's fixed-node reset.
type DelayRequestState uint8

const (
	DelayNotSent DelayRequestState = iota
	DelayResetConfirmed
	DelayResetUnknown
)

// DelaysKnown uses a previously read group, so callers can make persistence
// decisions without a second group read between preflight and dispatch.
func (s *Service) DelaysKnown(ctx context.Context, g Group, target string, timeoutMS int) (BatchDelayResult, DelayRequestState, error) {
	result := BatchDelayResult{Group: g.Name, URL: target, TimeoutMS: timeoutMS, Results: []BatchDelayItem{}}
	if err := validateDelay(target, timeoutMS); err != nil {
		return result, DelayNotSent, err
	}
	u := delayQuery(target, timeoutMS)
	var measured map[string]int
	if err := s.Client.Get(ctx, "/group/"+url.PathEscape(g.Name)+"/delay?"+u.Encode(), &measured); err != nil {
		var apiErr *control.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 504 {
			return result, DelayResetConfirmed, err
		}
		return result, DelayResetUnknown, err
	}
	for _, name := range g.All {
		item := BatchDelayItem{Proxy: name}
		if delay, ok := measured[name]; ok {
			item.DelayMS = &delay
		} else {
			item.Error = "latency test failed"
		}
		result.Results = append(result.Results, item)
	}
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
	return result, DelayResetConfirmed, nil
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
	if err := validateDelay(target, timeoutMS); err != nil {
		return result, err
	}
	u := delayQuery(target, timeoutMS)
	path := "/proxies/" + url.PathEscape(name) + "/delay?" + u.Encode()
	if provider, ok, err := s.providerForNode(ctx, name); err != nil {
		return result, err
	} else if ok {
		path = "/providers/proxies/" + url.PathEscape(provider) + "/" + url.PathEscape(name) + "/healthcheck?" + u.Encode()
	}
	var response struct {
		Delay int `json:"delay"`
	}
	if err := s.Client.Get(ctx, path, &response); err != nil {
		return result, err
	}
	result = DelayResult{Proxy: name, URL: target, TimeoutMS: timeoutMS, DelayMS: response.Delay}
	return result, nil
}

func validateDelay(target string, timeoutMS int) error {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("URL must be an absolute HTTP(S) URL without userinfo")
	}
	if timeoutMS < 1 || timeoutMS > 30000 {
		return errors.New("timeout must be an integer from 1 to 30000 milliseconds")
	}
	return nil
}

func delayQuery(target string, timeoutMS int) url.Values {
	u := url.Values{}
	u.Set("url", target)
	u.Set("timeout", strconv.Itoa(timeoutMS))
	return u
}

// Auto clears mihomo's fixed node for an automatic group. Selector groups
// require a selection and cannot be reset through this endpoint.
func (s *Service) Auto(ctx context.Context, group string) error {
	g, err := s.ShowState(ctx, group)
	if err != nil {
		return err
	}
	if g.Type != "URLTest" && g.Type != "Fallback" {
		return errors.New("group is not automatic; choose a URLTest or Fallback group")
	}
	c, ok := s.Client.(interface {
		Delete(context.Context, string, any) error
	})
	if !ok {
		return errors.New("mihomo control client does not support DELETE")
	}
	return c.Delete(ctx, "/proxies/"+url.PathEscape(group), nil)
}

func (s *Service) Groups(ctx context.Context) ([]Group, error)      { return s.groups(ctx, true) }
func (s *Service) GroupsState(ctx context.Context) ([]Group, error) { return s.groups(ctx, false) }
func (s *Service) groups(ctx context.Context, delays bool) ([]Group, error) {
	if s == nil || s.Client == nil {
		return nil, errors.New("proxy control client unavailable")
	}
	var response struct {
		Proxies map[string]struct {
			Group
			History []historyRecord `json:"history"`
			Extra   map[string]struct {
				History []historyRecord `json:"history"`
			} `json:"extra"`
			TestURL string `json:"testUrl"`
		} `json:"proxies"`
	}
	if err := s.Client.Get(ctx, "/proxies", &response); err != nil {
		return nil, err
	}
	groups := make([]Group, 0)
	top := map[string]proxyHistory{}
	for name, raw := range response.Proxies {
		top[name] = proxyHistory{History: raw.History, Extra: raw.Extra}
	}
	reader := groupHistoryReader{top: top}
	for name, raw := range response.Proxies {
		group := raw.Group
		if len(group.All) == 0 {
			continue
		}
		group.Name = name
		group.SelectedStatus = selectedStatus(group)
		if delays {
			reader.decorate(ctx, s, &group, raw.TestURL)
		}
		groups = append(groups, group)
	}
	order := make(map[string]int, len(s.GroupOrder))
	for i, name := range s.GroupOrder {
		if _, exists := order[name]; !exists {
			order[name] = i
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		left, leftKnown := order[groups[i].Name]
		right, rightKnown := order[groups[j].Name]
		if leftKnown && rightKnown {
			return left < right
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return groups[i].Name < groups[j].Name
	})
	return groups, nil
}

func (s *Service) Show(ctx context.Context, group string) (Group, error) {
	return s.show(ctx, group, true)
}
func (s *Service) ShowState(ctx context.Context, group string) (Group, error) {
	return s.show(ctx, group, false)
}
func (s *Service) show(ctx context.Context, group string, delays bool) (Group, error) {
	var result Group
	if s == nil || s.Client == nil {
		return result, errors.New("proxy control client unavailable")
	}
	if group == "" {
		return result, errors.New("proxy group required")
	}
	var raw struct {
		Group
		TestURL string `json:"testUrl"`
	}
	if err := s.Client.Get(ctx, "/proxies/"+url.PathEscape(group), &raw); err != nil {
		return result, err
	}
	result = raw.Group
	if delays {
		var top struct {
			Proxies map[string]proxyHistory `json:"proxies"`
		}
		_ = s.Client.Get(ctx, "/proxies", &top)
		reader := groupHistoryReader{top: top.Proxies}
		reader.decorate(ctx, s, &result, raw.TestURL)
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
	g, err := s.ShowState(ctx, group)
	if err != nil {
		return err
	}
	_, err = s.selectKnown(ctx, g, node, false)
	return err
}

// LiveSelection returns a selector choice or an explicitly fixed automatic
// choice. Now on an unfixed automatic group is a computed result; writing it
// back would pin that group to a node.
func (g Group) LiveSelection() string {
	switch g.Type {
	case "Selector":
		return g.Now
	case "URLTest", "Fallback":
		return g.Fixed
	default:
		return ""
	}
}

// SelectKnown uses a fresh group snapshot instead of querying the same group
// again. mihomo validates membership when it handles the PUT as well.
func (s *Service) SelectKnown(ctx context.Context, g Group, node string) (bool, error) {
	return s.selectKnown(ctx, g, node, true)
}

func (s *Service) selectKnown(ctx context.Context, g Group, node string, skipCurrent bool) (bool, error) {
	if s == nil || s.Client == nil {
		return false, errors.New("proxy control client unavailable")
	}
	if g.Name == "" || node == "" {
		return false, errors.New("proxy group and node required")
	}
	found := false
	for _, candidate := range g.All {
		if candidate == node {
			found = true
			break
		}
	}
	if !found {
		return false, errors.New("node is unavailable or was removed from the proxy group; run `nagi proxy show GROUP` to see current nodes")
	}
	if skipCurrent && g.LiveSelection() == node {
		return false, nil
	}
	if err := s.Client.Put(ctx, "/proxies/"+url.PathEscape(g.Name), map[string]string{"name": node}, nil); err != nil {
		return false, err
	}
	return true, nil
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
