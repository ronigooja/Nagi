package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	return &Page{ID: "node.tailscalePath", Title: nodeText(l, "path_status", "Tailscale path status"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{}
		var status proxy.TailscalePathStatus
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "tailscale", "status", name), &status); err != nil {
			return v, err
		}
		label := func(path string) string { return tailscalePathLabel(l, path) }
		switch status.State {
		case "not_started":
			v.Context = nodeText(l, "instance_not_started", "Instance · NOT STARTED")
		case "closed":
			v.Context = nodeText(l, "instance_closed", "Instance · CLOSED")
		case "error":
			v.Context = nodeText(l, "instance_unavailable", "Instance · STATUS UNAVAILABLE")
		case "ok":
			if status.Available {
				v.Context = nodeText(l, "instance_ready", "Instance · READY")
			} else {
				v.Context = nodeText(l, "instance_not_running", "Instance · NOT RUNNING")
			}
		default:
			v.Context = nodeText(l, "instance_unknown", "Instance · UNKNOWN")
		}
		if status.BackendState != "" {
			v.Context += "\n" + nodeText(l, "backend_state", "Backend") + ": " + tailscaleBackendLabel(l, status.BackendState)
		}
		if status.ErrorCode != "" {
			v.Context += "\n" + nodeText(l, "status_error", "Status") + ": " + status.ErrorCode
		}
		if status.SelectedExitNode != nil {
			p := status.SelectedExitNode
			v.Context += "\n\n" + nodeText(l, "selected_exit", "Selected exit") + "\n  " + firstPathText(p.Hostname, p.DNSName) + " · " + firstPathTextSlice(p.TailscaleIPs, nodeText(l, "ip_unknown", "IP unknown")) + "\n  " + label(p.Path)
			if p.Path == "direct" && p.Endpoint != "" {
				v.Context += " · " + nodeText(l, "endpoint", "Endpoint") + " " + p.Endpoint
			}
			if p.Path == "derp" && p.DERPRegion != "" {
				v.Context += " · " + nodeText(l, "relay", "Relay") + " " + p.DERPRegion
			}
			if p.Path == "peer_relay" && p.PeerRelay != "" {
				v.Context += " · " + p.PeerRelay
			}
		} else if status.State == "ok" {
			v.Context += "\n\n" + nodeText(l, "selected_exit_unknown", "Selected exit · UNKNOWN")
		}
		v.Empty = nodeText(l, "no_peers", "No peer status available.")
		for _, peer := range status.Peers {
			peer := peer
			display := label(peer.Path) + " · " + firstPathText(peer.Hostname, peer.DNSName) + " · " + firstPathTextSlice(peer.TailscaleIPs, nodeText(l, "ip_unknown", "IP unknown"))
			v.Items = append(v.Items, Item{ID: "node.tailscalePeer", Name: firstPathText(peer.Hostname, peer.DNSName), Label: display, Next: func() *Page { return localizedtailscalePeerPage(l, name, peer) }})
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
