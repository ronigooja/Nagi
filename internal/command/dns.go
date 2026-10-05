package command

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

var dnsDomain = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
var dnsBootstrap = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

func dnsURL(raw string, allowPlain bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("DNS server must be a URL without credentials, query, or fragment")
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
	return errors.New("DNS server must be an HTTPS DoH URL; explicit exceptions may use udp://IP:53 or tcp://IP:53")
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
		if (args[1] == "status" && len(args) != 2) || (args[1] == "check" && !(len(args) == 2 || len(args) == 3 && args[2] == "--packet-sample")) {
			return nil, usage("dns " + args[1])
		}
		data, err := store.Effective(name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		var cfg map[string]any
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fail("dns_error", errors.New("selected profile is not valid YAML"))
		}
		dns, _ := cfg["dns"].(map[string]any)
		tun, _ := cfg["tun"].(map[string]any)
		servers := stringList(dns["nameserver"])
		defaultServers := stringList(dns["default-nameserver"])
		issues := []string{}
		if dns["enable"] != true {
			issues = append(issues, "mihomo DNS is disabled")
		}
		if !encryptedServers(servers) || !encryptedServers(defaultServers) {
			issues = append(issues, "primary or bootstrap DNS includes a non-DoH server")
		}
		for _, key := range []string{"fallback", "proxy-server-nameserver", "direct-nameserver"} {
			if values := stringList(dns[key]); len(values) > 0 && !encryptedServers(values) {
				issues = append(issues, key+" includes a non-DoH server")
			}
		}
		if policy, ok := dns["nameserver-policy"].(map[string]any); ok {
			for domain, servers := range policy {
				if !encryptedServers(stringList(servers)) {
					issues = append(issues, "explicit plaintext exception: "+domain)
				}
			}
		}
		if err := validateDNSProxyTargets(data); err != nil {
			issues = append(issues, err.Error())
		}
		hijack := stringList(tun["dns-hijack"])
		if tun["enable"] != true || tun["auto-route"] != true || tun["strict-route"] != true || !containsDNS(hijack, "any:53") || !containsDNS(hijack, "tcp://any:53") {
			issues = append(issues, "TUN routing and DNS interception are not fully configured")
		}
		policy := "direct"
		proxied, ruled := 0, 0
		for _, server := range servers {
			if parsed, err := url.Parse(server); err == nil && parsed.Fragment != "" {
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
			issues = append(issues, "DNS upstreams use mixed routing policies")
		} else if dns["respect-rules"] == true {
			policy = "rules"
		}
		result := map[string]any{"profile": name, "enabled": dns["enable"] == true, "ipv6": dns["ipv6"] == true, "policy": policy, "upstream_hosts": dnsHosts(servers), "tun_enabled": tun["enable"] == true, "issues": issues, "leak_protection_verified": false}
		if args[1] == "check" {
			result["scope"] = "configuration, mihomo API, and observed OS routes/resolvers; application DNS paths and external leaks are not proven"
			observation := systemDNSCheck(ctx)
			evidence := map[string]any{"platform": observation.Platform, "route_interfaces": observation.RouteInterfaces, "resolvers": observation.Resolvers, "route_source": observation.RouteSource, "resolver_source": observation.ResolverSource, "issues": observation.Issues}
			if device, ok := tun["device"].(string); ok && device != "" {
				evidence["configured_tun_device"] = device
				_, interfaceErr := net.InterfaceByName(device)
				evidence["configured_tun_present"] = interfaceErr == nil
				matches := map[string]bool{}
				for family, name := range observation.RouteInterfaces {
					matches[family] = name == device
				}
				evidence["default_route_matches_tun"] = matches
			}
			result["system_evidence"] = evidence
			if len(args) == 3 {
				sample := packetSample(ctx, observation, realDNSPacketRunner{})
				result["packet_sample"] = map[string]any{"status": sample.Status, "domain": sample.Domain, "capture_interfaces": sample.CaptureInterfaces, "matched_interfaces": sample.MatchedInterfaces, "resolver_routes": sample.ResolverRoutes, "query_attempted": sample.QueryAttempted, "issues": sample.Issues}
			}
			var response map[string]any
			if err := client.Get(ctx, "/dns/query?name=example.com&type=A", &response); err == nil {
				result["engine_query_ok"] = true
			} else {
				result["engine_query_ok"] = false
				issues = append(issues, "mihomo DNS query unavailable")
			}
			result["issues"] = issues
		}
		return result, nil
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
		servers := append([]string(nil), args[firstURL:]...)
		for i, server := range servers {
			if err := dnsURL(server, false); err != nil {
				return nil, fail("dns_error", err)
			}
			if group != "" {
				parsed, _ := url.Parse(server)
				parsed.Fragment = group
				servers[i] = parsed.String()
			}
		}
		override, err := dnsOverride(store, name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		dns := dnsSection(override)
		override["ipv6"] = true
		dns["enable"] = true
		dns["ipv6"] = true
		dns["listen"] = "127.0.0.1:1053"
		dns["enhanced-mode"] = "fake-ip"
		dns["default-nameserver"] = dnsBootstrap
		dns["nameserver"] = servers
		dns["fallback"] = []string{}
		dns["direct-nameserver"] = []string{}
		dns["proxy-server-nameserver-policy"] = map[string]any{}
		dns["respect-rules"] = false
		if policy == "proxy" {
			dns["proxy-server-nameserver"] = dnsBootstrap
		} else {
			dns["proxy-server-nameserver"] = []string{}
		}
		if _, ok := dns["nameserver-policy"]; !ok {
			dns["nameserver-policy"] = map[string]any{}
		}
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
			return nil, fail("dns_error", errors.New("exception domain must be a DNS suffix such as lan or corp.example"))
		}
		key := "+." + domain
		override, err := dnsOverride(store, name)
		if err != nil {
			return nil, fail("dns_error", err)
		}
		dns := dnsSection(override)
		if dns["enable"] != true || len(stringList(dns["nameserver"])) == 0 {
			return nil, fail("dns_error", errors.New("run `nagi dns set direct URL` or `nagi dns set proxy NODE URL` first"))
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
				return nil, fail("dns_error", errors.New("configure encrypted DNS with `nagi dns set` before enabling TUN interception"))
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
		return errors.New("proxy node name is required")
	}
	if group == "RULES" || group == "DIRECT" {
		return errors.New("reserved mihomo proxy name cannot be used for pinned DNS")
	}
	data, err := store.Effective(name)
	if err != nil {
		return err
	}
	var cfg struct {
		Proxies []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return errors.New("selected profile is not valid YAML")
	}
	for _, item := range cfg.Proxies {
		if item.Name == group && item.Type != "direct" && item.Type != "reject" && item.Type != "pass" {
			return nil
		}
	}
	return errors.New("proxy node not found in selected profile; choose a non-direct node name from the source YAML")
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
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return errors.New("effective profile is not valid YAML")
	}
	for _, server := range cfg.DNS.NameServer {
		parsed, err := url.Parse(server)
		if err != nil {
			return errors.New("invalid DNS upstream URL")
		}
		if parsed.Fragment == "" || parsed.Fragment == "RULES" || parsed.Fragment == "DIRECT" {
			continue
		}
		found := false
		for _, item := range cfg.Proxies {
			if item.Name == parsed.Fragment && item.Type != "direct" && item.Type != "reject" && item.Type != "pass" {
				found = true
				break
			}
		}
		if !found {
			return errors.New("DNS proxy node is missing or no longer usable; update `nagi dns set proxy NODE URL` before activating this profile")
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
		return errors.New("local override is not valid YAML")
	}
	if len(local.DNS.NameServer) == 0 {
		return nil
	}
	return validateDNSProxyTargets(effective)
}
