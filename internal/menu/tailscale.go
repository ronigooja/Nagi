package menu

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

const tailscaleControlURL = "https://controlplane.tailscale.com"

const tailscaleEffects = "Saves a full local proxy-node snapshot; explicit subscription apply clears local overrides. Reloads running mihomo; a stopped engine stays stopped."

type menuTailscaleNode struct {
	Name              string `json:"name"`
	ExitNode          string `json:"exit_node"`
	Hostname          string `json:"hostname"`
	ControlURL        string `json:"control_url"`
	AuthKeyConfigured bool   `json:"auth_key_configured"`
}

type menuTailscaleTarget struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	DialerProxy string `json:"dialer_proxy"`
}

func tailscaleListPage() *Page {
	return &Page{Title: "Tailscale", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Offline configuration for the selected profile. Saved/reloaded does not establish Tailscale login or a working traffic path.", Empty: "No Tailscale nodes configured. Choose Add Tailscale node.", Actions: []Item{
			{Label: "Add Tailscale node", Impact: tailscaleEffects, Action: addTailscale},
		}}
		var data struct {
			Profile string              `json:"profile"`
			Nodes   []menuTailscaleNode `json:"nodes"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "tailscale", "list"), &data); err != nil {
			return view, err
		}
		view.Context = "Profile: " + data.Profile + "\n" + view.Context
		for _, node := range data.Nodes {
			node := node
			view.Items = append(view.Items, Item{Name: node.Name, Label: node.Name + " → " + node.ExitNode, Next: func() *Page { return tailscaleDetailPage(node.Name) }})
		}
		return view, nil
	}}
}

func tailscaleDetailPage(name string) *Page {
	return &Page{Title: "Tailscale: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Tailscale configuration: unknown", Empty: "No targets bound to this Tailscale node."}
		var data struct {
			Profile  string                `json:"profile"`
			Node     menuTailscaleNode     `json:"node"`
			Bindings []menuTailscaleTarget `json:"bindings"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "tailscale", "show", name), &data); err != nil {
			return view, err
		}
		key := "not configured"
		if data.Node.AuthKeyConfigured {
			key = "configured (hidden)"
		}
		control := "official Tailscale"
		if data.Node.ControlURL != "" && data.Node.ControlURL != tailscaleControlURL {
			control = "custom HTTPS Headscale server"
		}
		hostname := data.Node.Hostname
		if hostname == "" {
			hostname = "default"
		}
		view.Context = fmt.Sprintf("Profile: %s\nExit node: %s; hostname: %s; control server: %s\nAuth key: %s\n%s\nSaved/reloaded does not establish Tailscale login or a working traffic path.", data.Profile, data.Node.ExitNode, hostname, control, key, tailscaleEffects)
		for _, binding := range data.Bindings {
			binding := binding
			view.Items = append(view.Items, Item{Name: binding.Name, Label: tailscaleTargetLabel(binding), Impact: "Unbinds this target; provider changes cover ALL nodes, including refreshed nodes", Action: func(ctx context.Context, s *Session) error {
				_, err := s.Form(ctx, nil, nil, func(map[string]string) Result {
					return runTailscale(ctx, s, "unbind", binding.Kind, binding.Name)
				})
				return err
			}})
		}
		view.Actions = []Item{
			{Label: "Edit Tailscale node", Impact: tailscaleEffects, Action: func(ctx context.Context, s *Session) error { return editTailscale(ctx, s, data.Node) }},
			{Label: "Bind target", Impact: "Replaces target dialer-proxy; providers bind ALL nodes, including refreshed nodes", Action: func(ctx context.Context, s *Session) error { return bindTailscale(ctx, s, name) }},
			{Label: "Unbind target", Impact: "Clears target dialer-proxy; provider changes cover ALL nodes, including refreshed nodes", Action: func(ctx context.Context, s *Session) error {
				return tailscaleTargetForm(ctx, s, name, data.Bindings, true)
			}},
			{Label: "Delete Tailscale node", Impact: "Removes this node; fails while configuration still references it", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				result := runTailscale(ctx, s, "remove", name)
				return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
			}},
		}
		return view, nil
	}}
}

func tailscaleName(value string) error {
	if value == "" || len(value) > 128 {
		return fmt.Errorf("enter a printable name from 1 to 128 bytes")
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("enter a printable name from 1 to 128 bytes")
		}
	}
	return nil
}

