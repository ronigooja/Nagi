package menu

import "github.com/ronigooja/Nagi/internal/i18n"

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

const tailscaleEffects = "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."

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

func localizedtailscaleListPage(language i18n.Context) *Page {
	return &Page{ID: "tailscale.tailscaleListPage.page1", Title: menuText(language, "menu.tailscale.tailscale_list_page.tailscale", "Tailscale"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: menuText(language, "menu.tailscale.tailscale_list_page.no_tailscale_nodes_configured_choose_add_tailsca", "No Tailscale nodes configured. Choose Add Tailscale node."), Actions: []Item{
			{ID: "tailscale.tailscaleListPage.item2", Label: menuText(language, "menu.tailscale.tailscale_list_page.add_tailscale_node", "Add Tailscale node"), Impact: menuText(language, "menu.tailscale.tailscale_list_page.subscription_apply_retains_local_settings_and_no", "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."), Action: addTailscale},
		}}
		var data struct {
			Profile string              `json:"profile"`
			Nodes   []menuTailscaleNode `json:"nodes"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "tailscale", "list"), &data); err != nil {
			return view, err
		}
		view.Context = menuText(language, "menu.tailscale.tailscale_list_page.profile", "Profile: ") + data.Profile
		for _, node := range data.Nodes {
			node := node
			view.Items = append(view.Items, Item{ID: "tailscale.tailscaleListPage.item3", Name: node.Name, Label: node.Name + " → " + node.ExitNode, Next: func() *Page { return localizedtailscaleDetailPage(language, node.Name) }})
		}
		return view, nil
	}}
}

func localizedtailscaleDetailPage(language i18n.Context, name string) *Page {
	return &Page{ID: "tailscale.tailscaleDetailPage.page1", Title: menuText(language, "menu.tailscale.tailscale_detail_page.tailscale", "Tailscale: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.tailscale.tailscale_detail_page.tailscale_configuration_unknown", "Tailscale configuration: unknown"), Empty: menuText(language, "menu.tailscale.tailscale_detail_page.no_targets_bound_to_this_tailscale_node", "No targets bound to this Tailscale node.")}
		var data struct {
			Profile  string                `json:"profile"`
			Node     menuTailscaleNode     `json:"node"`
			Bindings []menuTailscaleTarget `json:"bindings"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "tailscale", "show", name), &data); err != nil {
			return view, err
		}
		key := menuText(language, "menu.tailscale.tailscale_detail_page.not_configured", "not configured")
		if data.Node.AuthKeyConfigured {
			key = "configured"
		}
		control := menuText(language, "menu.tailscale.tailscale_detail_page.official_tailscale", "official Tailscale")
		if data.Node.ControlURL != "" && data.Node.ControlURL != tailscaleControlURL {
			control = menuText(language, "menu.tailscale.tailscale_detail_page.custom_https_headscale_server", "custom HTTPS Headscale server")
		}
		hostname := data.Node.Hostname
		if hostname == "" {
			hostname = "default"
		}
		view.Context = menuFormat(language, "menu.tailscale.tailscale_detail_page.profile_s_exit_node_s_hostname_s_control_server", "Profile: %s\nExit node: %s; hostname: %s; control server: %s\nAuth key: %s", data.Profile, data.Node.ExitNode, hostname, control, key)
		managed := false
		for _, binding := range data.Bindings {
			if binding.Kind == "subscription" {
				managed = true
			}
		}
		if managed {
			view.Context += menuText(language, "menu.tailscale.tailscale_detail_page.entire_subscription_policy_manages_inherited_nod", "\nEntire-subscription policy manages inherited node/provider bindings. Unbind the subscription before changing individual bindings or deleting the exit.")
		}
		for _, binding := range data.Bindings {
			if managed && binding.Kind != "subscription" {
				continue
			}
			binding := binding
			impact := menuText(language, "menu.tailscale.tailscale_detail_page.unbinds_this_target", "Unbinds this target")
			if binding.Kind == "provider" {
				impact += menuText(language, "menu.tailscale.tailscale_detail_page.affects_all_current_and_future_nodes", "; affects all current and future nodes")
			} else if binding.Kind == "subscription" {
				impact += menuText(language, "menu.tailscale.tailscale_detail_page.removes_persistent_policy_and_retains_tailscale", "; removes persistent policy and retains Tailscale node")
			}
			view.Items = append(view.Items, Item{ID: "tailscale.tailscaleDetailPage.item2", Name: binding.Name, Label: localizedtailscaleTargetLabel(language, binding), Impact: impact, Action: func(ctx context.Context, s *Session) error {
				_, err := s.Form(ctx, nil, nil, func(map[string]string) Result {
					return runTailscale(ctx, s, "unbind", binding.Kind, binding.Name)
				})
				return err
			}})
		}
		view.Actions = []Item{
			{ID: "tailscale.tailscaleDetailPage.item3", Label: menuText(language, "menu.tailscale.tailscale_detail_page.edit_tailscale_node", "Edit Tailscale node"), Impact: menuText(language, "menu.tailscale.tailscale_detail_page.subscription_apply_retains_local_settings_and_no", "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."), Action: func(ctx context.Context, s *Session) error { return editTailscale(ctx, s, data.Node) }},
			{ID: "tailscale.tailscaleDetailPage.item4", Label: menuText(language, "menu.tailscale.tailscale_detail_page.bind_target", "Bind target"), Impact: menuText(language, "menu.tailscale.tailscale_detail_page.changes_the_target_s_tailscale_exit", "Changes the target's Tailscale exit"), Action: func(ctx context.Context, s *Session) error { return bindTailscale(ctx, s, name) }},
			{ID: "tailscale.tailscaleDetailPage.item5", Label: menuText(language, "menu.tailscale.tailscale_detail_page.unbind_target", "Unbind target"), Impact: menuText(language, "menu.tailscale.tailscale_detail_page.removes_the_target_s_tailscale_exit", "Removes the target's Tailscale exit"), Action: func(ctx context.Context, s *Session) error {
				return tailscaleTargetForm(ctx, s, name, data.Bindings, true)
			}},
			{ID: "tailscale.tailscaleDetailPage.item6", Label: menuText(language, "menu.tailscale.tailscale_detail_page.delete_tailscale_node", "Delete Tailscale node"), Impact: menuText(language, "menu.tailscale.tailscale_detail_page.unbind_references_first", "Unbind references first"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				result := runTailscale(ctx, s, "remove", name)
				return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
			}},
			{ID: "tailscale.tailscaleDetailPage.item7", Label: menuText(language, "menu.tailscale.tailscale_detail_page.bind_multiple_targets", "Bind multiple targets"), Impact: menuText(language, "menu.tailscale.tailscale_detail_page.subscription_apply_retains_local_settings_and_no", "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."), Next: func() *Page { return localizedtailscaleBatchKindPage(language, name, false) }},
			{ID: "tailscale.tailscaleDetailPage.item8", Label: menuText(language, "menu.tailscale.tailscale_detail_page.unbind_multiple_targets", "Unbind multiple targets"), Impact: menuText(language, "menu.tailscale.tailscale_detail_page.subscription_apply_retains_local_settings_and_no", "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."), Next: func() *Page { return localizedtailscaleBatchKindPage(language, name, true) }},
		}
		return view, nil
	}}
}

