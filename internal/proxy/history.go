package proxy

import (
	"context"
	"net/url"
	"time"
)

// NodeDelay is the most recent native mihomo observation, never a new probe.
type NodeDelay struct {
	Status     string `json:"status"`
	DelayMS    *int   `json:"delay_ms,omitempty"`
	MeasuredAt string `json:"measured_at,omitempty"`
	TestURL    string `json:"test_url,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
type historyRecord struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}
type proxyHistory struct {
	Group
	TestURL string          `json:"testUrl"`
	Name    string          `json:"name"`
	History []historyRecord `json:"history"`
	Extra   map[string]struct {
		History []historyRecord `json:"history"`
		Alive   *bool           `json:"alive"`
	} `json:"extra"`
}

func publicTestURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
func latestDelay(p proxyHistory, target string) NodeDelay {
	d := NodeDelay{Status: "untested"}
	records := p.History
	zeroAlive := false
	// Native per-URL records preserve expected-status failures. Choose the
	// newest observation across URLs; the configured URL breaks timestamp ties.
	if len(p.Extra) > 0 {
		records = nil
		var newest time.Time
		chosen := ""
		for candidate, state := range p.Extra {
			for _, r := range state.History {
				if chosen == "" || r.Time.After(newest) || r.Time.Equal(newest) && (candidate == target || chosen != target && candidate < chosen) {
					newest = r.Time
					chosen = candidate
					records = state.History
				}
			}
		}
		d.TestURL = publicTestURL(chosen)
		state := p.Extra[chosen]
		zeroAlive = state.Alive != nil && *state.Alive
	}

	var latest *historyRecord
	for i := range records {
		if latest == nil || !records[i].Time.Before(latest.Time) {
			latest = &records[i]
		}
	}
	if latest == nil {
		return d
	}
	if !latest.Time.IsZero() {
		d.MeasuredAt = latest.Time.Format(time.RFC3339Nano)
	}
	d.Status = "failed"
	if latest.Delay > 0 || latest.Delay == 0 && zeroAlive {
		d.Status = "ok"
		v := latest.Delay
		d.DelayMS = &v
	}
	return d
}

// NodeInfo identifies selectable entries independently of display names.
type NodeInfo struct {
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
}
type groupHistoryReader struct {
	top       map[string]proxyHistory
	providers map[string][]proxyHistory
	loaded    bool
}

func (s *Service) historyReader(ctx context.Context) (*groupHistoryReader, error) {
	var response struct {
		Proxies map[string]proxyHistory `json:"proxies"`
	}
	if err := s.Client.Get(ctx, "/proxies", &response); err != nil {
		return nil, err
	}
	return &groupHistoryReader{top: response.Proxies}, nil
}
func (r *groupHistoryReader) providerNodes(ctx context.Context, s *Service) map[string][]proxyHistory {
	if !r.loaded {
		r.loaded = true
		var response struct {
			Providers map[string]struct {
				Proxies []proxyHistory `json:"proxies"`
			} `json:"providers"`
		}
		if s.Client.Get(ctx, "/providers/proxies", &response) == nil {
			r.providers = map[string][]proxyHistory{}
			for k, v := range response.Providers {
				r.providers[k] = v.Proxies
			}
		}
	}
	return r.providers
}
func (r *groupHistoryReader) resolve(ctx context.Context, s *Service, group, name string) (proxyHistory, string, bool) {
	p, topOK := r.top[name]
	cfg, known := s.groupConfig(group)
	topAllowed := !known
	providerAllowed := map[string]bool{}
	if known {
		topAllowed = cfg.IncludeAll || cfg.IncludeAllProxies
		for _, n := range cfg.Proxies {
			if n == name {
				topAllowed = true
			}
		}
		for _, n := range cfg.Use {
			providerAllowed[n] = true
		}
	}
	// Without source configuration, accept only ownership unique across snapshots.
	count := 0
	provider := ""
	if topOK && topAllowed {
		count++
	}
	for key, nodes := range r.providerNodes(ctx, s) {
		if key == "COMPATIBLE" {
			continue
		}
		if known && !providerAllowed[key] && !cfg.IncludeAll && !cfg.IncludeAllProviders {
			continue
		}
		for _, node := range nodes {
			if node.Name == name {
				p = node
				provider = key
				count++
			}
		}
	}
	return p, provider, count == 1
}
func nodeKind(name string, p proxyHistory) string {
	if name == "DIRECT" || name == "REJECT" || name == "REJECT-DROP" || name == "PASS" || name == "COMPATIBLE" {
		return "builtin"
	}
	if len(p.All) > 0 || p.Type == "Selector" || p.Type == "URLTest" || p.Type == "Fallback" || p.Type == "LoadBalance" || p.Type == "Relay" {
		return "group"
	}
	return "node"
}
func delayStatus(d NodeDelay) string {
	if d.Status == "ok" {
		return "available"
	}
	if d.Status == "failed" {
		return "unavailable"
	}
	return ""
}
func (r *groupHistoryReader) leafStatus(ctx context.Context, s *Service, name string, seen map[string]bool) string {
	if seen[name] {
		return ""
	}
	seen[name] = true
	p, ok := r.top[name]
	if !ok || p.Type == "LoadBalance" || p.Now == "" {
		return ""
	}
	member := false
	for _, n := range p.All {
		if n == p.Now {
			member = true
		}
	}
	if !member {
		return ""
	}
	child, provider, ok := r.resolve(ctx, s, name, p.Now)
	if !ok {
		return ""
	}
	switch scopedNodeKind(p.Now, child, provider) {
	case "node":
		return delayStatus(latestDelay(child, p.TestURL))
	case "group":
		return r.leafStatus(ctx, s, p.Now, seen)
	}
	return ""
}
func (r *groupHistoryReader) decorate(ctx context.Context, s *Service, g *Group, target string) {
	g.NodeDelays = map[string]NodeDelay{}
	g.NodeInfo = map[string]NodeInfo{}
	for _, name := range g.All {
		p, provider, ok := r.resolve(ctx, s, g.Name, name)
		if !ok {
			g.NodeInfo[name] = NodeInfo{Kind: "unknown"}
			g.NodeDelays[name] = NodeDelay{Status: "untested", Reason: "history unavailable or ambiguous node ownership"}
			continue
		}
		info := NodeInfo{Kind: scopedNodeKind(name, p, provider)}
		switch info.Kind {
		case "node":
			d := latestDelay(p, target)
			g.NodeDelays[name] = d
			info.Status = delayStatus(d)
		case "group":
			info.Status = r.leafStatus(ctx, s, name, map[string]bool{})
		}
		g.NodeInfo[name] = info
	}
}

func scopedNodeKind(name string, p proxyHistory, provider string) string {
	if provider != "" && len(p.All) == 0 {
		return "node"
	}
	return nodeKind(name, p)
}
