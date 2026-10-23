package proxy

import (
	"context"
	"net/netip"
	"net/url"
	"strconv"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/i18n"
)

type TailscalePathStatus struct {
	Available        bool                `json:"available"`
	State            string              `json:"state"`
	BackendState     string              `json:"backend_state,omitempty"`
	SelectedExitNode *TailscalePathPeer  `json:"selected_exit_node,omitempty"`
	Peers            []TailscalePathPeer `json:"peers,omitempty"`
	ErrorCode        string              `json:"error_code,omitempty"`
}

type TailscalePathPeer struct {
	DNSName        string   `json:"dns_name"`
	Hostname       string   `json:"hostname"`
	TailscaleIPs   []string `json:"tailscale_ips,omitempty"`
	Online         bool     `json:"online"`
	Active         bool     `json:"active"`
	ExitNode       bool     `json:"exit_node"`
	ExitNodeOption bool     `json:"exit_node_option"`
	Path           string   `json:"path"`
	Endpoint       string   `json:"endpoint,omitempty"`
	PeerRelay      string   `json:"peer_relay,omitempty"`
	DERPRegion     string   `json:"derp_region,omitempty"`
}

type TailscalePathProbe struct {
	PeerIP         string `json:"peer_ip"`
	NodeName       string `json:"node_name"`
	Transport      string `json:"transport"`
	Endpoint       string `json:"endpoint,omitempty"`
	DERPRegionID   int    `json:"derp_region_id,omitempty"`
	DERPRegionCode string `json:"derp_region_code,omitempty"`
	PeerRelay      string `json:"peer_relay,omitempty"`
	LatencyMS      int    `json:"latency_ms"`
}

func (s *Service) TailscalePathStatus(ctx context.Context, name string) (TailscalePathStatus, error) {
	var result TailscalePathStatus
	if s == nil || s.Client == nil {
		return result, i18n.Errorf("backend.proxy.proxy_control_client_unavailable", "proxy control client unavailable")
	}
	if err := validTailscaleNodeName(name); err != nil {
		return result, err
	}
	err := s.Client.Get(ctx, "/proxies/"+url.PathEscape(name)+"/tailscale/status", &result)
	if err != nil {
		return result, tailscalePathAPIError(err)
	}
	return result, nil
}

func (s *Service) TailscalePathProbe(ctx context.Context, name, peerIP string, timeoutMS int) (TailscalePathProbe, error) {
	var result TailscalePathProbe
	if s == nil || s.Client == nil {
		return result, i18n.Errorf("backend.proxy.proxy_control_client_unavailable", "proxy control client unavailable")
	}
	if err := validTailscaleNodeName(name); err != nil {
		return result, err
	}
	addr, err := netip.ParseAddr(peerIP)
	if err != nil || addr.Zone() != "" {
		return result, i18n.Errorf("backend.proxy.tailscale_peer_ip_must_be_a_literal_ip_address", "peer IP must be a literal IP address")
	}
	if timeoutMS == 0 {
		timeoutMS = 5000
	}
	if timeoutMS < 1000 || timeoutMS > 15000 {
		return result, i18n.Errorf("backend.proxy.tailscale_probe_timeout_must_be_between_1000_and_15000_ms", "probe timeout must be between 1000 and 15000 ms")
	}
	client, ok := s.Client.(TailscalePathClient)
	if !ok {
		return result, i18n.Errorf("backend.proxy.tailscale_path_probe_is_not_supported_by_this_client", "Tailscale path probe is not supported by this client")
	}
	err = client.Post(ctx, "/proxies/"+url.PathEscape(name)+"/tailscale/ping", map[string]any{"peer_ip": addr.String(), "timeout_ms": timeoutMS}, &result)
	if err != nil {
		return result, tailscalePathAPIError(err)
	}
	return result, nil
}

func validTailscaleNodeName(name string) error {
	if name == "" || name == "." || name == ".." {
		return i18n.Errorf("backend.proxy.proxy_node_name_is_required", "proxy node name is required")
	}
	return nil
}

func tailscalePathAPIError(err error) error {
	if api, ok := err.(*control.APIError); ok && api.Status == 404 {
		return i18n.Errorf("backend.proxy.tailscale_path_api_unavailable_engine_unsupported_or_node_missing", "embedded Tailscale path API is unavailable; the engine may not support it or the proxy node may be missing")
	}
	return err
}

func ParseTailscalePathTimeout(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, i18n.Errorf("backend.proxy.invalid_tailscale_probe_timeout", "invalid probe timeout")
	}
	return n, nil
}
