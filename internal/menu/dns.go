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
	return &Page{Title: "DNS", Help: []string{"dns"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: pageStatus(ctx, s, "DNS", []string{"dns", "status"}, "enabled", "policy", "tun_enabled") + "\ndns check inspects configuration and the mihomo API only; it is not an OS DNS leak test.", Actions: []Item{
			{Label: "Show DNS status", Args: []string{"dns", "status"}},
			{Label: "Check DNS configuration", Args: []string{"dns", "check"}},
			{Label: "Set direct DoH upstreams", Impact: "Saves and activates DNS policy", Action: dnsSet("direct")},
			{Label: "Set proxy DoH upstreams", Impact: "Saves and activates DNS policy", Action: dnsSet("proxy")},
			{Label: "Add DNS exception", Action: dnsExceptionAdd},
			{Label: "Remove DNS exception", Action: dnsExceptionRemove},
			{Label: "Enable TUN DNS interception", Args: []string{"dns", "tun", "on"}},
			{Label: "Disable TUN DNS interception", Args: []string{"dns", "tun", "off"}, Impact: "Disables TUN DNS interception in the selected profile"},
			{Label: "Query a domain", Action: dnsQuery},
			{Label: "Flush mihomo DNS cache", Args: []string{"dns", "flush"}},
		}}, nil
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
