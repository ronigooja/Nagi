package menu

import (
	"context"
	"strings"
)

func init() { RegisterPage("dns", DNSPage) }

// DNSPage exposes the existing DNS commands without making claims about OS DNS capture.
func DNSPage() *Page {
	return &Page{Title: "DNS", Help: []string{"dns"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Context: "dns check inspects configuration and the mihomo API only; it is not an OS DNS leak test.", Items: []Item{
			{Label: "Show DNS status", Args: []string{"dns", "status"}},
			{Label: "Check DNS configuration", Args: []string{"dns", "check"}},
			{Label: "Set direct DoH upstreams", Action: dnsSet("direct")},
			{Label: "Set proxy DoH upstreams", Action: dnsSet("proxy")},
			{Label: "Add DNS exception", Action: dnsExceptionAdd},
			{Label: "Remove DNS exception", Action: dnsExceptionRemove},
			{Label: "Enable TUN DNS interception", Args: []string{"dns", "tun", "on"}},
			{Label: "Disable TUN DNS interception", Args: []string{"dns", "tun", "off"}, Confirm: "Disable TUN DNS interception in the selected profile?"},
			{Label: "Query a domain", Action: dnsQuery},
			{Label: "Flush mihomo DNS cache", Args: []string{"dns", "flush"}},
		}}, nil
	}}
}

func dnsSet(policy string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		args := []string{"dns", "set", policy}
		if policy == "proxy" {
			node, err := s.Prompt("Proxy node name")
			if err != nil {
				return err
			}
			if strings.TrimSpace(node) == "" {
				return nil
			}
			args = append(args, node)
		}
		urls, err := s.Prompt("HTTPS DoH URLs (space separated)")
		if err != nil {
			return err
		}
		upstreams := strings.Fields(urls)
		if len(upstreams) == 0 {
			return nil
		}
		args = append(args, upstreams...)
		s.Run(ctx, args...)
		return nil
	}
}

func dnsExceptionAdd(ctx context.Context, s *Session) error {
	domain, err := s.Prompt("Domain suffix")
	if err != nil {
		return err
	}
	if strings.TrimSpace(domain) == "" {
		return nil
	}
	server, err := s.Prompt("DNS server URL")
	if err != nil {
		return err
	}
	if strings.TrimSpace(server) == "" {
		return nil
	}
	s.Run(ctx, "dns", "exception", "add", domain, server)
	return nil
}

func dnsExceptionRemove(ctx context.Context, s *Session) error {
	domain, err := s.Prompt("Domain suffix")
	if err != nil {
		return err
	}
	if strings.TrimSpace(domain) == "" {
		return nil
	}
	s.Run(ctx, "dns", "exception", "remove", domain)
	return nil
}

func dnsQuery(ctx context.Context, s *Session) error {
	domain, err := s.Prompt("Domain name")
	if err != nil {
		return err
	}
	if strings.TrimSpace(domain) == "" {
		return nil
	}
	typ, err := s.Select("DNS record type", []string{"A", "AAAA"})
	if err != nil {
		return err
	}
	s.Run(ctx, "dns", "query", domain, typ)
	return nil
}
