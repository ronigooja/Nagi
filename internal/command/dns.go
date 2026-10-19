package command

import (
	"context"
	"errors"
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"gopkg.in/yaml.v3"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
)

var dnsDomain = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
var dnsBootstrap = []string{"https://1.12.12.12/dns-query", "https://120.53.53.53/dns-query"}

func dnsBootstrapURL(raw string) error {
	if err := dnsURL(raw, false); err != nil {
		return err
	}
	parsed, _ := url.Parse(raw)
	if net.ParseIP(parsed.Hostname()) == nil {
		return messageError("cli.error.dns.bootstrap_doh_url_must_use_an_ip_address_to_avoid_a_dns_resolution_c", "bootstrap DoH URL must use an IP address to avoid a DNS resolution cycle")
	}
	return nil
}

func dnsURL(raw string, allowPlain bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return messageError("cli.error.dns.dns_server_must_be_a_url_without_credentials_query_or_fragment", "DNS server must be a URL without credentials, query, or fragment")
	}
	if u.Scheme == "https" && u.Path != "" {
		return nil
	}
	if allowPlain && (u.Scheme == "udp" || u.Scheme == "tcp") && u.Path == "" {
		host := u.Hostname()
		if net.ParseIP(host) != nil {
			return nil
		}
	}
	return messageError("cli.error.dns.dns_server_must_be_an_https_doh_url_explicit_exceptions_may_use_udp_", "DNS server must be an HTTPS DoH URL; explicit exceptions may use udp://IP:53 or tcp://IP:53")
}

