package menu

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func init() { RegisterPage("dns", DNSPage) }

// DNSPage exposes the existing DNS commands without making claims about OS DNS capture.
func DNSPage() *Page {
	return &Page{Title: "DNS", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: pageStatus(ctx, s, "DNS", []string{"dns", "status"}, "enabled", "policy", "tun_enabled"), Actions: []Item{
			submenu(statePage("Servers", []string{"dns", "status"}, []string{"policy"}, Item{Label: "DNS details", Args: []string{"dns", "status"}},
				Item{Label: "Set direct DoH upstreams", Action: dnsSet("direct")},
				Item{Label: "Set proxy DoH upstreams", Action: dnsSet("proxy")})),
			submenu(actionPage("Domain Exceptions", Item{Label: "Add DNS exception", Action: dnsExceptionAdd},
				Item{Label: "Remove DNS exception", Action: dnsExceptionRemove})),
			submenu(statePage("TUN DNS Interception", []string{"dns", "status"}, []string{"tun_enabled"}, Item{Label: "Enable TUN DNS interception", Args: []string{"dns", "tun", "on"}},
				Item{Label: "Disable TUN DNS interception", Args: []string{"dns", "tun", "off"}})),
			submenu(actionPage("Checks & Queries", Item{Label: "Check DNS configuration", Args: []string{"dns", "check"}},
				Item{Label: "Query a domain", Action: dnsQuery},
				Item{Label: "Flush mihomo DNS cache", Args: []string{"dns", "flush"}})),
		}}
		return view, nil
	}}
}

func dnsSet(policy string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{}
		if policy == "proxy" {
			fields = append(fields, Field{Key: "node", Label: "Proxy node name", Required: true})
		}
		fields = append(fields, Field{Key: "urls", Label: "HTTPS DoH URLs (space separated)", Required: true, Validate: func(v string) error {
			for _, upstream := range strings.Fields(v) {
				u, err := url.Parse(upstream)
				if err != nil || u.Scheme != "https" || u.Host == "" {
					return fmt.Errorf("each DoH upstream must be an HTTPS URL")
				}
			}
			return nil
		}})
		_, err := s.Form(ctx, fields, nil, func(v map[string]string) Result {
			args := []string{"dns", "set", policy}
			if policy == "proxy" {
				args = append(args, v["node"])
			}
			return s.Run(ctx, append(args, strings.Fields(v["urls"])...)...)
		})
		return err
	}
}

func dnsExceptionAdd(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: "Domain suffix", Required: true}, {Key: "server", Label: "DNS server URL", Required: true}}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "dns", "exception", "add", v["domain"], v["server"])
	})
	return err
}

func dnsExceptionRemove(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: "Domain suffix", Required: true}}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "dns", "exception", "remove", v["domain"])
	})
	return err
}

func dnsQuery(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: "Domain name", Required: true}, {Key: "type", Label: "DNS record type", Choices: []string{"A", "AAAA"}}}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "dns", "query", v["domain"], v["type"])
	})
	return err
}
