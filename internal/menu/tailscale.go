package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
)

const tailscaleControlURL = "https://controlplane.tailscale.com"

func localizedtailscalePathPage(l i18n.Context, name string) *Page {
	var lastProbe *proxy.TailscalePathProbe
	var probedAt string
	return &Page{ID: "node.tailscalePath", Title: nodeText(l, "tailscale_title", "Tailscale"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{}
		var status proxy.TailscalePathStatus
		readErr := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "tailscale", "status", name), &status)
		if readErr != nil {
			status = proxy.TailscalePathStatus{}
		}
		v.Context = ""
		running := readErr == nil && status.State == "ok" && status.Available
		if readErr != nil {
			v.Context += "\n" + nodeText(l, "tailscale_status_unavailable", "Status unavailable") + ": " + readErr.Error()
		} else {
			v.Context += "\n" + nodeText(l, "tailscale_state", "Status") + " · " + tailscaleInstanceLabel(l, status)
		}

		exit := status.SelectedExitNode
		exitName, exitIP := "", ""
		if exit != nil {
			exitName = firstPathText(exit.Hostname, exit.DNSName)
			exitIP = firstPathTextSlice(exit.TailscaleIPs, "")
		}
		v.Context += "\n\n" + nodeText(l, "current_exit", "Current exit") + "\n  "
		if exitName == "" && exitIP == "" {
			v.Context += nodeText(l, "exit_info_unavailable", "No selected exit information")
		} else {
			v.Context += firstPathText(exitName, nodeText(l, "exit_name_unknown", "Name unknown"))
			if exitIP != "" {
				v.Context += " · " + exitIP
			}
		}

		v.Context += "\n\n" + nodeText(l, "observed_path", "Observed local path") + "\n  "
		if readErr != nil {
			v.Context += nodeText(l, "path_status_unavailable", "Path status unavailable")
		} else if !running {
			v.Context += nodeText(l, "path_unavailable_until_running", "Path unavailable while Tailscale is not running")
		} else if exit == nil {
			v.Context += nodeText(l, "path_no_exit", "No selected exit is reported")
		} else if !exit.Online {
			v.Context += nodeText(l, "exit_offline", "Exit is offline")
		} else {
			switch exit.Path {
			case "direct":
				v.Context += nodeText(l, "connection_direct", "Direct") + "\n  " + routeLine(l, nodeText(l, "this_device", "This device"), firstPathText(exitName, exitIP))
				if exit.Endpoint != "" {
					v.Context += "\n  " + nodeText(l, "endpoint", "Endpoint") + " · " + exit.Endpoint
				}
			case "derp":
				region := firstPathText(exit.DERPRegion, nodeText(l, "region_unknown", "region unknown"))
				v.Context += nodeText(l, "connection_derp", "DERP relay") + " · " + region + "\n  " + routeLine(l, nodeText(l, "this_device", "This device"), nodeText(l, "derp_relay_hop", "DERP relay")+" ("+region+")", firstPathText(exitName, exitIP))
			case "peer_relay":
				relay := firstPathText(exit.PeerRelay, nodeText(l, "relay_peer_unknown", "relay peer unknown"))
				v.Context += nodeText(l, "connection_peer_relay", "Peer relay") + " · " + relay + "\n  " + routeLine(l, nodeText(l, "this_device", "This device"), nodeText(l, "peer_relay_hop", "Peer relay")+" ("+relay+")", firstPathText(exitName, exitIP))
			case "idle":
				v.Context += nodeText(l, "path_idle_to_exit", "No active path to exit")
			default:
				v.Context += nodeText(l, "path_unknown_to_exit", "Path to exit is unknown")
			}
		}
		if lastProbe != nil {
			v.Context += "\n\n" + nodeText(l, "last_probe", "Last probe") + " · " + probedAt + "\n  " + firstPathText(lastProbe.NodeName, nodeText(l, "exit_name_unknown", "Exit name unknown")) + " · " + lastProbe.PeerIP + " · " + tailscalePathLabel(l, lastProbe.Transport)
			if lastProbe.Transport == "derp" && lastProbe.DERPRegionCode != "" {
				v.Context += " (" + lastProbe.DERPRegionCode + ")"
			}
			if lastProbe.Transport == "direct" && lastProbe.Endpoint != "" {
				v.Context += " · " + nodeText(l, "endpoint", "Endpoint") + " " + lastProbe.Endpoint
			}
			if lastProbe.Transport == "peer_relay" && lastProbe.PeerRelay != "" {
				v.Context += " · " + lastProbe.PeerRelay
			}
			v.Context += " · " + strconv.Itoa(lastProbe.LatencyMS) + " ms"
		}

		if running && exit != nil && exit.Online {
			if ip, ok := probeIP(exit.TailscaleIPs); ok {
				v.Items = append(v.Items, Item{ID: "node.tailscaleExitProbe", Label: nodeText(l, "probe_exit", "Probe connection to exit"), Action: func(ctx context.Context, s *Session) error {
					result := s.Fetch(ctx, "proxy", "tailscale", "probe", name, ip)
					if result.Error != nil || result.Code != 0 {
						return errors.New(result.errorText(l))
					}
					var probe proxy.TailscalePathProbe
					if err := localizeddecodeMenuResult(l, result, &probe); err != nil {
						return err
					}
					lastProbe = &probe
					probedAt = time.Now().Format("2006-01-02 15:04")
					s.feedback(feedbackSuccess, nodeText(l, "probe_complete", "Exit probe completed."))
					return nil
				}})
			}
		}
		v.Items = append(v.Items, Item{ID: "node.tailscaleRefresh", Label: nodeText(l, "refresh_path_status", "Refresh path status"), Action: func(context.Context, *Session) error { return nil }})
		if readErr == nil && len(otherTailscalePeers(status.Peers, exit)) > 0 {
			count := len(otherTailscalePeers(status.Peers, exit))
			v.Items = append(v.Items, Item{ID: "node.tailscalePeers", Label: menuFormat(l, "menu.node.other_devices_count", "Other tailnet devices · %d", count), Next: func() *Page { return localizedtailscalePeersPage(l, name) }})
		}
		return v, nil
	}}
}