func localizedtailscaleBatchKindPage(language i18n.Context, name string, unbind bool) *Page {
	effect := menuText(language, "menu.tailscale.tailscale_batch_kind_page.persistent_binding_current_and_future_nodes_afte", "Persistent binding; current and future nodes after apply or scheduled refresh")
	if unbind {
		effect = menuText(language, "menu.tailscale.tailscale_batch_kind_page.removes_persistent_binding_retains_tailscale_nod", "Removes persistent binding; retains Tailscale node")
	}
	return localizedactionPage(language, menuText(language, "menu.tailscale.tailscale_batch_kind_page.choose_binding_scope", "Choose binding scope"), Item{ID: "tailscale.tailscaleBatchKindPage.item1", Label: menuText(language, "menu.tailscale.tailscale_batch_kind_page.specified_nodes", "Specified nodes"), Next: func() *Page { return localizedtailscaleBatchPage(language, name, "node", unbind) }},
		Item{ID: "tailscale.tailscaleBatchKindPage.item2", Label: menuText(language, "menu.tailscale.tailscale_batch_kind_page.entire_providers", "Entire providers"), Impact: menuText(language, "menu.tailscale.tailscale_batch_kind_page.affects_all_current_and_future_nodes", "Affects all current and future nodes"), Next: func() *Page { return localizedtailscaleBatchPage(language, name, "provider", unbind) }},
		Item{ID: "tailscale.tailscaleBatchKindPage.item3", Label: menuText(language, "menu.tailscale.tailscale_batch_kind_page.entire_subscriptions", "Entire subscriptions"), Impact: effect, Next: func() *Page { return localizedtailscaleBatchPage(language, name, "subscription", unbind) }},
	)
}

