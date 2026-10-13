package menu

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"

	"github.com/ronigooja/Nagi/internal/profile"
)

const tailscaleControlURL = "https://controlplane.tailscale.com"

const tailscaleEffects = "Subscription apply clears ordinary overrides; entire-subscription bindings persist. Reloads mihomo if running."

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
		view := PageView{Empty: "No Tailscale nodes configured. Choose Add Tailscale node.", Actions: []Item{
			{Label: "Add Tailscale node", Impact: tailscaleEffects, Action: addTailscale},
		}}
		var data struct {
			Profile string              `json:"profile"`
			Nodes   []menuTailscaleNode `json:"nodes"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "tailscale", "list"), &data); err != nil {
			return view, err
		}
		view.Context = "Profile: " + data.Profile
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
			key = "configured"
		}
		control := "official Tailscale"
		if data.Node.ControlURL != "" && data.Node.ControlURL != tailscaleControlURL {
			control = "custom HTTPS Headscale server"
		}
		hostname := data.Node.Hostname
		if hostname == "" {
			hostname = "default"
		}
		view.Context = fmt.Sprintf("Profile: %s\nExit node: %s; hostname: %s; control server: %s\nAuth key: %s", data.Profile, data.Node.ExitNode, hostname, control, key)
		managed := false
		for _, binding := range data.Bindings {
			if binding.Kind == "subscription" {
				managed = true
			}
		}
		if managed {
			view.Context += "\nEntire-subscription policy manages inherited node/provider bindings. Unbind the subscription before changing individual bindings or deleting the exit."
		}
		for _, binding := range data.Bindings {
			if managed && binding.Kind != "subscription" {
				continue
			}
			binding := binding
			impact := "Unbinds this target"
			if binding.Kind == "provider" {
				impact += "; affects all current and future nodes"
			} else if binding.Kind == "subscription" {
				impact += "; removes persistent policy and retains Tailscale node"
			}
			view.Items = append(view.Items, Item{Name: binding.Name, Label: tailscaleTargetLabel(binding), Impact: impact, Action: func(ctx context.Context, s *Session) error {
				_, err := s.Form(ctx, nil, nil, func(map[string]string) Result {
					return runTailscale(ctx, s, "unbind", binding.Kind, binding.Name)
				})
				return err
			}})
		}
		view.Actions = []Item{
			{Label: "Edit Tailscale node", Impact: tailscaleEffects, Action: func(ctx context.Context, s *Session) error { return editTailscale(ctx, s, data.Node) }},
			{Label: "Bind target", Impact: "Changes the target's Tailscale exit", Action: func(ctx context.Context, s *Session) error { return bindTailscale(ctx, s, name) }},
			{Label: "Unbind target", Impact: "Removes the target's Tailscale exit", Action: func(ctx context.Context, s *Session) error {
				return tailscaleTargetForm(ctx, s, name, data.Bindings, true)
			}},
			{Label: "Delete Tailscale node", Impact: "Unbind references first", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				result := runTailscale(ctx, s, "remove", name)
				return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
			}},
			{Label: "Bind multiple targets", Impact: tailscaleEffects, Next: func() *Page { return tailscaleBatchKindPage(name, false) }},
			{Label: "Unbind multiple targets", Impact: tailscaleEffects, Next: func() *Page { return tailscaleBatchKindPage(name, true) }},
		}
		return view, nil
	}}
}

func tailscaleBatchKindPage(name string, unbind bool) *Page {
	effect := "Persistent binding; current and future nodes after apply or scheduled refresh"
	if unbind {
		effect = "Removes persistent binding; retains Tailscale node"
	}
	return actionPage("Choose binding scope",
		Item{Label: "Specified nodes", Next: func() *Page { return tailscaleBatchPage(name, "node", unbind) }},
		Item{Label: "Entire providers", Impact: "Affects all current and future nodes", Next: func() *Page { return tailscaleBatchPage(name, "provider", unbind) }},
		Item{Label: "Entire subscriptions", Impact: effect, Next: func() *Page { return tailscaleBatchPage(name, "subscription", unbind) }},
	)
}

func tailscaleBatchPage(name, kind string, unbind bool) *Page {
	action, title := "bind", "Bind multiple targets"
	if unbind {
		action, title = "unbind", "Unbind multiple targets"
	}
	selected := map[string]bool{}
	return &Page{Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: "No eligible targets available."}
		var data struct {
			Targets []menuTailscaleTarget `json:"targets"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "tailscale", "targets"), &data); err != nil {
			return view, err
		}
		if kind != "subscription" && hasSubscriptionPolicy(data.Targets) {
			view.Empty = "An entire-subscription policy manages these targets. Unbind the subscription first."
			return view, nil
		}
		targets := []menuTailscaleTarget{}
		available := map[string]bool{}
		for _, target := range data.Targets {
			target := target
			if target.Kind != kind || unbind && target.DialerProxy != name {
				continue
			}
			targets = append(targets, target)
			available[target.Name] = true
			view.Items = append(view.Items, Item{Name: target.Name, Label: tailscaleTargetLabel(target), Selected: selected[target.Name], Action: func(context.Context, *Session) error {
				selected[target.Name] = !selected[target.Name]
				return nil
			}})
		}
		count := 0
		for target, checked := range selected {
			if !available[target] {
				delete(selected, target)
			} else if checked {
				count++
			}
		}
		view.Context = fmt.Sprintf("Tailscale: %s; selected: %d\nChoose numbers to toggle targets, then Submit. Existing bindings appear after →.\n%s", name, count, tailscaleEffects)
		if kind == "subscription" {
			if unbind {
				view.Context += "\nEntire subscriptions: removes the persistent policy, restores source bindings, and retains the Tailscale node."
			} else {
				view.Context += "\nEntire subscriptions: already applied to this profile; binding survives apply and scheduled refresh. New nodes inherit the exit."
			}
		}
		if kind == "provider" {
			view.Context += "\nProviders: affects all current and future nodes, including refresh additions."
		}
		if len(targets) > 0 {
			view.Actions = []Item{{Label: "Submit selected targets", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				args := []string{action}
				if !unbind {
					args = append(args, name)
				}
				args = append(args, kind)
				for _, target := range targets {
					if selected[target.Name] {
						args = append(args, target.Name)
					}
				}
				if count == 0 {
					s.feedback(feedbackWarning, "Select at least one target before submitting.")
					return Navigation{}, nil
				}
				result := runTailscale(ctx, s, args...)
				if result.Code != 0 || result.Error != nil {
					s.feedback(feedbackWarning, "Selections are retained. Choose Submit to retry or 0 to cancel.")
					return Navigation{}, nil
				}
				return Navigation{Back: true}, nil
			}}}
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
	keyLabel := "Paste Auth Key (input is visible)"
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
	_, err := s.Form(ctx, tailscaleFields(false), map[string]string{"control": tailscaleControlURL}, func(v map[string]string) Result {
		args := []string{"add", v["name"], v["exit"], "--auth-key-stdin", "--control-url", v["control"]}
		if v["hostname"] != "" {
			args = append(args, "--hostname", v["hostname"])
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
	_, err := s.Form(ctx, tailscaleFields(true), map[string]string{"exit": node.ExitNode, "hostname": node.Hostname, "control": control}, func(v map[string]string) Result {
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
	if hasSubscriptionPolicy(targets) {
		retained := []menuTailscaleTarget{}
		for _, target := range targets {
			if target.Kind == "subscription" {
				retained = append(retained, target)
			}
		}
		targets = retained
	}
	if len(targets) == 0 {
		s.feedback(feedbackWarning, "No eligible targets available.")
		return nil
	}
	s.WriteLine(tailscaleEffects)
	s.WriteLine("Providers: affects all nodes, including future additions.")
	s.WriteLine("Entire subscriptions: persistent binding includes new nodes after apply and scheduled refresh; only already-applied records are eligible.")
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
	return runTailscaleInput(ctx, s, nil, args...)
}

func runTailscaleInput(ctx context.Context, s *Session, input io.Reader, args ...string) Result {
	command := append([]string{"proxy", "tailscale"}, args...)
	var result Result
	if input == nil {
		result = s.Fetch(ctx, command...)
	} else if runner, ok := s.runner.(InputRunner); ok {
		result = runner.RunInput(ctx, command, input)
	} else {
		result = Result{Code: 1, Error: &CLIError{Code: "input_unavailable", Message: "Auth Key input unavailable"}}
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
	if err := decodeMenuResult(result, &data); err != nil {
		s.feedback(feedbackError, "Could not decode Tailscale operation result.")
		return Result{Code: 1, Error: &CLIError{Message: "Could not decode Tailscale operation result"}}
	}
	message := "Configuration unchanged."
	if data.Saved {
		message = "Configuration saved."
	}
	if data.Reloaded {
		message += " Mihomo reloaded."
	}
	s.feedback(feedbackSuccess, message)
	return result
}

func hasSubscriptionPolicy(targets []menuTailscaleTarget) bool {
	for _, target := range targets {
		if target.Kind == "subscription" && target.DialerProxy != "" {
			return true
		}
	}
	return false
}