func tailscaleFields(edit bool) []Field {
	fields := []Field{}
	keyLabel := "Auth key file path (never enter the key itself)"
	if edit {
		keyLabel += " (blank preserves existing key)"
	}
	if !edit {
		fields = append(fields, Field{Key: "name", Label: "Tailscale node name (spaces allowed)", Required: true, Validate: tailscaleName})
	}
	fields = append(fields,
		Field{Key: "exit", Label: "Exit node (Tailscale IP, name, or auto selector)", Required: true},
		Field{Key: "hostname", Label: "Hostname (optional)"},
		Field{Key: "control", Label: "Control URL (official default; optional HTTPS Headscale)", Required: true, Validate: func(raw string) error {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") || u.RawQuery != "" {
				return fmt.Errorf("enter an HTTPS control URL without credentials, query, or fragment")
			}
			return nil
		}},
		Field{Key: "keyfile", Label: keyLabel, Required: !edit},
	)
	return fields
}

func addTailscale(ctx context.Context, s *Session) error {
	s.WriteLine(tailscaleEffects)
	s.WriteLine("Use the official Tailscale control server or an HTTPS Headscale server. The CLI reads the auth key from a file; the menu does not read it.")
	_, err := s.Form(ctx, tailscaleFields(false), map[string]string{"control": tailscaleControlURL}, func(v map[string]string) Result {
		args := []string{"add", v["name"], v["exit"], v["keyfile"], "--control-url", v["control"]}
		if v["hostname"] != "" {
			args = append(args, "--hostname", v["hostname"])
		}
		return runTailscale(ctx, s, args...)
	})
	return err
}

func editTailscale(ctx context.Context, s *Session, node menuTailscaleNode) error {
	s.WriteLine(tailscaleEffects)
	s.WriteLine("Blank keeps current values. Leave auth key file blank to preserve the existing key.")
	control := node.ControlURL
	if control == "" {
		control = tailscaleControlURL
	}
	_, err := s.Form(ctx, tailscaleFields(true), map[string]string{"exit": node.ExitNode, "hostname": node.Hostname, "control": control}, func(v map[string]string) Result {
		args := []string{"edit", node.Name, "--exit-node", v["exit"], "--control-url", v["control"]}
		if v["hostname"] != "" {
			args = append(args, "--hostname", v["hostname"])
		}
		if v["keyfile"] != "" {
			args = append(args, "--auth-key-file", v["keyfile"])
		}
		return runTailscale(ctx, s, args...)
	})
	return err
}

func tailscaleTargetLabel(target menuTailscaleTarget) string {
	label := target.Kind + ": " + target.Name
	if target.Type != "" {
		label += " (" + target.Type + ")"
	}
	if target.DialerProxy != "" {
		label += " → " + target.DialerProxy
	}
	return label
}

func bindTailscale(ctx context.Context, s *Session, name string) error {
	var data struct {
		Targets []menuTailscaleTarget `json:"targets"`
	}
	if err := decodeMenuResult(s.Fetch(ctx, "proxy", "tailscale", "targets"), &data); err != nil {
		return err
	}
	return tailscaleTargetForm(ctx, s, name, data.Targets, false)
}

func tailscaleTargetForm(ctx context.Context, s *Session, name string, targets []menuTailscaleTarget, unbind bool) error {
	if len(targets) == 0 {
		s.feedback(feedbackWarning, "No eligible targets available.")
		return nil
	}
	s.WriteLine(tailscaleEffects)
	s.WriteLine("A provider binding applies to ALL nodes in that provider, including nodes obtained by later refreshes.")
	choices := []string{}
	byLabel := map[string]menuTailscaleTarget{}
	for _, target := range targets {
		label := tailscaleTargetLabel(target)
		choices = append(choices, label)
		byLabel[label] = target
	}
	_, err := s.Form(ctx, []Field{{Key: "target", Label: "Choose target (0 cancels)", Required: true, Choices: choices}}, nil, func(v map[string]string) Result {
		target := byLabel[v["target"]]
		if unbind {
			return runTailscale(ctx, s, "unbind", target.Kind, target.Name)
		}
		return runTailscale(ctx, s, "bind", name, target.Kind, target.Name)
	})
	return err
}

func runTailscale(ctx context.Context, s *Session, args ...string) Result {
	command := append([]string{"proxy", "tailscale"}, args...)
	result := s.Fetch(ctx, command...)
	s.discardInterrupts()
	if result.Code != 0 || result.Error != nil {
		s.ShowResult(command, result)
		return result
	}
	var data struct {
		Saved    bool `json:"saved"`
		Reloaded bool `json:"reloaded"`
	}
	if err := decodeMenuResult(result, &data); err != nil {
		s.feedback(feedbackError, "Could not decode Tailscale operation result.")
		return Result{Code: 1, Error: &CLIError{Message: "Could not decode Tailscale operation result"}}
	}
	s.feedback(feedbackSuccess, fmt.Sprintf("Saved: %t; reloaded: %t. Tailscale login and traffic path are not verified.", data.Saved, data.Reloaded))
	return result
}