func tailscaleInstanceLabel(l i18n.Context, status proxy.TailscalePathStatus) string {
	if status.State == "not_started" {
		return nodeText(l, "instance_not_started", "Not started")
	}
	if status.State == "closed" {
		return nodeText(l, "instance_stopped", "Stopped")
	}
	if status.State == "error" {
		return nodeText(l, "instance_status_unavailable", "Status unavailable")
	}
	if status.State == "ok" && status.Available {
		return nodeText(l, "instance_running", "Running")
	}
	switch strings.ToLower(strings.ReplaceAll(status.BackendState, "_", "")) {
	case "needslogin":
		return nodeText(l, "instance_needs_login", "Sign-in required")
	case "needsmachineauth", "needs machine auth":
		return nodeText(l, "instance_needs_machine_auth", "Machine authorization required")
	case "starting":
		return nodeText(l, "backend_starting", "Starting")
	case "stopped":
		return nodeText(l, "instance_stopped", "Stopped")
	}
	if status.BackendState != "" {
		return tailscaleBackendLabel(l, status.BackendState)
	}
	return nodeText(l, "instance_unknown", "Unknown")
}

func routeLine(l i18n.Context, hops ...string) string {
	return strings.Join(hops, " "+nodeText(l, "route_arrow", "→")+" ")
}

func probeIP(values []string) (string, bool) {
	for _, value := range values {
		ip, err := netip.ParseAddr(value)
		if err == nil && ip.Zone() == "" {
			return ip.String(), true
		}
	}
	return "", false
}

func otherTailscalePeers(peers []proxy.TailscalePathPeer, exit *proxy.TailscalePathPeer) []proxy.TailscalePathPeer {
	if exit == nil {
		return append([]proxy.TailscalePathPeer(nil), peers...)
	}
	result := make([]proxy.TailscalePathPeer, 0, len(peers))
	for _, peer := range peers {
		if !sameTailscalePeer(peer, *exit, peers) {
			result = append(result, peer)
		}
	}
	return result
}

