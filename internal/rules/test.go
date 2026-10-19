package rules

import (
	"github.com/ronigooja/Nagi/internal/i18n"
	"net/netip"
	"strings"
	"unicode"
)

// TestRequest is the destination-only contract of mihomo's rule test API.
type TestRequest struct {
	Target  string `json:"target"`
	IP      string `json:"ip,omitempty"`
	Port    uint16 `json:"port,omitempty"`
	Network string `json:"network,omitempty"`
	Process string `json:"process,omitempty"`
}

func (r *TestRequest) Validate() error {
	if ip, err := netip.ParseAddr(r.Target); err == nil && ip.Zone() == "" {
		r.Target = ip.Unmap().String()
		if r.IP != "" {
			return i18n.Errorf("backend.rules.ip_can_only_accompany_a_domain_target", "--ip can only accompany a domain target")
		}
	} else {
		host := strings.TrimSuffix(strings.ToLower(r.Target), ".")
		if host == "" || len(host) > 253 {
			return i18n.Errorf("backend.rules.target_must_be_a_domain_or_ip_address_without_a_url_or_port", "TARGET must be a domain or IP address, without a URL or port")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return i18n.Errorf("backend.rules.invalid_domain_target", "invalid domain TARGET")
			}
			for _, ch := range label {
				if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
					return i18n.Errorf("backend.rules.target_must_be_an_ascii_domain_or_ip_address_without_a_url_or_port", "TARGET must be an ASCII domain or IP address, without a URL or port")
				}
			}
		}
		r.Target = host
		if r.IP != "" {
			ip, err := netip.ParseAddr(r.IP)
			if err != nil || ip.Zone() != "" {
				return i18n.Errorf("backend.rules.ip_must_be_an_ipv4_or_ipv6_address_without_a_zone", "--ip must be an IPv4 or IPv6 address without a zone")
			}
			r.IP = ip.Unmap().String()
		}
	}
	if r.Port == 0 {
		r.Port = 443
	}
	if r.Network == "" {
		r.Network = "tcp"
	}
	if r.Network != "tcp" && r.Network != "udp" {
		return i18n.Errorf("backend.rules.network_must_be_tcp_or_udp", "--network must be tcp or udp")
	}
	if len(r.Process) > 256 || strings.TrimSpace(r.Process) != r.Process || strings.ContainsAny(r.Process, "/\\") {
		return i18n.Errorf("backend.rules.process_must_be_a_name_of_at_most_256_bytes_without_a_path_or_surrounding_whitespace", "--process must be a name of at most 256 bytes, without a path or surrounding whitespace")
	}
	for _, ch := range r.Process {
		if unicode.IsControl(ch) {
			return i18n.Errorf("backend.rules.process_must_not_contain_control_characters", "--process must not contain control characters")
		}
	}
	return nil
}

type TestResult struct {
	Target        string   `json:"target"`
	DestinationIP string   `json:"destination_ip"`
	Port          uint16   `json:"port"`
	Network       string   `json:"network"`
	Process       string   `json:"process"`
	Mode          string   `json:"mode"`
	Matched       bool     `json:"matched"`
	Rule          string   `json:"rule"`
	RulePayload   string   `json:"rule_payload"`
	Providers     []string `json:"providers"`
	Outbound      string   `json:"outbound"`
	Warnings      []string `json:"warnings"`
}
