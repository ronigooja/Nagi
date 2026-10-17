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
	Name    string          `json:"name"`
	History []historyRecord `json:"history"`
	Extra   map[string]struct {
		History []historyRecord `json:"history"`
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
	if latest.Delay > 0 {
		d.Status = "ok"
		v := latest.Delay
		d.DelayMS = &v
	}
	return d
}

// groupHistoryReader caches the provider snapshot for an entire group listing.
type groupHistoryReader struct {
	top       map[string]proxyHistory
	providers map[string][]proxyHistory
	loaded    bool
}

func (r *groupHistoryReader) decorate(ctx context.Context, s *Service, g *Group, target string) {
	g.NodeDelays = map[string]NodeDelay{}
	for _, name := range g.All {
		p, ok := r.top[name]
		reason := "history unavailable"
		if !ok && r.top != nil {
			if !r.loaded {
				r.loaded = true
				var response struct {
					Providers map[string]struct {
						Proxies []proxyHistory `json:"proxies"`
					} `json:"providers"`
				}
				if s.Client.Get(ctx, "/providers/proxies", &response) == nil {
					r.providers = map[string][]proxyHistory{}
					for key, v := range response.Providers {
						r.providers[key] = v.Proxies
					}
				}
			}
			count := 0
			for _, nodes := range r.providers {
				for _, node := range nodes {
					if node.Name == name {
						p = node
						count++
					}
				}
			}
			ok = count == 1
			if count > 1 {
				reason = "ambiguous node ownership"
			}
		}
		if ok {
			g.NodeDelays[name] = latestDelay(p, target)
		} else {
			g.NodeDelays[name] = NodeDelay{Status: "untested", Reason: reason}
		}
	}
}