func sameTailscalePeer(peer, exit proxy.TailscalePathPeer, peers []proxy.TailscalePathPeer) bool {
	for _, a := range peer.TailscaleIPs {
		for _, b := range exit.TailscaleIPs {
			if a == b {
				return true
			}
		}
	}
	if len(peer.TailscaleIPs) > 0 && len(exit.TailscaleIPs) > 0 {
		return false
	}
	peerNames := []string{peer.Hostname, peer.DNSName}
	exitNames := map[string]bool{}
	for _, name := range []string{exit.Hostname, exit.DNSName} {
		if key := tailscaleNameKey(name); key != "" {
			exitNames[key] = true
		}
	}
	matched := false
	for _, name := range peerNames {
		if exitNames[tailscaleNameKey(name)] && tailscaleNameKey(name) != "" {
			matched = true
		}
	}
	if !matched {
		return false
	}
	count := 0
	for _, candidate := range peers {
		for _, name := range []string{candidate.Hostname, candidate.DNSName} {
			if exitNames[tailscaleNameKey(name)] && tailscaleNameKey(name) != "" {
				count++
				break
			}
		}
	}
	return count == 1
}

func tailscaleNameKey(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}

func localizedtailscalePeersPage(l i18n.Context, name string) *Page {
	return &Page{ID: "node.tailscalePeers", Title: nodeText(l, "other_devices_title", "Other tailnet devices"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		var status proxy.TailscalePathStatus
		result := s.Fetch(ctx, "proxy", "tailscale", "status", name)
		if err := localizeddecodeMenuResult(l, result, &status); err != nil {
			return PageView{Context: nodeText(l, "devices_unavailable", "Device list unavailable: ") + err.Error()}, nil
		}
		v := PageView{Empty: nodeText(l, "no_other_devices", "No other tailnet devices are reported.")}
		for _, peer := range otherTailscalePeers(status.Peers, status.SelectedExitNode) {
			peer := peer
			label := tailscalePathLabel(l, peer.Path) + " · " + firstPathText(peer.Hostname, peer.DNSName)
			if len(peer.TailscaleIPs) > 0 {
				label += " · " + peer.TailscaleIPs[0]
			}
			v.Items = append(v.Items, Item{ID: "node.tailscalePeer", Name: firstPathText(peer.Hostname, peer.DNSName), Label: label, Next: func() *Page { return localizedtailscalePeerPage(l, name, peer) }})
		}
		return v, nil
	}}
}