func localizedtailscaleBatchPage(language i18n.Context, name, kind string, unbind bool) *Page {
	action, title := "bind", menuText(language, "menu.tailscale.tailscale_batch_page.bind_multiple_targets", "Bind multiple targets")
	if unbind {
		action, title = "unbind", menuText(language, "menu.tailscale.tailscale_batch_page.unbind_multiple_targets", "Unbind multiple targets")
	}
	selected := map[string]bool{}
	return &Page{ID: "tailscale.tailscaleBatchPage.page1", Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: menuText(language, "menu.tailscale.tailscale_batch_page.no_eligible_targets_available", "No eligible targets available.")}
		var data struct {
			Targets []menuTailscaleTarget `json:"targets"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "tailscale", "targets"), &data); err != nil {
			return view, err
		}
		if kind != "subscription" && localizedhasSubscriptionPolicy(language, data.Targets) {
			view.Empty = menuText(language, "menu.tailscale.tailscale_batch_page.an_entire_subscription_policy_manages_these_targ", "An entire-subscription policy manages these targets. Unbind the subscription first.")
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
			view.Items = append(view.Items, Item{ID: "tailscale.tailscaleBatchPage.item2", Name: target.Name, Label: localizedtailscaleTargetLabel(language, target), Selected: selected[target.Name], Action: func(context.Context, *Session) error {
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
		view.Context = menuFormat(language, "menu.tailscale.tailscale_batch_page.tailscale_s_selected_d_choose_numbers_to_toggle", "Tailscale: %s; selected: %d\nChoose numbers to toggle targets, then Submit. Existing bindings appear after →.\n%s", name, count, menuText(language, "menu.tailscale.tailscale_batch_page.subscription_apply_retains_local_settings_and_no", "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."))
		if kind == "subscription" {
			if unbind {
				view.Context += menuText(language, "menu.tailscale.tailscale_batch_page.entire_subscriptions_removes_the_persistent_poli", "\nEntire subscriptions: removes the persistent policy, restores source bindings, and retains the Tailscale node.")
			} else {
				view.Context += menuText(language, "menu.tailscale.tailscale_batch_page.entire_subscriptions_already_applied_to_this_pro", "\nEntire subscriptions: already applied to this profile; binding survives apply and scheduled refresh. New nodes inherit the exit.")
			}
		}
		if kind == "provider" {
			view.Context += menuText(language, "menu.tailscale.tailscale_batch_page.providers_affects_all_current_and_future_nodes_i", "\nProviders: affects all current and future nodes, including refresh additions.")
		}
		if len(targets) > 0 {
			view.Actions = []Item{{ID: "tailscale.tailscaleBatchPage.item3", Label: menuText(language, "menu.tailscale.tailscale_batch_page.submit_selected_targets", "Submit selected targets"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
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
					s.feedback(feedbackWarning, menuText(language, "menu.tailscale.tailscale_batch_page.select_at_least_one_target_before_submitting", "Select at least one target before submitting."))
					return Navigation{}, nil
				}
				result := runTailscale(ctx, s, args...)
				if result.Code != 0 || result.Error != nil {
					s.feedback(feedbackWarning, menuText(language, "menu.tailscale.tailscale_batch_page.selections_are_retained_choose_submit_to_retry_o", "Selections are retained. Choose Submit to retry or 0 to cancel."))
					return Navigation{}, nil
				}
				return Navigation{Back: true}, nil
			}}}
		}
		return view, nil
	}}
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
	_, err := s.Form(ctx, localizedtailscaleFields(s.language,
		false), map[string]string{"control": tailscaleControlURL}, func(v map[string]string) Result {
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

func localizedtailscaleTargetLabel(language i18n.Context, target menuTailscaleTarget) string {
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
	if err := localizeddecodeMenuResult(s.language,
		s.Fetch(ctx, "proxy", "tailscale", "targets"), &data); err != nil {
		return err
	}
	return tailscaleTargetForm(ctx, s, name, data.Targets, false)
}

func tailscaleTargetForm(ctx context.Context, s *Session, name string, targets []menuTailscaleTarget, unbind bool) error {
	if localizedhasSubscriptionPolicy(s.language, targets) {
		retained := []menuTailscaleTarget{}
		for _, target := range targets {
			if target.Kind == "subscription" {
				retained = append(retained, target)
			}
		}
		targets = retained
	}
	if len(targets) == 0 {
		s.feedback(feedbackWarning, menuText(s.language, "menu.tailscale.tailscale_target_form.no_eligible_targets_available", "No eligible targets available."))
		return nil
	}
	s.WriteLine(menuText(s.language, "menu.tailscale.tailscale_target_form.subscription_apply_retains_local_settings_and_no", "Subscription apply retains local settings and nodes; entire-subscription bindings persist. Reloads mihomo if running."))
	s.WriteLine(menuText(s.language, "menu.tailscale.tailscale_target_form.providers_affects_all_nodes_including_future_add", "Providers: affects all nodes, including future additions."))
	s.WriteLine(menuText(s.language, "menu.tailscale.tailscale_target_form.entire_subscriptions_persistent_binding_includes", "Entire subscriptions: persistent binding includes new nodes after apply and scheduled refresh; only already-applied records are eligible."))
	choices := []string{}
	byLabel := map[string]menuTailscaleTarget{}
	for _, target := range targets {
		label := localizedtailscaleTargetLabel(s.language,
			target)
		choices = append(choices, label)
		byLabel[label] = target
	}
	_, err := s.Form(ctx, []Field{{Key: "target", Label: menuText(s.language, "menu.tailscale.tailscale_target_form.choose_target_0_cancels", "Choose target (0 cancels)"), Required: true, Choices: choices}}, nil, func(v map[string]string) Result {
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

func localizedhasSubscriptionPolicy(language i18n.Context, targets []menuTailscaleTarget) bool {
	for _, target := range targets {
		if target.Kind == "subscription" && target.DialerProxy != "" {
			return true
		}
	}
	return false
}
func tailscaleListPage() *Page {
	return localizedtailscaleListPage(i18n.New(i18n.English))
}
func tailscaleDetailPage(name string) *Page {
	return localizedtailscaleDetailPage(i18n.New(i18n.English), name)
}
func tailscaleBatchKindPage(name string, unbind bool) *Page {
	return localizedtailscaleBatchKindPage(i18n.New(i18n.English), name, unbind)
}
func tailscaleBatchPage(name, kind string, unbind bool) *Page {
	return localizedtailscaleBatchPage(i18n.New(i18n.English), name, kind, unbind)
}
func tailscaleName(value string) error {
	return localizedtailscaleName(i18n.New(i18n.English), value)
}
func tailscaleFields(
	edit bool) []Field {
	return localizedtailscaleFields(i18n.New(i18n.English), edit)
}
func tailscaleTargetLabel(target menuTailscaleTarget) string {
	return localizedtailscaleTargetLabel(i18n.New(i18n.English), target)
}
func hasSubscriptionPolicy(targets []menuTailscaleTarget) bool {
	return localizedhasSubscriptionPolicy(i18n.New(i18n.English), targets)
}
