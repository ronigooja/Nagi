package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func init() { RegisterPage("dns", DNSPage) }

// DNSPage exposes the existing DNS commands without making claims about OS DNS capture.
func localizedDNSPage(language i18n.Context) *Page {
	return &Page{ID: "dns.DNSPage.page1", Title: "DNS", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: pageStatus(ctx, s, "DNS", []string{"dns", "status"}, "enabled", "policy", "tun_enabled"), Actions: []Item{
			localizedsubmenu(language, localizedstatePage(language, menuText(language, "menu.dns.d_n_s_page.servers", "Servers"), []string{"dns", "status"}, []string{"policy"}, Item{ID: "dns.DNSPage.item2", Label: menuText(language, "menu.dns.details", "DNS details"), Args: []string{"dns", "status"}},
				Item{ID: "dns.DNSPage.item3", Label: menuText(language, "menu.dns.d_n_s_page.set_direct_doh_upstreams", "Set direct DoH upstreams"), Action: localizeddnsSet(language, "direct")},
				Item{ID: "dns.DNSPage.item4", Label: menuText(language, "menu.dns.d_n_s_page.set_proxy_doh_upstreams", "Set proxy DoH upstreams"), Action: localizeddnsSet(language, "proxy")})),
			localizedsubmenu(language, localizedactionPage(language, menuText(language, "menu.dns.d_n_s_page.domain_exceptions", "Domain Exceptions"), Item{ID: "dns.DNSPage.item5", Label: menuText(language, "menu.dns.d_n_s_page.add_dns_exception", "Add DNS exception"), Action: dnsExceptionAdd},
				Item{ID: "dns.DNSPage.item6", Label: menuText(language, "menu.dns.d_n_s_page.remove_dns_exception", "Remove DNS exception"), Action: dnsExceptionRemove})),
			localizedsubmenu(language, localizedstatePage(language, menuText(language, "menu.dns.d_n_s_page.tun_dns_interception", "TUN DNS Interception"), []string{"dns", "status"}, []string{"tun_enabled"}, Item{ID: "dns.DNSPage.item7", Label: menuText(language, "menu.dns.d_n_s_page.enable_tun_dns_interception", "Enable TUN DNS interception"), Args: []string{"dns", "tun", "on"}},
				Item{ID: "dns.DNSPage.item8", Label: menuText(language, "menu.dns.d_n_s_page.disable_tun_dns_interception", "Disable TUN DNS interception"), Args: []string{"dns", "tun", "off"}})),
			localizedsubmenu(language, localizedactionPage(language, menuText(language, "menu.dns.d_n_s_page.checks_queries", "Checks & Queries"), Item{ID: "dns.DNSPage.item9", Label: menuText(language, "menu.dns.d_n_s_page.check_dns_configuration", "Check DNS configuration"), Args: []string{"dns", "check"}},
				Item{ID: "dns.DNSPage.item10", Label: menuText(language, "menu.dns.d_n_s_page.query_a_domain", "Query a domain"), Action: dnsQuery},
				Item{ID: "dns.DNSPage.item11", Label: menuText(language, "menu.dns.d_n_s_page.flush_mihomo_dns_cache", "Flush mihomo DNS cache"), Args: []string{"dns", "flush"}})),
		}}
		return view, nil
	}}
}

func localizeddnsSet(language i18n.Context, policy string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{}
		if policy == "proxy" {
			fields = append(fields, Field{Key: "node", Label: menuText(language, "menu.dns.dns_set.proxy_node_name", "Proxy node name"), Required: true})
		}
		fields = append(fields, Field{Key: "urls", Label: menuText(language, "menu.dns.dns_set.https_doh_urls_space_separated", "HTTPS DoH URLs (space separated)"), Required: true, Validate: func(v string) error {
			for _, upstream := range strings.Fields(v) {
				u, err := url.Parse(upstream)
				if err != nil || u.Scheme != "https" || u.Host == "" {
					return fmt.Errorf("%s", menuText(language, "menu.dns.dns_set.each_doh_upstream_must_be_an_https_url", "each DoH upstream must be an HTTPS URL"))
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
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: menuText(s.language, "menu.dns.dns_exception_add.domain_suffix", "Domain suffix"), Required: true}, {Key: "server", Label: menuText(s.language, "menu.dns.server_url", "DNS server URL"), Required: true}}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "dns", "exception", "add", v["domain"], v["server"])
	})
	return err
}

func dnsExceptionRemove(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: menuText(s.language, "menu.dns.dns_exception_remove.domain_suffix", "Domain suffix"), Required: true}}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "dns", "exception", "remove", v["domain"])
	})
	return err
}

func dnsQuery(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: menuText(s.language, "menu.dns.dns_query.domain_name", "Domain name"), Required: true}, {Key: "type", Label: menuText(s.language, "menu.dns.dns_query.dns_record_type", "DNS record type"), Choices: []string{"A", "AAAA"}, ChoiceLabels: []string{menuState(s.language, "A"), menuState(s.language, "AAAA")}}}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "dns", "query", v["domain"], v["type"])
	})
	return err
}
func DNSPage() *Page {
	return localizedDNSPage(i18n.New(i18n.English))
}
func dnsSet(policy string) func(context.
	Context, *Session) error {
	return localizeddnsSet(i18n.New(i18n.English), policy)
}