func firstPathText(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
func firstPathTextSlice(values []string, fallback string) string {
	if len(values) > 0 {
		return values[0]
	}
	return fallback
}

func tailscalePathLabel(l i18n.Context, path string) string {
	switch path {
	case "direct":
		return nodeText(l, "path_direct", "DIRECT")
	case "derp":
		return "DERP"
	case "peer_relay":
		return nodeText(l, "path_peer_relay", "PEER RELAY")
	case "idle":
		return nodeText(l, "path_idle", "IDLE")
	default:
		return nodeText(l, "path_unknown", "UNKNOWN")
	}
}

func tailscaleBackendLabel(l i18n.Context, state string) string {
	switch strings.ToLower(state) {
	case "running":
		return nodeText(l, "backend_running", "Running")
	case "needslogin":
		return nodeText(l, "backend_needs_login", "Needs login")
	case "needs machine auth":
		return nodeText(l, "backend_needs_machine_auth", "Needs machine authorization")
	case "starting":
		return nodeText(l, "backend_starting", "Starting")
	case "stopped":
		return nodeText(l, "backend_stopped", "Stopped")
	default:
		return state
	}
}

func localizedtailscalePeerPage(l i18n.Context, name string, peer proxy.TailscalePathPeer) *Page {
	var lastProbe *proxy.TailscalePathProbe
	var probedAt string
	return &Page{ID: "node.tailscalePeer", Title: firstPathText(peer.Hostname, peer.DNSName), Load: func(ctx context.Context, s *Session) (PageView, error) {
		var latest proxy.TailscalePathStatus
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "tailscale", "status", name), &latest); err != nil {
			return PageView{}, err
		}
		currentPeer, found := peer, false
		for _, candidate := range latest.Peers {
			for _, ip := range candidate.TailscaleIPs {
				for _, peerIP := range peer.TailscaleIPs {
					if ip == peerIP {
						currentPeer, found = candidate, true
					}
				}
			}
		}
		contextText := nodeText(l, "peer", "Peer") + " · " + tailscalePathLabel(l, currentPeer.Path) + "\n" + nodeText(l, "address", "Address") + " · " + firstPathTextSlice(currentPeer.TailscaleIPs, nodeText(l, "ip_unknown", "IP unknown"))
		if !found {
			contextText = nodeText(l, "peer_unavailable", "Peer unavailable in the latest status snapshot.")
		}
		if found && currentPeer.Path == "direct" && currentPeer.Endpoint != "" {
			contextText += "\n" + nodeText(l, "endpoint", "Endpoint") + " · " + currentPeer.Endpoint
		}
		if found && currentPeer.Path == "derp" && currentPeer.DERPRegion != "" {
			contextText += "\n" + nodeText(l, "relay", "Relay") + " · " + currentPeer.DERPRegion
		}
		if found && currentPeer.Path == "peer_relay" && currentPeer.PeerRelay != "" {
			contextText += "\n" + nodeText(l, "peer_relay_via", "Peer relay") + " · " + currentPeer.PeerRelay
		}
		if lastProbe != nil {
			contextText += "\n\n" + nodeText(l, "last_probe", "Last probe") + " · " + probedAt + "\n  " + tailscalePathLabel(l, lastProbe.Transport)
			if lastProbe.Transport == "direct" && lastProbe.Endpoint != "" {
				contextText += " · " + nodeText(l, "endpoint", "Endpoint") + " " + lastProbe.Endpoint
			}
			if lastProbe.Transport == "derp" && lastProbe.DERPRegionCode != "" {
				contextText += " · " + nodeText(l, "relay", "Relay") + " " + lastProbe.DERPRegionCode
			}
			if lastProbe.Transport == "peer_relay" && lastProbe.PeerRelay != "" {
				contextText += " · " + nodeText(l, "peer_relay_via", "Peer relay") + " " + lastProbe.PeerRelay
			}
			contextText += " · " + strconv.Itoa(lastProbe.LatencyMS) + " ms"
		}
		view := PageView{Context: contextText}
		if found && len(currentPeer.TailscaleIPs) > 0 {
			view.Actions = []Item{{ID: "node.tailscaleProbe", Label: nodeText(l, "probe_peer", "Probe this peer"), Action: func(ctx context.Context, s *Session) error {
				result := s.Fetch(ctx, "proxy", "tailscale", "probe", name, currentPeer.TailscaleIPs[0])
				if result.Error != nil || result.Code != 0 {
					return errors.New(result.errorText(l))
				}
				var probe proxy.TailscalePathProbe
				if err := localizeddecodeMenuResult(l, result, &probe); err != nil {
					return err
				}
				lastProbe = &probe
				probedAt = time.Now().Format("2006-01-02 15:04")
				s.feedback(feedbackSuccess, nodeText(l, "probe_complete", "Peer probe completed."))
				return nil
			}}}
		}
		return view, nil
	}}
}

type menuTailscaleNode struct {
	Name              string `json:"name"`
	ExitNode          string `json:"exit_node"`
	Hostname          string `json:"hostname"`
	ControlURL        string `json:"control_url"`
	AuthKeyConfigured bool   `json:"auth_key_configured"`
}

func localizedtailscaleName(language i18n.Context, value string) error {
	if value == "" || len(value) > 128 {
		return fmt.Errorf("%s", menuText(language, "menu.tailscale.tailscale_name.enter_a_printable_name_from_1_to_128_bytes", "enter a printable name from 1 to 128 bytes"))
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("%s", menuText(language, "menu.tailscale.tailscale_name.enter_a_printable_name_from_1_to_128_bytes", "enter a printable name from 1 to 128 bytes"))
		}
	}
	return nil
}

func localizedtailscaleFields(language i18n.Context, edit bool) []Field {
	fields := []Field{}
	keyLabel := menuText(language, "menu.tailscale.tailscale_fields.paste_auth_key_input_is_visible", "Paste Auth Key (input is visible)")
	if edit {
		keyLabel += menuText(language, "menu.tailscale.tailscale_fields.blank_preserves_existing_key", " (blank preserves existing key)")
	}
	if !edit {
		fields = append(fields, Field{Key: "name", Label: menuText(language, "menu.tailscale.tailscale_fields.tailscale_node_name_spaces_allowed", "Tailscale node name (spaces allowed)"), Required: true, Validate: func(value string) error {
			return localizedtailscaleName(

				language, value,
			)
		},
		})
	}
	fields = append(fields,
		Field{Key: "exit", Label: menuText(language, "menu.tailscale.tailscale_fields.exit_node_tailscale_ip_name_or_auto_selector", "Exit node (Tailscale IP, name, or auto selector)"), Required: true},
		Field{Key: "hostname", Label: menuText(language, "menu.tailscale.tailscale_fields.hostname_optional", "Hostname (optional)")},
		Field{Key: "control", Label: menuText(language, "menu.tailscale.tailscale_fields.control_url_official_default_optional_https_head", "Control URL (official default; optional HTTPS Headscale)"), Required: true, Validate: func(raw string) error {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") || u.RawQuery != "" {
				return fmt.Errorf("%s", menuText(language, "menu.tailscale.tailscale_fields.enter_an_https_control_url_without_credentials_q", "enter an HTTPS control URL without credentials, query, or fragment"))
			}
			return nil
		}},
		Field{Key: "authkey", Label: keyLabel, Required: !edit, Validate: func(value string) error {
			if value == "" && edit {
				return nil
			}
			return profile.ValidateTailscaleAuthKey(value)
		}},
	)
	return fields
}