func dnsOverride(store *profile.Store, name string) (map[string]any, error) {
	data, err := store.Override(name)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func dnsSection(override map[string]any) map[string]any {
	if existing, ok := override["dns"].(map[string]any); ok {
		return existing
	}
	section := map[string]any{}
	override["dns"] = section
	return section
}

func saveDNS(ctx context.Context, store *profile.Store, name string, override map[string]any) error {
	data, err := yaml.Marshal(override)
	if err != nil {
		return err
	}
	return store.SetOverride(ctx, name, data)
}

func dnsCommand(ctx context.Context, args []string, store *profile.Store, name string, client *control.Client) (any, error) {
	if len(args) < 2 {
		return nil, usage("dns status|set direct URL [URL...]|set proxy NODE URL [URL...]|exception add DOMAIN SERVER|exception remove DOMAIN|tun on|off|query DOMAIN [A|AAAA]|flush|check")
	}
	switch args[1] {
	case "status", "check":
		if len(args) != 2 {
			return nil, usage("dns " + args[1])
		}
		data, err := store.Effective(name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		var cfg map[string]any
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fail("dns_error", messageError("cli.error.dns.selected_profile_is_not_valid_yaml", "selected profile is not valid YAML"))
		}
		dns, _ := cfg["dns"].(map[string]any)
		tun, _ := cfg["tun"].(map[string]any)
		servers := stringList(dns["nameserver"])
		defaultServers := stringList(dns["default-nameserver"])
		issues := []string{}
		messages := map[string]i18n.Descriptor{}
		addIssue := func(message *i18n.Message) {
			messages[fmt.Sprintf("/issues/%d", len(issues))] = message.Descriptor()
			issues = append(issues, message.Error())
		}
		if dns["enable"] != true {
			addIssue(i18n.Errorf("backend.dns.issue.mihomo_dns_is_disabled", "mihomo DNS is disabled"))
		}
		if !encryptedServers(servers) || !encryptedServers(defaultServers) {
			addIssue(i18n.Errorf("backend.dns.issue.primary_or_bootstrap_dns_includes_a_non_doh_server", "primary or bootstrap DNS includes a non-DoH server"))
		}
		for _, key := range []string{"fallback", "proxy-server-nameserver", "direct-nameserver"} {
			if values := stringList(dns[key]); len(values) > 0 && !encryptedServers(values) {
				addIssue(i18n.NewMessage("backend.dns.issue.non_doh", map[string]any{"field": key}, key+" includes a non-DoH server", nil))
			}
		}
		if policy, ok := dns["nameserver-policy"].(map[string]any); ok {
			for domain, servers := range policy {
				if !encryptedServers(stringList(servers)) {
					addIssue(i18n.NewMessage("backend.dns.issue.plaintext_exception", map[string]any{"domain": domain}, "explicit plaintext exception: "+domain, nil))
				}
			}
		}
		if err := validateDNSProxyTargets(data); err != nil {
			var message *i18n.Message
			if errors.As(err, &message) {
				addIssue(message)
			} else {
				issues = append(issues, err.Error())
			}
		}
		hijack := stringList(tun["dns-hijack"])
		if tun["enable"] != true || tun["auto-route"] != true || tun["strict-route"] != true || !containsDNS(hijack, "any:53") || !containsDNS(hijack, "tcp://any:53") {
			addIssue(i18n.Errorf("backend.dns.issue.tun_routing_and_dns_interception_are_not_fully_configured", "TUN routing and DNS interception are not fully configured"))
		}
		policy := "direct"
		proxied, ruled := 0, 0
		for _, server := range servers {
			if parsed, err := url.Parse(server); err == nil && parsed.Fragment != "" && parsed.Fragment != "DIRECT" {
				if parsed.Fragment == "RULES" {
					ruled++
				} else {
					proxied++
				}
			}
		}
		if proxied == len(servers) && len(servers) > 0 {
			policy = "proxy"
		} else if ruled == len(servers) && len(servers) > 0 {
			policy = "rules"
		} else if proxied+ruled > 0 {
			policy = "mixed"
			addIssue(i18n.Errorf("backend.dns.issue.dns_upstreams_use_mixed_routing_policies", "DNS upstreams use mixed routing policies"))
		} else if dns["respect-rules"] == true {
			policy = "rules"
		}
		result := map[string]any{"profile": name, "enabled": dns["enable"] == true, "ipv6": dns["ipv6"] == true, "resolve_proxy_destinations": dns["resolve-proxy-destinations"] == true, "policy": policy, "upstream_hosts": dnsHosts(servers), "tun_enabled": tun["enable"] == true, "issues": issues}
		if args[1] == "check" {
			result["scope"] = "configuration and mihomo API"
			messages["/scope"] = i18n.Descriptor{ID: "backend.dns.check.scope"}
			var response map[string]any
			if err := client.Get(ctx, "/dns/query?name=example.com&type=A", &response); err == nil {
				result["engine_query_ok"] = true
			} else {
				result["engine_query_ok"] = false
				addIssue(i18n.Errorf("backend.dns.issue.mihomo_dns_query_unavailable", "mihomo DNS query unavailable"))
			}
			result["issues"] = issues
		}
		return i18n.Result{Value: result, Messages: messages}, nil
	case "set":
		if len(args) < 4 {
			return nil, usage("dns set direct URL [URL...]|proxy NODE URL [URL...]")
		}
		policy := args[2]
		if policy != "direct" && policy != "proxy" {
			return nil, usage("dns set direct URL [URL...]|proxy NODE URL [URL...]")
		}
		group := ""
		firstURL := 3
		if policy == "proxy" {
			if len(args) < 5 {
				return nil, usage("dns set proxy NODE URL [URL...]")
			}
			group = args[3]
			firstURL = 4
			if err := dnsProxyNode(store, name, group); err != nil {
				return nil, fail("dns_error", err)
			}
		}
		serverArgs := args[firstURL:]
		bootstrap := append([]string(nil), dnsBootstrap...)
		for i, arg := range serverArgs {
			if arg != "--bootstrap" {
				continue
			}
			if i == 0 || i == len(serverArgs)-1 {
				return nil, usage("dns set direct URL [URL...] [--bootstrap IP_DOH_URL ...]|proxy NODE URL [URL...] [--bootstrap IP_DOH_URL ...]")
			}
			bootstrap = append([]string(nil), serverArgs[i+1:]...)
			serverArgs = serverArgs[:i]
			break
		}
		servers := append([]string(nil), serverArgs...)
		for _, server := range bootstrap {
			if err := dnsBootstrapURL(server); err != nil {
				return nil, fail("dns_error", err)
			}
		}
		for _, server := range servers {
			if err := dnsURL(server, false); err != nil {
				return nil, fail("dns_error", err)
			}
		}
		override, err := dnsOverride(store, name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		previousDNS := dnsSection(override)
		dns := profile.DNSSettings(serverArgs, bootstrap, group)
		// Keep deliberate local domain exceptions; generated defaults start empty.
		if policy, ok := previousDNS["nameserver-policy"]; ok {
			dns["nameserver-policy"] = policy
		}
		override["dns"] = dns
		override[profile.DNSReplaceKey] = true
		delete(override, profile.DNSProxyGroupKey)
		override["ipv6"] = true
		if err := saveDNS(ctx, store, name, override); err != nil {
			return nil, fail("dns_error", err)
		}
		return map[string]any{"profile": name, "policy": policy, "proxy_node": group, "upstream_count": len(servers), "saved": true}, nil
	case "exception":
		if len(args) < 4 || (args[2] != "add" && args[2] != "remove") {
			return nil, usage("dns exception add DOMAIN SERVER|remove DOMAIN")
		}
		if (args[2] == "add" && len(args) != 5) || (args[2] == "remove" && len(args) != 4) {
			return nil, usage("dns exception add DOMAIN SERVER|remove DOMAIN")
		}
		domain := strings.ToLower(args[3])
		if !dnsDomain.MatchString(domain) {
			return nil, fail("dns_error", messageError("cli.error.dns.exception_domain_must_be_a_dns_suffix_such_as_lan_or_corp_example", "exception domain must be a DNS suffix such as lan or corp.example"))
		}
		key := "+." + domain
		override, err := dnsOverride(store, name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		dns := dnsSection(override)
		if dns["enable"] != true || len(stringList(dns["nameserver"])) == 0 {
			return nil, fail("dns_error", messageError("cli.error.dns.run_nagi_dns_set_direct_url_or_nagi_dns_set_proxy_node_url_first", "run `nagi dns set direct URL` or `nagi dns set proxy NODE URL` first"))
		}
		policy, _ := dns["nameserver-policy"].(map[string]any)
		if policy == nil {
			policy = map[string]any{}
		}
		if args[2] == "add" {
			if err := dnsURL(args[4], true); err != nil {
				return nil, fail("dns_error", err)
			}
			policy[key] = []string{args[4]}
		} else {
			delete(policy, key)
		}
		dns["nameserver-policy"] = policy
		if err := saveDNS(ctx, store, name, override); err != nil {
			return nil, fail("dns_error", err)
		}
		return map[string]any{"profile": name, "domain": domain, "exception_changed": true}, nil
	case "tun":
		if len(args) != 3 || (args[2] != "on" && args[2] != "off") {
			return nil, usage("dns tun on|off")
		}
		if args[2] == "on" {
			data, err := store.Effective(name)
			if err != nil {
				return nil, fail("dns_error", err)
			}
			var cfg struct {
				DNS struct {
					Enable            bool     `yaml:"enable"`
					NameServer        []string `yaml:"nameserver"`
					DefaultNameserver []string `yaml:"default-nameserver"`
				} `yaml:"dns"`
			}
			if err := yaml.Unmarshal(data, &cfg); err != nil || !cfg.DNS.Enable || !encryptedServers(cfg.DNS.NameServer) || !encryptedServers(cfg.DNS.DefaultNameserver) {
				return nil, fail("dns_error", messageError("cli.error.dns.configure_encrypted_dns_with_nagi_dns_set_before_enabling_tun_interc", "configure encrypted DNS with `nagi dns set` before enabling TUN interception"))
			}
		}
		override, err := dnsOverride(store, name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		if args[2] == "on" {
			override["tun"] = map[string]any{"enable": true, "stack": "system", "auto-route": true, "strict-route": true, "auto-detect-interface": true, "dns-hijack": []string{"any:53", "tcp://any:53"}}
		} else {
			override["tun"] = map[string]any{"enable": false}
		}
		if err := saveDNS(ctx, store, name, override); err != nil {
			return nil, fail("dns_error", err)
		}
		return map[string]any{"profile": name, "tun_enabled": args[2] == "on", "saved": true}, nil
	case "query":
		if len(args) < 3 || len(args) > 4 {
			return nil, usage("dns query DOMAIN [A|AAAA]")
		}
		if !dnsDomain.MatchString(args[2]) || !strings.Contains(args[2], ".") {
			return nil, usage("dns query DOMAIN [A|AAAA]")
		}
		qtype := "A"
		if len(args) == 4 {
			qtype = args[3]
		}
		if qtype != "A" && qtype != "AAAA" {
			return nil, usage("dns query DOMAIN [A|AAAA]")
		}
		var response map[string]any
		if err := client.Get(ctx, "/dns/query?name="+url.QueryEscape(args[2])+"&type="+qtype, &response); err != nil {
			return nil, fail("dns_error", err)
		}
		return map[string]any{"domain": args[2], "type": qtype, "response": response}, nil
	case "flush":
		if len(args) != 2 {
			return nil, usage("dns flush")
		}
		if err := client.Post(ctx, "/cache/dns/flush", nil, nil); err != nil {
			return nil, fail("dns_error", err)
		}
		return map[string]any{"flushed": true}, nil
	}
	return nil, usage("dns status|set direct URL [URL...]|set proxy NODE URL [URL...]|exception add DOMAIN SERVER|exception remove DOMAIN|tun on|off|query DOMAIN [A|AAAA]|flush|check")
}

func stringList(value any) []string {
	if list, ok := value.([]string); ok {
		return list
	}
	if list, ok := value.([]any); ok {
		result := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	if s, ok := value.(string); ok {
		return []string{s}
	}
	return nil
}

func encryptedServers(servers []string) bool {
	if len(servers) == 0 {
		return false
	}
	for _, server := range servers {
		parsed, err := url.Parse(server)
		if err != nil {
			return false
		}
		parsed.Fragment = ""
		if err := dnsURL(parsed.String(), false); err != nil {
			return false
		}
	}
	return true
}

func dnsHosts(servers []string) []string {
	result := make([]string, 0, len(servers))
	for _, server := range servers {
		parsed, err := url.Parse(server)
		if err == nil {
			result = append(result, parsed.Hostname())
		}
	}
	return result
}

func containsDNS(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func dnsProxyNode(store *profile.Store, name, group string) error {
	if strings.TrimSpace(group) == "" {
		return messageError("cli.error.dns.proxy_node_name_is_required", "proxy node name is required")
	}
	if group == "RULES" || group == "DIRECT" {
		return messageError("cli.error.dns.reserved_mihomo_proxy_name_cannot_be_used_for_pinned_dns", "reserved mihomo proxy name cannot be used for pinned DNS")
	}
	data, err := store.Effective(name)
	if err != nil {
		return err
	}
	var cfg dnsBindingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return messageError("cli.error.dns.selected_profile_is_not_valid_yaml", "selected profile is not valid YAML")
	}
	if validDNSBinding(cfg, group) {
		return nil
	}
	return messageError("cli.error.dns.proxy_target_not_found_in_selected_profile_choose_a_non_direct_node_", "proxy target not found in selected profile; choose a non-direct node or proxy group from the effective YAML")
}

type dnsBindingConfig struct {
	Proxies []struct {
		Name string `yaml:"name"`
		Type string `yaml:"type"`
	} `yaml:"proxies"`
	ProxyGroups []struct {
		Name string `yaml:"name"`
	} `yaml:"proxy-groups"`
}

func validDNSBinding(cfg dnsBindingConfig, name string) bool {
	if name == "" || name == "DIRECT" || name == "RULES" {
		return false
	}
	for _, item := range cfg.Proxies {
		if item.Name == name && item.Type != "direct" && item.Type != "reject" && item.Type != "pass" {
			return true
		}
	}
	for _, item := range cfg.ProxyGroups {
		if item.Name == name {
			return true
		}
	}
	return false
}

func validateDNSProxyTargets(data []byte) error {
	var cfg struct {
		DNS struct {
			NameServer []string `yaml:"nameserver"`
		} `yaml:"dns"`
		Proxies []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"proxies"`
		ProxyGroups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return messageError("cli.error.dns.effective_profile_is_not_valid_yaml", "effective profile is not valid YAML")
	}
	for _, server := range cfg.DNS.NameServer {
		parsed, err := url.Parse(server)
		if err != nil {
			return messageError("cli.error.dns.invalid_dns_upstream_url", "invalid DNS upstream URL")
		}
		if parsed.Fragment == "" || parsed.Fragment == "RULES" || parsed.Fragment == "DIRECT" {
			continue
		}
		if !validDNSBinding(dnsBindingConfig{Proxies: cfg.Proxies, ProxyGroups: cfg.ProxyGroups}, parsed.Fragment) {
			return messageError("cli.error.dns.dns_proxy_target_is_missing_or_no_longer_usable_update_nagi_dns_set_", "DNS proxy target is missing or no longer usable; update `nagi dns set proxy NAME URL` before activating this profile")
		}
	}
	return nil
}

func validateManagedDNSProxyTargets(override, effective []byte) error {
	var local struct {
		DNS struct {
			NameServer []string `yaml:"nameserver"`
		} `yaml:"dns"`
	}
	if err := yaml.Unmarshal(override, &local); err != nil {
		return messageError("cli.error.dns.local_override_is_not_valid_yaml", "local override is not valid YAML")
	}
	if len(local.DNS.NameServer) == 0 {
		return nil
	}
	var metadata map[string]any
	if err := yaml.Unmarshal(override, &metadata); err != nil {
		return err
	}
	if group, _ := metadata[profile.DNSProxyGroupKey].(string); group != "" {
		if err := profile.ValidateDNSGroup(effective, group); err != nil {
			return err
		}
	}
	return validateDNSProxyTargets(effective)
}

// A live selector can differ from its persisted default. Refuse applying the
// protected defaults when its actual chain currently ends at a direct adapter.
func validateDefaultDNSLiveTarget(ctx context.Context, args []string, store *profile.Store, current string, client *control.Client) error {
	if len(args) < 4 || args[3] != current {
		return nil
	}
	group := ""
	if len(args) == 6 && args[4] == "--dns-proxy" {
		group = args[5]
	} else {
		var err error
		group, err = store.DefaultDNSProxyGroup(args[3])
		if err != nil {
			return err
		}
	}
	if group == "" {
		return nil
	}
	var response struct {
		Proxies map[string]struct {
			Type  string   `json:"type"`
			Now   string   `json:"now"`
			Fixed string   `json:"fixed"`
			All   []string `json:"all"`
		} `json:"proxies"`
	}
	// A stopped engine has no live selection; the profile checks saved defaults.
	if err := client.Get(ctx, "/proxies", &response); err != nil {
		return nil
	}
	visited := map[string]bool{}
	for {
		item, ok := response.Proxies[group]
		if !ok || visited[group] {
			return messageError("cli.error.dns.dns_proxy_group_has_no_usable_live_selection_run_nagi_proxy_groups_a", "DNS proxy group has no usable live selection; run `nagi proxy groups` and choose an available proxy")
		}
		visited[group] = true
		switch strings.ToLower(item.Type) {
		case "direct", "reject", "rejectdrop", "pass":
			return messageError("cli.error.dns.dns_proxy_group_currently_selects_a_direct_or_rejecting_adapter_sele", "DNS proxy group currently selects a direct or rejecting adapter; select a proxy with `nagi proxy select GROUP NODE` before applying defaults")
		}
		if len(item.All) == 0 {
			return nil
		}
		next := item.Fixed
		if next == "" {
			next = item.Now
		}
		if next == "" {
			return messageError("cli.error.dns.dns_proxy_group_has_no_current_proxy_selection_run_nagi_proxy_groups", "DNS proxy group has no current proxy selection; run `nagi proxy groups` before applying defaults")
		}
		group = next
	}
}