func addTailscale(ctx context.Context, s *Session) error {
	groups, err := selectNodeGroups(ctx, s, nil, nodeText(s.language, "continue", "Continue"))
	if err != nil {
		return err
	}
	_, err = s.Form(ctx, localizedtailscaleFields(s.language,
		false), map[string]string{"control": tailscaleControlURL}, func(v map[string]string) Result {
		args := []string{"add", "tailscale", v["name"], v["exit"], "--auth-key-stdin", "--control-url", v["control"]}
		if v["hostname"] != "" {
			args = append(args, "--hostname", v["hostname"])
		}
		for _, group := range groups {
			args = append(args, "--group", group)
		}
		return runTailscaleInput(ctx, s, strings.NewReader(v["authkey"]), args...)
	})
	return err
}

func editTailscale(ctx context.Context, s *Session, node menuTailscaleNode) error {
	control := node.ControlURL
	if control == "" {
		control = tailscaleControlURL
	}
	_, err := s.Form(ctx, localizedtailscaleFields(s.language,
		true), map[string]string{"exit": node.ExitNode, "hostname": node.Hostname, "control": control}, func(v map[string]string) Result {
		args := []string{"edit", node.Name, "--exit-node", v["exit"], "--control-url", v["control"]}
		if v["hostname"] != "" {
			args = append(args, "--hostname", v["hostname"])
		}
		if v["authkey"] != "" {
			args = append(args, "--auth-key-stdin")
			return runTailscaleInput(ctx, s, strings.NewReader(v["authkey"]), args...)
		}
		return runTailscale(ctx, s, args...)
	})
	return err
}

func runTailscale(ctx context.Context, s *Session, args ...string) Result {
	return runTailscaleInput(ctx, s, nil, args...)
}

func runTailscaleInput(ctx context.Context, s *Session, input io.Reader, args ...string) Result {
	command := append([]string{"proxy", "node"}, args...)
	var result Result
	if input == nil {
		result = s.Fetch(ctx, command...)
	} else if runner, ok := s.runner.(InputRunner); ok {
		result = runner.RunInput(ctx, command, input)
	} else {
		result = Result{Code: 1, Error: &CLIError{Code: "input_unavailable", Message: menuText(s.language, "menu.tailscale.run_tailscale_input.auth_key_input_unavailable", "Auth Key input unavailable")}}
	}
	s.discardInterrupts()
	if result.Code != 0 || result.Error != nil {
		s.ShowResult(command, result)
		return result
	}
	var data struct {
		Saved    bool `json:"saved"`
		Reloaded bool `json:"reloaded"`
	}
	if err := localizeddecodeMenuResult(s.language,
		result, &data); err != nil {
		s.feedback(feedbackError, menuText(s.language, "menu.tailscale.run_tailscale_input.could_not_decode_tailscale_operation_result", "Could not decode Tailscale operation result."))
		return Result{Code: 1, Error: &CLIError{Message: menuText(s.language, "menu.tailscale.run_tailscale_input.could_not_decode_tailscale_operation_result2", "Could not decode Tailscale operation result")}}
	}
	message := menuText(s.language, "menu.tailscale.run_tailscale_input.configuration_unchanged", "Configuration unchanged.")

	if data.Saved {
		message = menuText(s.language, "menu.tailscale.run_tailscale_input.configuration_saved", "Configuration saved.")

	}
	if data.Reloaded {
		message += menuText(s.language, "menu.tailscale.run_tailscale_input.mihomo_reloaded", " Mihomo reloaded.")

	}
	s.feedback(feedbackSuccess, message)
	return result
}
