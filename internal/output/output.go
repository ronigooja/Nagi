package output

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/rules"
	"github.com/ronigooja/Nagi/internal/service"
	"github.com/ronigooja/Nagi/internal/subscription"
	"github.com/ronigooja/Nagi/internal/traffic"
)

type Candidates []string

var urlPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)

func RedactError(message string) string {
	return urlPattern.ReplaceAllString(message, "[redacted URL]")
}

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Envelope struct {
	Messages map[string]i18n.Descriptor `json:"messages,omitempty"`
	OK       bool                       `json:"ok"`
	Data     any                        `json:"data,omitempty"`
	Error    *Failure                   `json:"error,omitempty"`
}

func Write(w io.Writer, jsonMode bool, data any) error {
	if jsonMode {
		return json.NewEncoder(w).Encode(Envelope{OK: true, Data: data, Messages: CollectMessages(data)})
	}
	if result, ok := data.(i18n.Result); ok {
		return Write(messageWriter{Writer: w, source: result}, false, result.Value)
	}
	switch v := data.(type) {
	case CaptureResult:
		return writeCapture(w, v)
	case Raw:
		_, err := w.Write(v)
		return err
	case RewriteResult:
		return writeRewrite(w, v)
	case profile.ProviderInterval:
		return line(w, renderText(w, "cli.output.write.provider_s_download_interval_d_seconds_s_enabled_t", map[string]any{"name": fmt.Sprint(v.Name), "interval": v.IntervalSeconds, "source": stateText(w, v.Source), "enabled": stateText(w, fmt.Sprint(v.Enabled))}, fmt.Sprintf("provider %s: download interval %d seconds (%s); enabled %t", v.Name, v.IntervalSeconds, v.Source, v.Enabled)))
	case profile.Healthcheck:
		enabled := stateText(w, "unknown")
		if v.Enabled != nil {
			enabled = stateText(w, fmt.Sprint(*v.Enabled))
		}
		if v.Kind == "subscription" && v.IntervalDefaulted {
			if err := line(w, renderText(w, "cli.output.write.subscription_s_follows_subscription_configuration_and_mihomo_def", map[string]any{"name": fmt.Sprint(v.Name)}, fmt.Sprintf("subscription %s: follows subscription configuration and mihomo defaults", v.Name))); err != nil {
				return err
			}
		} else if err := line(w, renderText(w, "cli.output.write.s_s_interval_d_seconds_s_enabled_s_lazy_t", map[string]any{"kind": stateText(w, v.Kind), "name": fmt.Sprint(v.Name), "interval": v.IntervalSeconds, "source": stateText(w, v.Source), "enabled": fmt.Sprint(enabled), "lazy": stateText(w, fmt.Sprint(v.Lazy))}, fmt.Sprintf("%s %s: interval %d seconds (%s); enabled %s; lazy %t", v.Kind, v.Name, v.IntervalSeconds, v.Source, enabled, v.Lazy))); err != nil {
			return err
		}
		if v.TestURL != "" {
			if err := line(w, renderText(w, "cli.output.write.test_url.message", map[string]any{"url": fmt.Sprint(v.TestURL), "source": stateText(w, v.URLSource)}, "Test URL: "+fmt.Sprint(v.TestURL)+" ("+fmt.Sprint(v.URLSource)+")")); err != nil {
				return err
			}
		}
		return line(w, RenderMessageField(v, "/reason", v.Reason, writerLanguage(w)))
	case string:
		return line(w, v)
	case Candidates:
		for _, candidate := range v {
			if err := line(w, candidate); err != nil {
				return err
			}
		}
		return nil
	case engine.Status:
		return writeLifecycle(w, v)
	case diagnostic.FullReport:
		if v.Healthy {
			if err := stateLine(w, text(w, "cli.output.write.diagnostics", "Diagnostics"), "healthy"); err != nil {
				return err
			}
		} else {
			if err := line(w, renderText(w, "cli.output.write.diagnostics_needs.message", map[string]any{"state": fmt.Sprint(stateValue(w, "attention"))}, "Diagnostics: needs "+fmt.Sprint(stateValue(w, "attention")))); err != nil {
				return err
			}
		}
		for _, check := range v.Checks {
			if err := line(w, "["+stateValue(w, strings.ToUpper(check.Status))+"] "+RenderMessageField(check, "/name", check.Name, writerLanguage(w))+": "+RenderMessageField(check, "/message", check.Message, writerLanguage(w))); err != nil {
				return err
			}
		}
		return nil
	case diagnostic.Report:
		if v.Healthy {
			if err := stateLine(w, text(w, "cli.output.write.nagi_doctor", "Nagi doctor"), "healthy"); err != nil {
				return err
			}
		} else {
			if err := line(w, renderText(w, "cli.output.write.nagi_doctor_needs.message", map[string]any{"state": fmt.Sprint(stateValue(w, "attention"))}, "Nagi doctor: needs "+fmt.Sprint(stateValue(w, "attention")))); err != nil {
				return err
			}
		}
		for _, check := range v.Checks {
			if err := line(w, "["+stateValue(w, strings.ToUpper(check.Status))+"] "+RenderMessageField(check, "/name", check.Name, writerLanguage(w))+": "+RenderMessageField(check, "/message", check.Message, writerLanguage(w))); err != nil {
				return err
			}
		}
		return nil
	case rules.Report:
		for i, lineText := range v.Order {
			if err := line(w, fmt.Sprintf("%d. %s", i, lineText)); err != nil {
				return err
			}
		}
		if len(v.Order) == 0 {
			if err := line(w, text(w, "cli.output.write.no_rules_configured", "No rules configured.")); err != nil {
				return err
			}
		}
		for _, conflict := range v.Conflicts {
			if err := line(w, renderText(w, "cli.output.write.conflict_rule_d_shadows_rule_d_s", map[string]any{"earlier": conflict.Earlier, "later": conflict.Later, "reason": RenderMessageField(conflict, "/reason", conflict.Reason, writerLanguage(w))}, fmt.Sprintf("Conflict: rule %d shadows rule %d (%s)", conflict.Earlier, conflict.Later, conflict.Reason))); err != nil {
				return err
			}
		}
		return nil
	case rules.TestResult:
		return writeRuleTest(w, v)
	case proxy.Group:
		return writeGroup(w, v)
	case proxy.Provider:
		return writeProvider(w, v)
	case proxy.Connection:
		return writeConnection(w, v)
	case proxy.DelayResult:
		return line(w, renderText(w, "cli.output.write.proxy_s_delay_s_url_s_timeout_d_ms", map[string]any{"proxy": fmt.Sprint(v.Proxy), "latency": fmt.Sprint(latencyText(v.DelayMS)), "url": fmt.Sprint(v.URL), "timeout": v.TimeoutMS}, fmt.Sprintf("Proxy %s delay: %s (URL: %s, timeout: %d ms)", v.Proxy, latencyText(v.DelayMS), v.URL, v.TimeoutMS)))
	case proxy.BatchDelayResult:
		if err := line(w, renderText(w, "cli.output.write.proxy_group.message", map[string]any{"group": fmt.Sprint(v.Group)}, "Proxy group "+fmt.Sprint(v.Group)+" latency:")); err != nil {
			return err
		}
		if v.AutomaticReset && v.SavedSelectionRemoved {
			if err := line(w, text(w, "cli.output.write.automatic_selection_resumed_saved_choice_removed", "Automatic selection resumed; saved choice removed.")); err != nil {
				return err
			}
		}
		if len(v.Results) == 0 {
			return line(w, text(w, "cli.output.write.no_nodes_in_this_group", "No nodes in this group."))
		}
		for _, item := range v.Results {
			value := text(w, "cli.output.write.latency_unavailable", "latency unavailable")
			if item.DelayMS != nil {
				value = latencyText(*item.DelayMS)
			}
			if item.Error != "" {
				value = RenderMessageField(item, "/error", item.Error, writerLanguage(w))
			}
			if item.Unmeasurable != "" {
				value = text(w, "cli.output.write.no_latency", "no latency (") + RenderMessageField(item, "/unmeasurable", item.Unmeasurable, writerLanguage(w)) + ")"
			}
			if err := line(w, item.Proxy+": "+value); err != nil {
				return err
			}
		}
		return nil
	case proxy.RestoreResult:
		if len(v.Restored) == 0 && len(v.Unavailable) == 0 && len(v.Failed) == 0 {
			return line(w, text(w, "cli.output.write.no_saved_proxy_selections_for_this_profile", "No saved proxy selections for this profile."))
		}
		for _, item := range v.Restored {
			if err := line(w, renderText(w, "cli.output.write.restored.message", map[string]any{"selection": fmt.Sprint(item)}, "Restored: "+fmt.Sprint(item))); err != nil {
				return err
			}
		}
		for _, item := range v.Unavailable {
			if err := line(w, renderText(w, "cli.output.write.or.message", map[string]any{"unavailable": fmt.Sprint(stateValue(w, "unavailable")), "removed": fmt.Sprint(stateValue(w, "removed")), "selection": fmt.Sprint(item)}, fmt.Sprint(stateValue(w, "unavailable"))+" or "+fmt.Sprint(stateValue(w, "removed"))+": "+fmt.Sprint(item))); err != nil {
				return err
			}
		}
		for _, item := range v.Failed {
			if err := line(w, renderText(w, "cli.output.write.could_not_restore.message", map[string]any{"selection": fmt.Sprint(item)}, "Could not restore: "+fmt.Sprint(item))); err != nil {
				return err
			}
		}
		return nil
	case proxy.Candidates:
		for _, name := range v {
			if err := line(w, name); err != nil {
				return err
			}
		}
		return nil
	case subscription.RefreshResult:
		if err := line(w, renderText(w, "cli.output.write.downloaded_subscription_s_d_bytes", map[string]any{"name": fmt.Sprint(v.Name), "bytes": v.Bytes}, fmt.Sprintf("Downloaded subscription %s (%d bytes).", v.Name, v.Bytes))); err != nil {
			return err
		}
		if v.Preview != nil {
			if err := writePreview(w, *v.Preview); err != nil {
				return err
			}
		}
		return line(w, renderText(w, "cli.output.write.run_nagi_subscription_apply_s_to_select_its_profile", map[string]any{"name": fmt.Sprint(v.Name)}, fmt.Sprintf("Run `nagi subscription apply %s` to select its profile.", v.Name)))
	case subscription.Preview:
		return writePreview(w, v)
	case traffic.State:
		state := "disabled"
		if v.Enabled {
			state = "enabled"
		}
		if err := line(w, renderText(w, "cli.output.write.system_proxy.message", map[string]any{"state": fmt.Sprint(stateValue(w, state)), "backend": fmt.Sprint(stateValue(w, v.Backend))}, "System proxy: "+fmt.Sprint(stateValue(w, state))+" ("+fmt.Sprint(stateValue(w, v.Backend))+")")); err != nil {
			return err
		}
		for _, item := range []struct {
			name  string
			value traffic.Setting
		}{{"HTTP", v.HTTP}, {"HTTPS", v.HTTPS}, {"SOCKS", v.SOCKS}} {
			label := "off"
			if writerLanguage(w).Language() == i18n.SimplifiedChinese {
				label = stateText(w, "disabled")
			}
			if item.value.Enabled {
				label = fmt.Sprintf("%s:%d", item.value.Host, item.value.Port)
			}
			if err := line(w, item.name+": "+label); err != nil {
				return err
			}
		}
		return nil
	case service.Result:
		return writeServiceResult(w, v)
	case map[string]any:
		return writeHumanMap(w, v)
	default:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	}
}

func latencyText(milliseconds int) string {
	if milliseconds == 0 {
		return "<1 ms"
	}
	return fmt.Sprintf("%d ms", milliseconds)
}

func line(w io.Writer, s string) error {
	_, err := io.WriteString(w, s+"\n")
	return err
}

func stateLine(w io.Writer, label, value string) error {
	return line(w, label+": "+stateValue(w, value))
}

func writeLifecycle(w io.Writer, v engine.Status) error {
	state := "stopped"
	if v.Running {
		state = "running"
	}
	if err := stateLine(w, "Mihomo", state); err != nil {
		return err
	}
	if v.PID != 0 {
		if err := line(w, fmt.Sprintf("PID: %d", v.PID)); err != nil {
			return err
		}
	}
	if v.StalePID != 0 {
		if err := line(w, renderText(w, "cli.output.writelifecycle.previous_pid_d", map[string]any{"pid": v.StalePID}, fmt.Sprintf("Previous PID: %d", v.StalePID))); err != nil {
			return err
		}
	}
	if v.SocketPath != "" {
		if err := line(w, renderText(w, "cli.output.writelifecycle.socket.message", map[string]any{"path": fmt.Sprint(v.SocketPath)}, "Socket: "+fmt.Sprint(v.SocketPath))); err != nil {
			return err
		}
	}
	if v.LogPath != "" {
		if err := line(w, renderText(w, "cli.output.writelifecycle.log.message", map[string]any{"path": fmt.Sprint(v.LogPath)}, "Log: "+fmt.Sprint(v.LogPath))); err != nil {
			return err
		}
	}
	if v.UnexpectedExit {
		return line(w, text(w, "cli.output.writelifecycle.the_previous_mihomo_process_exited_unexpectedly_inspect_logs_wit", "The previous mihomo process exited unexpectedly. Inspect logs with `nagi logs`."))
	}
	return nil
}

func writeGroup(w io.Writer, g proxy.Group) error {
	heading := g.Name
	if g.Type != "" {
		heading += " (" + g.Type + ")"
	}
	if g.Now != "" {
		heading += " -> " + g.Now
	}
	if g.SelectedStatus == "unavailable" || g.SelectedStatus == "removed" {
		heading += " (" + stateValue(w, g.SelectedStatus) + ")"
	}
	if err := line(w, heading); err != nil {
		return err
	}
	if len(g.All) == 0 {
		return line(w, text(w, "cli.output.writegroup.no_nodes_available_in_this_group", "  No nodes available in this group."))
	}
	for _, node := range g.All {
		marker := "  "
		if node == g.Now {
			marker = "* "
		}
		if err := line(w, marker+node); err != nil {
			return err
		}
	}
	return nil
}

func writeProvider(w io.Writer, p proxy.Provider) error {
	if err := line(w, renderText(w, "cli.output.writeprovider.s_s_s_d_nodes", map[string]any{"name": fmt.Sprint(p.Name), "type": fmt.Sprint(p.Type), "vehicle": fmt.Sprint(p.VehicleType), "count": len(p.Nodes)}, fmt.Sprintf("%s (%s, %s): %d nodes", p.Name, p.Type, p.VehicleType, len(p.Nodes)))); err != nil {
		return err
	}
	for _, name := range p.Nodes {
		if err := line(w, "  "+name); err != nil {
			return err
		}
	}
	return nil
}

func writeHumanMap(w io.Writer, v map[string]any) error {
	if svc, ok := v["service"].(service.Result); ok {
		if err := writeServiceResult(w, svc); err != nil {
			return err
		}
		if engineStatus, ok := v["engine"].(engine.Status); ok {
			return writeLifecycle(w, engineStatus)
		}
		return nil
	}
	if v["recovered"] == false {
		if reason, ok := v["reason"].(string); ok {
			return line(w, fieldText(w, "/reason", reason))
		}
	}
	if message, ok := v["error"].(string); ok && len(v) == 1 {
		if message == "" {
			return line(w, text(w, "cli.output.writehumanmap.no_background_update_failure_is_pending", "No background update failure is pending."))
		}
		message = fieldText(w, "/error", message)
		return line(w, renderText(w, "cli.output.writehumanmap.last_background_update_error.message", map[string]any{"detail": fmt.Sprint(RedactError(message))}, "Last background update error: "+fmt.Sprint(RedactError(message))))
	}
	if action, ok := v["action"].(string); ok && (v["completed"] == true || v["scheduled"] == true) {
		if v["scheduled"] == true {
			if v["background"] == true {
				return line(w, text(w, "cli.output.writehumanmap.update_scheduled_nagi_will_reopen_after_the_background_update_co", "Update scheduled. Nagi will reopen after the background update completes."))
			}
			return line(w, text(w, "cli.output.writehumanmap.update_opened_in_terminal_quit_nagi_app_to_continue_it_will_reop", "Update opened in Terminal. Quit Nagi.app to continue; it will reopen after a successful update."))
		}
		switch action {
		case "app-install":
			return line(w, text(w, "cli.output.writehumanmap.installed_nagi_app_open_it_from_applications", "Installed Nagi.app. Open it from Applications."))
		case "app-uninstall":
			return line(w, text(w, "cli.output.writehumanmap.removed_nagi_app_the_cli_and_user_data_are_preserved", "Removed Nagi.app. The CLI and user data are preserved."))
		case "uninstall":
			if v["user_data_removed"] == true {
				return line(w, text(w, "cli.output.writehumanmap.removed_nagi_and_its_user_data", "Removed Nagi and its user data."))
			}
			return line(w, text(w, "cli.output.writehumanmap.removed_nagi_user_profiles_subscriptions_settings_and_logs_are_p", "Removed Nagi. User profiles, subscriptions, settings, and logs are preserved."))
		case "update":
			return line(w, text(w, "cli.output.writehumanmap.updated_nagi_cli_mihomo_and_any_installed_app_the_default_profil", "Updated Nagi CLI, mihomo, and any installed App. The default profile was refreshed."))
		}
	}
	if app, ok := v["app"].(string); ok && v["managed"] == true {
		if v["installed"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.managed_nagi_app.message", map[string]any{"path": fmt.Sprint(app)}, "Managed Nagi App: "+fmt.Sprint(app)))
		}
		return line(w, text(w, "cli.output.writehumanmap.nagi_app_is_not_installed_run_nagi_app_install", "Nagi App is not installed. Run `nagi app install`."))
	}
	if v["quit"] == true {
		return line(w, renderText(w, "cli.output.writehumanmap.for_this_login_session_login_startup_preference_is_unchanged.message", map[string]any{"state": fmt.Sprint(stateValue(w, "stopped"))}, "Nagi "+fmt.Sprint(stateValue(w, "stopped"))+" for this login session. Login startup preference is unchanged."))
	}
	if v["flushed"] == true {
		return line(w, text(w, "cli.output.writehumanmap.cleared_mihomo_dns_cache", "Cleared mihomo DNS cache."))
	}
	if v["exception_changed"] == true {
		return line(w, renderText(w, "cli.output.writehumanmap.updated_dns_exception_for_v", map[string]any{"domain": fmt.Sprint(v["domain"])}, fmt.Sprintf("Updated DNS exception for %v.", v["domain"])))
	}
	if v["saved"] == true && v["action"] != nil && v["profile"] != nil && v["reloaded"] != nil {
		state := text(w, "cli.output.writehumanmap.saved_mihomo_is_stopped_run_nagi_start_to_activate", "Saved; mihomo is stopped. Run `nagi start` to activate.")
		if v["reloaded"] == true {
			state = text(w, "cli.output.writehumanmap.saved_and_reloaded_mihomo", "Saved and reloaded mihomo.")
		}
		if v["kind"] == "subscription" {
			if v["action"] == "bind" {
				state += text(w, "cli.output.writehumanmap.entire_subscription_policy_survives_apply_and_scheduled_refresh_", " Entire-subscription policy survives apply and scheduled refresh; new nodes inherit the exit.")
			} else {
				state += text(w, "cli.output.writehumanmap.removed_entire_subscription_policy_retained_the_tailscale_node", " Removed entire-subscription policy; retained the Tailscale node.")
			}
		}
		if targets, ok := v["targets"].([]string); ok {
			return line(w, renderText(w, "cli.output.writehumanmap.tailscale_s_d_s_targets_in_profile_s_s", map[string]any{"action": stateText(w, fmt.Sprint(v["action"])), "count": len(targets), "kind": stateText(w, fmt.Sprint(v["kind"])), "profile": fmt.Sprint(v["profile"]), "state": fmt.Sprint(state)}, fmt.Sprintf("Tailscale %s: %d %s targets in profile %s. %s", v["action"], len(targets), v["kind"], v["profile"], state)))
		}
		return line(w, renderText(w, "cli.output.writehumanmap.tailscale_s_in_profile_s_s", map[string]any{"action": stateText(w, fmt.Sprint(v["action"])), "profile": fmt.Sprint(v["profile"]), "state": fmt.Sprint(state)}, fmt.Sprintf("Tailscale %s in profile %s. %s", v["action"], v["profile"], state)))
	}

	if v["saved"] == true && v["profile"] != nil && v["action"] == nil {
		if enabled, ok := v["tun_enabled"].(bool); ok {
			return line(w, renderText(w, "cli.output.writehumanmap.saved_tun_dns_interception_setting.message", map[string]any{"enabled": fmt.Sprint(stateValue(w, yesNo(enabled)))}, "Saved TUN DNS interception setting: "+fmt.Sprint(stateValue(w, yesNo(enabled)))+"."))
		}
		return line(w, renderText(w, "cli.output.writehumanmap.saved_dns_policy_for_profile_v", map[string]any{"profile": fmt.Sprint(v["profile"])}, fmt.Sprintf("Saved DNS policy for profile %v.", v["profile"])))
	}
	if domain, ok := v["domain"].(string); ok && v["response"] != nil {
		return line(w, renderText(w, "cli.output.writehumanmap.mihomo_dns_response_for_s_v_v", map[string]any{"domain": fmt.Sprint(domain), "type": fmt.Sprint(v["type"]), "response": fmt.Sprint(v["response"])}, fmt.Sprintf("Mihomo DNS response for %s (%v): %v", domain, v["type"], v["response"])))
	}
	if _, ok := v["upstream_hosts"].([]string); ok {
		if err := line(w, renderText(w, "cli.output.writehumanmap.dns_profile_v_enabled_s_ipv6_s_doh_policy_s_tun_s", map[string]any{"profile": fmt.Sprint(v["profile"]), "enabled": fmt.Sprint(stateValue(w, yesNo(v["enabled"] == true))), "ipv6": fmt.Sprint(stateValue(w, yesNo(v["ipv6"] == true))), "policy": fmt.Sprint(stateValue(w, fmt.Sprint(v["policy"]))), "tun": fmt.Sprint(stateValue(w, yesNo(v["tun_enabled"] == true)))}, fmt.Sprintf("DNS profile: %v | enabled: %s | IPv6: %s | DoH policy: %s | TUN: %s", v["profile"], stateValue(w, yesNo(v["enabled"] == true)), stateValue(w, yesNo(v["ipv6"] == true)), stateValue(w, fmt.Sprint(v["policy"])), stateValue(w, yesNo(v["tun_enabled"] == true))))); err != nil {
			return err
		}
		if queryOK, ok := v["engine_query_ok"].(bool); ok {
			if err := line(w, renderText(w, "cli.output.writehumanmap.mihomo_dns_query_succeeded_t", map[string]any{"succeeded": queryOK}, fmt.Sprintf("Mihomo DNS query succeeded: %t", queryOK))); err != nil {
				return err
			}
		}
		for _, issue := range v["issues"].([]string) {
			if err := line(w, renderText(w, "cli.output.writehumanmap.warning.message", map[string]any{"detail": fmt.Sprint(issue)}, "Warning: "+fmt.Sprint(issue))); err != nil {
				return err
			}
		}
		if scope, ok := v["scope"].(string); ok {
			return line(w, renderText(w, "cli.output.writehumanmap.scope.message", map[string]any{"scope": fmt.Sprint(scope)}, "Scope: "+fmt.Sprint(scope)))
		}
		return nil
	}
	if yaml, ok := v["yaml"].(string); ok {
		return writeRaw(w, yaml)
	}
	if lines, ok := v["lines"].([]string); ok {
		if len(lines) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_log_lines_available", "No log lines available."))
		}
		return writeRaw(w, strings.Join(lines, "\n"))
	}
	if profiles, ok := v["profiles"].([]string); ok {
		current, _ := v["current"].(string)
		if len(profiles) == 0 {
			if err := line(w, text(w, "cli.output.writehumanmap.no_profiles_found_import_one_with_nagi_profile_import_name_file", "No profiles found. Import one with `nagi profile import NAME FILE`.")); err != nil {
				return err
			}
		}
		for _, name := range profiles {
			marker := "  "
			if name == current {
				marker = "* "
			}
			if err := line(w, marker+name); err != nil {
				return err
			}
		}
		if currentErr, ok := v["current_error"].(string); ok && currentErr != "" {
			currentErr = fieldText(w, "/current_error", currentErr)
			return line(w, renderText(w, "cli.output.writehumanmap.warning_selected_profile_is_unavailable.message", map[string]any{"detail": fmt.Sprint(currentErr)}, "Warning: selected profile is unavailable: "+fmt.Sprint(currentErr)))
		}
		return nil
	}
	if groups, ok := v["groups"].([]proxy.Group); ok {
		if len(groups) == 0 {
			if _, searched := v["query"]; searched {
				return line(w, text(w, "cli.output.writehumanmap.no_matching_nodes_found", "No matching nodes found."))
			}
			return line(w, text(w, "cli.output.writehumanmap.no_proxy_groups_configured_check_the_selected_profile_with_nagi_", "No proxy groups configured. Check the selected profile with `nagi config show`."))
		}
		for i, group := range groups {
			if i > 0 {
				if err := line(w, ""); err != nil {
					return err
				}
			}
			if err := writeGroup(w, group); err != nil {
				return err
			}
		}
		return nil
	}
	if providers, ok := v["providers"].([]proxy.Provider); ok {
		if len(providers) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_proxy_providers_configured", "No proxy providers configured."))
		}
		for _, provider := range providers {
			if err := writeProvider(w, provider); err != nil {
				return err
			}
		}
		return nil
	}
	if entries, ok := v["subscriptions"].([]subscription.Entry); ok {
		scheduler := "stopped"
		if v["scheduler_running"] == true {
			scheduler = "running"
		}
		if _, err := fmt.Fprintln(w, text(w, "cli.output.writehumanmap.scheduler", "Scheduler: ")+stateValue(w, scheduler)); err != nil {
			return err
		}
		if len(entries) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_subscriptions_configured_add_one_with_nagi_subscription_add_n", "No subscriptions configured. Add one with `nagi subscription add NAME URL`."))
		}
		for _, entry := range entries {
			label := entry.Name
			if entry.KeyConfigured {
				label += text(w, "cli.output.writehumanmap.age_key_configured", "  Age key configured")
			}
			if !entry.UpdatedAt.IsZero() {
				label += text(w, "cli.output.writehumanmap.updated", "  Updated: ") + entry.UpdatedAt.Format("2006-01-02 15:04 MST")
			} else {
				label += text(w, "cli.output.writehumanmap.never_updated", "  Never updated")
			}
			if !entry.ExpiresAt.IsZero() {
				label += text(w, "cli.output.writehumanmap.expires", "  Expires: ") + entry.ExpiresAt.Format("2006-01-02 15:04 MST")
			}
			if entry.Total > 0 {
				label += renderText(w, "cli.output.writehumanmap.traffic_d_d_bytes", map[string]any{"used": entry.Upload + entry.Download, "total": entry.Total}, fmt.Sprintf("  Traffic: %d/%d bytes", entry.Upload+entry.Download, entry.Total))
			}
			if entry.ScheduleSeconds == 0 {
				label += text(w, "cli.output.writehumanmap.schedule_off", "  Schedule: off")
			} else {
				label += renderText(w, "cli.output.writehumanmap.schedule_ds", map[string]any{"interval": entry.ScheduleSeconds}, fmt.Sprintf("  Schedule: %ds", entry.ScheduleSeconds))
			}
			if !entry.NextDue.IsZero() {
				label += text(w, "cli.output.writehumanmap.next_due", "  Next due: ") + entry.NextDue.Format("2006-01-02 15:04 MST")
			}
			if entry.Profile != "" {
				label += text(w, "cli.output.writehumanmap.profile", "  Profile: ") + entry.Profile
			}
			if entry.CacheVersion != "" {
				label += text(w, "cli.output.writehumanmap.cache_version", "  Cache version: ") + entry.CacheVersion
			}
			if entry.AppliedCacheVersion != "" {
				label += text(w, "cli.output.writehumanmap.applied_version", "  Applied version: ") + entry.AppliedCacheVersion
			}
			if entry.LastFailureStage != "" {
				label += text(w, "cli.output.writehumanmap.last_failure", "  Last failure: ") + entry.LastFailureStage + " (" + RenderMessageField(entry, "/last_failure_reason", entry.LastFailureReason, writerLanguage(w)) + ")"
			}
			if entry.PauseReason != "" {
				label += text(w, "cli.output.writehumanmap.automatic_apply_paused", "  Automatic apply paused: ") + RenderMessageField(entry, "/pause_reason", entry.PauseReason, writerLanguage(w))
			}

			if err := line(w, label); err != nil {
				return err
			}
		}
		return nil
	}
	if connections, ok := v["connections"].([]proxy.Connection); ok {
		if len(connections) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_active_connections", "No active connections."))
		}
		for _, connection := range connections {
			destination := connectionDestination(connection.Metadata)
			if destination == "" {
				destination = text(w, "cli.output.writehumanmap.destination_unavailable", "Destination unavailable")
			}
			label := renderText(w, "cli.output.writehumanmap.s_id_s_upload_d_b_download_d_b", map[string]any{"destination": fmt.Sprint(destination), "id": fmt.Sprint(connection.ID), "upload": connection.Upload, "download": connection.Download}, fmt.Sprintf("%s  ID: %s  Upload: %d B  Download: %d B", destination, connection.ID, connection.Upload, connection.Download))
			if len(connection.Chains) > 0 {
				label += text(w, "cli.output.writehumanmap.chain", "  Chain: ") + strings.Join(connection.Chains, " -> ")
			}
			if err := line(w, label); err != nil {
				return err
			}
		}
		return nil
	}
	if entries, ok := v["entries"].([]rules.Entry); ok {
		if name, ok := v["profile"].(string); ok {
			if err := line(w, renderText(w, "cli.output.writehumanmap.profile_116.message", map[string]any{"profile": fmt.Sprint(name)}, "Profile: "+fmt.Sprint(name))); err != nil {
				return err
			}
		}
		if len(entries) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_custom_rules_add_one_with_nagi_rules_add_name_type_payload_ta", "No custom rules. Add one with `nagi rules add NAME TYPE PAYLOAD TARGET`."))
		}
		for _, entry := range entries {
			state := "disabled"
			if entry.Enabled {
				state = "enabled"
			}
			if entry.Kind == "rules" {
				if err := line(w, renderText(w, "cli.output.writehumanmap.s_s_d_imported_rules", map[string]any{"name": fmt.Sprint(entry.Name), "state": fmt.Sprint(stateValue(w, state)), "count": len(entry.Items)}, fmt.Sprintf("%s (%s): %d imported rules", entry.Name, stateValue(w, state), len(entry.Items)))); err != nil {
					return err
				}
				for i, rule := range entry.Items {
					if err := line(w, fmt.Sprintf("  %d. %s", i+1, rule)); err != nil {
						return err
					}
				}
				continue
			}
			if err := line(w, entry.Name+" ("+stateValue(w, state)+"): "+rules.Line(entry)); err != nil {
				return err
			}
		}
		return nil
	}
	if active, ok := v["rules"].([]map[string]any); ok {
		if len(active) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_active_rules_reported_by_mihomo", "No active rules reported by mihomo."))
		}
		for _, rule := range active {
			if err := line(w, fmt.Sprintf("%v. %v %v -> %v", rule["index"], rule["type"], rule["payload"], rule["proxy"])); err != nil {
				return err
			}
		}
		return nil
	}
	if providers, ok := v["providers"].(map[string]any); ok {
		if len(providers) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_active_rule_providers", "No active rule providers."))
		}
		for name := range providers {
			if err := line(w, name); err != nil {
				return err
			}
		}
		return nil
	}
	if id, ok := v["id"].(string); ok {
		if rule, ok := v["rule"].(string); ok {
			return line(w, renderText(w, "cli.output.writehumanmap.connection_s_matched_s_v", map[string]any{"id": fmt.Sprint(id), "rule": fmt.Sprint(rule), "payload": fmt.Sprint(v["rule_payload"])}, fmt.Sprintf("Connection %s matched: %s %v", id, rule, v["rule_payload"])))
		}
	}
	if name, ok := v["name"].(string); ok && v["saved"] == true {
		if profile, ok := v["profile"].(string); ok {
			if err := line(w, renderText(w, "cli.output.writehumanmap.profile_126.message", map[string]any{"profile": fmt.Sprint(profile)}, "Profile: "+fmt.Sprint(profile))); err != nil {
				return err
			}
		}
		return line(w, renderText(w, "cli.output.writehumanmap.rule_s_v_effective_configuration_was_validated_running_mihomo_wa", map[string]any{"name": fmt.Sprint(name), "action": stateText(w, fmt.Sprint(v["action"]))}, fmt.Sprintf("Rule %s: %v. Effective configuration was validated; running mihomo was reloaded if active.", name, v["action"])))
	}
	if running, ok := v["running"].(bool); ok {
		state := "stopped"
		if running {
			state = "running"
		}
		if err := stateLine(w, "Mihomo", state); err != nil {
			return err
		}
		if err := printField(w, text(w, "cli.output.writehumanmap.profile_129", "Profile"), v, "profile"); err != nil {
			return err
		}
		if profileErr, ok := v["profile_error"].(string); ok && profileErr != "" {
			profileErr = fieldText(w, "/profile_error", profileErr)
			if err := line(w, renderText(w, "cli.output.writehumanmap.warning_selected_profile_is_unavailable.message", map[string]any{"detail": fmt.Sprint(profileErr)}, "Warning: selected profile is unavailable: "+fmt.Sprint(profileErr))); err != nil {
				return err
			}
		}
		for _, field := range []struct{ key, label string }{{"pid", "PID"}, {"stale_pid", text(w, "cli.output.writehumanmap.previous_pid", "Previous PID")}, {"version", text(w, "cli.output.writehumanmap.mihomo_version", "Mihomo version")}, {"mixed_port", text(w, "cli.output.writehumanmap.mixed_port", "Mixed port")}, {"socket_path", text(w, "cli.output.writehumanmap.socket", "Socket")}, {"log_path", text(w, "cli.output.writehumanmap.log", "Log")}} {
			if err := printField(w, field.label, v, field.key); err != nil {
				return err
			}
		}
		if err := printSystemDNS(w, v); err != nil {
			return err
		}
		if v["unexpected_exit"] == true {
			return line(w, text(w, "cli.output.writehumanmap.the_previous_mihomo_process_exited_unexpectedly_inspect_logs_wit", "The previous mihomo process exited unexpectedly. Inspect logs with `nagi logs`."))
		}
		return nil
	}
	if enabled, ok := v["enabled"].(bool); ok {
		label := "disabled"
		if enabled {
			label = "enabled"
		}
		kind := "TUN"
		if _, ok := v["bind_address"]; ok {
			kind = text(w, "cli.output.writehumanmap.lan_access", "LAN access")
		}
		if err := stateLine(w, kind, label); err != nil {
			return err
		}
		if err := printSystemDNS(w, v); err != nil {
			return err
		}
		if status, ok := v["adapter_status"].(string); ok {
			name, _ := v["adapter_name"].(string)
			if name != "" {
				return line(w, renderText(w, "cli.output.writehumanmap.adapter.message", map[string]any{"name": fmt.Sprint(name), "state": fmt.Sprint(stateValue(w, status))}, "Adapter "+fmt.Sprint(name)+": "+fmt.Sprint(stateValue(w, status))))
			}
			return stateLine(w, text(w, "cli.output.writehumanmap.adapter_142", "Adapter"), status)
		}
		return printField(w, text(w, "cli.output.writehumanmap.bind_address", "Bind address"), v, "bind_address")
	}
	if _, ok := v["http"]; ok {
		for _, item := range []struct{ key, label string }{{"http", text(w, "cli.output.writehumanmap.http_https_port", "HTTP/HTTPS port")}, {"socks", text(w, "cli.output.writehumanmap.socks_port", "SOCKS port")}, {"mixed", text(w, "cli.output.writehumanmap.mixed_http_socks_port", "Mixed HTTP/SOCKS port")}, {"bind_address", text(w, "cli.output.writehumanmap.bind_address", "Bind address")}, {"allow_lan", text(w, "cli.output.writehumanmap.lan_access", "LAN access")}} {
			if err := printField(w, item.label, v, item.key); err != nil {
				return err
			}
		}
		return nil
	}
	if valid, ok := v["valid"].(bool); ok && valid {
		return line(w, renderText(w, "cli.output.writehumanmap.profile_v_is_valid", map[string]any{"profile": fmt.Sprint(v["profile"])}, fmt.Sprintf("Profile %v is valid.", v["profile"])))
	}
	if name, ok := v["name"].(string); ok {
		switch {
		case v["exported"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.exported_profile_s_to_v", map[string]any{"name": fmt.Sprint(name), "file": fmt.Sprint(v["file"])}, fmt.Sprintf("Exported profile %s to %v.", name, v["file"])))
		case v["backed_up"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.backed_up_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Backed up profile "+fmt.Sprint(name)+"."))
		case v["restored"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.restored_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Restored profile "+fmt.Sprint(name)+" from its backup. If selected and running, mihomo was reloaded."))
		case v["override_saved"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.saved_local_override_for_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Saved local override for profile "+fmt.Sprint(name)+"."))
		case v["override_cleared"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.cleared_local_override_for_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Cleared local override for profile "+fmt.Sprint(name)+"."))
		case v["default_override_restored"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.restored_the_previous_local_override_for_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Restored the previous local override for profile "+fmt.Sprint(name)+"."))
		case v["default_override_applied"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.local_default_settings_are_applied_to_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Local default settings are applied to profile "+fmt.Sprint(name)+"."))
		case v["default_override_applied"] == false:
			return line(w, renderText(w, "cli.output.writehumanmap.local_default_settings_are_not_applied_to_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Local default settings are not applied to profile "+fmt.Sprint(name)+"."))
		case v["changed"] != nil:
			if v["changed"] == false {
				return line(w, text(w, "cli.output.writehumanmap.no_differences", "No differences."))
			}
			if err := line(w, v["diff"].(string)); err != nil {
				return err
			}
			if warnings, ok := v["warnings"].([]string); ok {
				for index, warning := range warnings {
					warning = fieldText(w, fmt.Sprintf("/warnings/%d", index), warning)
					if err := line(w, renderText(w, "cli.output.writehumanmap.warning.message", map[string]any{"detail": fmt.Sprint(warning)}, "Warning: "+fmt.Sprint(warning))); err != nil {
						return err
					}
				}
			}
			return nil
		case v["imported"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.imported_profile_s_run_nagi_profile_use_s_to_select_it", map[string]any{"name": fmt.Sprint(name)}, fmt.Sprintf("Imported profile %s. Run `nagi profile use %s` to select it.", name, name)))
		case v["removed"] == true:
			if kind, _ := v["kind"].(string); kind == "profile" {
				return line(w, renderText(w, "cli.output.writehumanmap.removed_profile.message", map[string]any{"profile": fmt.Sprint(name)}, "Removed profile "+fmt.Sprint(name)+" and its backups, local overrides, and saved proxy choices. Subscriptions and cached downloads were kept."))
			}
			return line(w, renderText(w, "cli.output.writehumanmap.removed_subscription.message", map[string]any{"name": fmt.Sprint(name)}, "Removed subscription "+fmt.Sprint(name)+" and its cache."))
		case v["applied"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.applied_subscription_s_as_selected_profile_v_if_mihomo_was_runni", map[string]any{"name": fmt.Sprint(name), "profile": fmt.Sprint(v["profile"])}, fmt.Sprintf("Applied subscription %s as selected profile %v. If mihomo was running, its configuration was reloaded.", name, v["profile"])))
		case v["schedule_seconds"] != nil:
			seconds, _ := v["schedule_seconds"].(int64)
			if seconds == 0 {
				return line(w, renderText(w, "cli.output.writehumanmap.subscription.message", map[string]any{"name": fmt.Sprint(name)}, "Subscription "+fmt.Sprint(name)+" scheduling is off."))
			}
			return line(w, renderText(w, "cli.output.writehumanmap.subscription_s_will_refresh_every_d_seconds_while_the_scheduler_", map[string]any{"name": fmt.Sprint(name), "interval": seconds}, fmt.Sprintf("Subscription %s will refresh every %d seconds while the scheduler is running.", name, seconds)))
		case v["key_configured"] == true:
			return line(w, renderText(w, "cli.output.writehumanmap.saved_private_age_identity_for_subscription.message", map[string]any{"name": fmt.Sprint(name)}, "Saved private age identity for subscription "+fmt.Sprint(name)+". Run `nagi subscription update "+fmt.Sprint(name)+"` to refresh it."))
		case v["key_configured"] == false:
			return line(w, renderText(w, "cli.output.writehumanmap.cleared_private_age_identity_for_subscription.message", map[string]any{"name": fmt.Sprint(name)}, "Cleared private age identity for subscription "+fmt.Sprint(name)+"."))
		case len(v) == 1:
			return line(w, renderText(w, "cli.output.writehumanmap.saved_subscription_s_run_nagi_subscription_update_s_to_download_", map[string]any{"name": fmt.Sprint(name)}, fmt.Sprintf("Saved subscription %s. Run `nagi subscription update %s` to download it.", name, name)))
		}
	}
	if current, ok := v["current"].(string); ok && len(v) == 1 {
		return line(w, renderText(w, "cli.output.writehumanmap.selected_profile.message", map[string]any{"profile": fmt.Sprint(current)}, "Selected profile "+fmt.Sprint(current)+". If mihomo was running, its configuration was reloaded."))
	}
	if group, ok := v["group"].(string); ok {
		if v["automatic"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.selection_resumed_for.message", map[string]any{"automatic": fmt.Sprint(stateValue(w, "Automatic")), "group": fmt.Sprint(group)}, fmt.Sprint(stateValue(w, "Automatic"))+" selection resumed for "+fmt.Sprint(group)+"; saved choice removed."))
		}
		if node, ok := v["node"].(string); ok {
			return line(w, renderText(w, "cli.output.writehumanmap.selected_s_in_proxy_group_s", map[string]any{"node": fmt.Sprint(node), "group": fmt.Sprint(group)}, fmt.Sprintf("Selected %s in proxy group %s.", node, group)))
		}
	}
	if nodes, ok := v["nodes"].([]profile.TailscaleNode); ok {
		if len(nodes) == 0 {
			return line(w, renderText(w, "cli.output.writehumanmap.no_tailscale_nodes_configured_in_profile.message", map[string]any{"profile": fmt.Sprint(fmt.Sprint(v["profile"]))}, "No Tailscale nodes configured in profile "+fmt.Sprint(fmt.Sprint(v["profile"]))+"."))
		}
		for _, n := range nodes {
			if err := line(w, renderText(w, "cli.output.writehumanmap.s_exit_node_s_hostname_s_control_url_s_auth_key_configured_t", map[string]any{"name": fmt.Sprint(n.Name), "exit": fmt.Sprint(n.ExitNode), "hostname": fmt.Sprint(n.Hostname), "control_url": fmt.Sprint(n.ControlURL), "auth_key_configured": n.AuthKeyConfigured}, fmt.Sprintf("%s: exit node %s; hostname %s; control URL %s; auth key configured: %t", n.Name, n.ExitNode, n.Hostname, n.ControlURL, n.AuthKeyConfigured))); err != nil {
				return err
			}
		}
		return nil
	}
	if n, ok := v["node"].(profile.TailscaleNode); ok {
		if err := line(w, renderText(w, "cli.output.writehumanmap.tailscale_s_exit_node_s_hostname_s_control_url_s_auth_key_config", map[string]any{"name": fmt.Sprint(n.Name), "exit": fmt.Sprint(n.ExitNode), "hostname": fmt.Sprint(n.Hostname), "control_url": fmt.Sprint(n.ControlURL), "auth_key_configured": n.AuthKeyConfigured}, fmt.Sprintf("Tailscale %s: exit node %s; hostname %s; control URL %s; auth key configured: %t", n.Name, n.ExitNode, n.Hostname, n.ControlURL, n.AuthKeyConfigured))); err != nil {
			return err
		}
		bindings, _ := v["bindings"].([]profile.TailscaleBinding)
		if len(bindings) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_targets_bound_to_this_node", "No targets bound to this node."))
		}
		for _, b := range bindings {
			if err := line(w, fmt.Sprintf("%s %s -> %s", b.Kind, b.Name, b.DialerProxy)); err != nil {
				return err
			}
		}
		return nil
	}
	if targets, ok := v["targets"].([]profile.TailscaleTarget); ok {
		if len(targets) == 0 {
			return line(w, text(w, "cli.output.writehumanmap.no_eligible_proxy_nodes_providers_or_applied_subscriptions", "No eligible proxy nodes, providers, or applied subscriptions."))
		}
		for _, t := range targets {
			dialer := t.DialerProxy
			if dialer == "" {
				dialer = "none"
			}
			if err := line(w, renderText(w, "cli.output.writehumanmap.s_s_s_dialer_proxy_s", map[string]any{"kind": fmt.Sprint(t.Kind), "name": fmt.Sprint(t.Name), "type": fmt.Sprint(t.Type), "dialer": fmt.Sprint(dialer)}, fmt.Sprintf("%s %s (%s): dialer proxy %s", t.Kind, t.Name, t.Type, dialer))); err != nil {
				return err
			}
		}
		return nil
	}
	if provider, ok := v["provider"].(string); ok {
		if v["removed"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.removed_proxy_provider.message", map[string]any{"provider": fmt.Sprint(provider), "profile": fmt.Sprint(fmt.Sprint(v["profile"]))}, "Removed proxy provider "+fmt.Sprint(provider)+" from profile "+fmt.Sprint(fmt.Sprint(v["profile"]))+"."))
		}
		if v["added"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.saved_proxy_provider_s_to_group_s_in_profile_s_update_interval_v", map[string]any{"provider": fmt.Sprint(provider), "group": fmt.Sprint(v["group"]), "profile": fmt.Sprint(v["profile"]), "interval": fmt.Sprint(v["interval_seconds"])}, fmt.Sprintf("Saved proxy provider %s to group %s in profile %s (update interval: %v seconds).", provider, v["group"], v["profile"], v["interval_seconds"])))
		}
		if v["updated"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.updated_proxy_provider.message", map[string]any{"provider": fmt.Sprint(provider)}, "Updated proxy provider "+fmt.Sprint(provider)+"."))
		}
		if v["healthcheck_started"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.started_healthcheck_for_proxy_provider.message", map[string]any{"provider": fmt.Sprint(provider)}, "Started healthcheck for proxy provider "+fmt.Sprint(provider)+"."))
		}
	}
	if mode, ok := v["mode"].(string); ok {
		if v["persistent"] == true {
			if v["changed"] == true {
				return line(w, renderText(w, "cli.output.writehumanmap.saved.message", map[string]any{"mode": fmt.Sprint(stateValue(w, mode))}, "Saved "+fmt.Sprint(stateValue(w, mode))+" mode in the selected profile. If mihomo was running, its configuration was reloaded."))
			}
			return line(w, renderText(w, "cli.output.writehumanmap.saved_profile_mode.message", map[string]any{"mode": fmt.Sprint(stateValue(w, mode))}, "Saved profile mode: "+fmt.Sprint(stateValue(w, mode))))
		}
		if v["changed"] == true {
			return line(w, renderText(w, "cli.output.writehumanmap.set_temporary_mihomo_mode_to.message", map[string]any{"mode": fmt.Sprint(stateValue(w, mode))}, "Set temporary mihomo mode to "+fmt.Sprint(stateValue(w, mode))+"."))
		}
		return stateLine(w, text(w, "cli.output.writehumanmap.mihomo_mode", "Mihomo mode"), mode)
	}
	if v["reloaded"] == true {
		return line(w, renderText(w, "cli.output.writehumanmap.reloaded_validated_profile_v_in_the_running_mihomo_instance", map[string]any{"profile": fmt.Sprint(v["profile"])}, fmt.Sprintf("Reloaded validated profile %v in the running mihomo instance.", v["profile"])))
	}
	if closed, ok := v["closed"].(bool); ok && closed {
		if id, ok := v["id"].(string); ok {
			return line(w, renderText(w, "cli.output.writehumanmap.closed_connection.message", map[string]any{"id": fmt.Sprint(id)}, "Closed connection "+fmt.Sprint(id)+"."))
		}
		return line(w, text(w, "cli.output.writehumanmap.closed_all_connections", "Closed all connections."))
	}
	if _, ok := v["nagi"].(string); ok {
		for _, field := range []struct{ key, label string }{{"nagi", "Nagi"}, {"mihomo", "Mihomo"}, {"mihomo_commit", text(w, "cli.output.writehumanmap.mihomo_commit", "Mihomo commit")}, {"os", text(w, "cli.output.writehumanmap.os", "OS")}, {"arch", text(w, "cli.output.writehumanmap.architecture", "Architecture")}} {
			if err := printField(w, field.label, v, field.key); err != nil {
				return err
			}
		}
		return nil
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func writeServiceResult(w io.Writer, result service.Result) error {
	if err := line(w, renderText(w, "cli.output.writeserviceresult.service_manager.message", map[string]any{"manager": fmt.Sprint(stateValue(w, result.Manager))}, "Service manager: "+fmt.Sprint(stateValue(w, result.Manager)))); err != nil {
		return err
	}
	for _, item := range []struct {
		label string
		value bool
	}{{text(w, "cli.output.writeserviceresult.installed", "Installed"), result.Installed}, {text(w, "cli.output.writeserviceresult.enabled", "Enabled"), result.Enabled}, {text(w, "cli.output.writeserviceresult.active", "Active"), result.Active}} {
		if err := stateLine(w, item.label, yesNo(item.value)); err != nil {
			return err
		}
	}
	return line(w, renderText(w, "cli.output.writeserviceresult.service_file.message", map[string]any{"path": fmt.Sprint(result.Path)}, "Service file: "+fmt.Sprint(result.Path)))
}

func writePreview(w io.Writer, p subscription.Preview) error {
	if err := line(w, renderText(w, "cli.output.writepreview.subscription.message", map[string]any{"name": fmt.Sprint(p.Name), "format": fmt.Sprint(p.Format)}, "Subscription "+fmt.Sprint(p.Name)+" ("+fmt.Sprint(p.Format)+") node changes:")); err != nil {
		return err
	}
	for _, item := range []struct {
		label string
		names []string
	}{{text(w, "cli.output.writepreview.added", "Added"), p.Added}, {text(w, "cli.output.writepreview.removed", "Removed"), p.Removed}, {text(w, "cli.output.writepreview.changed", "Changed"), p.Changed}} {
		value := stateText(w, "none")
		if len(item.names) > 0 {
			value = strings.Join(item.names, ", ")
		}
		if err := line(w, item.label+": "+value); err != nil {
			return err
		}
	}
	if err := line(w, text(w, "cli.output.writepreview.configuration_changes", "Configuration changes:")); err != nil {
		return err
	}
	if len(p.Changes) == 0 {
		if err := line(w, text(w, "cli.output.writepreview.none", "  none")); err != nil {
			return err
		}
	}
	for _, change := range p.Changes {
		if err := line(w, "  "+change.Kind+" "+change.Path+": "+RenderMessageField(change, "/behavior", change.Behavior, writerLanguage(w))); err != nil {
			return err
		}
	}
	if !p.RemoteNodesCompared {
		return line(w, text(w, "cli.output.writepreview.remote_provider_nodes_were_not_compared", "Remote provider nodes were not compared."))
	}
	return nil
}

func writeConnection(w io.Writer, c proxy.Connection) error {
	label := renderText(w, "cli.output.writeconnection.connection_s", map[string]any{"id": fmt.Sprint(c.ID)}, fmt.Sprintf("Connection %s", c.ID))
	if destination := connectionDestination(c.Metadata); destination != "" {
		label += " -> " + destination
	}
	if len(c.Chains) > 0 {
		label += text(w, "cli.output.writeconnection.proxy_chain", "\nProxy chain: ") + strings.Join(c.Chains, " -> ")
	}
	label += renderText(w, "cli.output.writeconnection.upload_d_b_download_d_b", map[string]any{"upload": c.Upload, "download": c.Download}, fmt.Sprintf("\nUpload: %d B\nDownload: %d B", c.Upload, c.Download))
	return line(w, label)
}

func printField(w io.Writer, label string, fields map[string]any, key string) error {
	value, ok := fields[key]
	if !ok || value == nil {
		return nil
	}
	if text, ok := value.(string); ok && text == "" {
		return nil
	}
	text := fmt.Sprint(value)
	if key == "profile" {
		text = StyleProfileValue(text, StylesEnabled(w))
	}
	return line(w, fmt.Sprintf("%s: %s", label, text))
}

func connectionDestination(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	var host string
	for _, key := range []string{"host", "destinationIP"} {
		if value, ok := metadata[key].(string); ok && value != "" {
			host = value
			break
		}
	}
	if host == "" {
		return ""
	}
	if port, ok := metadata["destinationPort"]; ok && port != nil {
		if text, ok := port.(string); !ok || text != "" {
			return fmt.Sprintf("%s:%v", host, port)
		}
	}
	return host
}

func writeRaw(w io.Writer, value string) error {
	if value == "" {
		return nil
	}
	if _, err := io.WriteString(w, value); err != nil {
		return err
	}
	if !strings.HasSuffix(value, "\n") {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

func WriteError(w io.Writer, jsonMode bool, code, message string) {
	message = RedactError(message)
	if jsonMode {
		_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: &Failure{Code: code, Message: message}})
		return
	}
	_, _ = fmt.Fprintf(w, "%s (%s): %s\n", stateValue(w, "Error"), code, message)
	if code == "usage" {
		_, _ = io.WriteString(w, text(w, "cli.output.writeerror.run_nagi_help_for_usage", "Run `nagi --help` for usage.\n"))
	}
}

func printSystemDNS(w io.Writer, data map[string]any) error {
	if state, ok := data["system_dns"].(traffic.DNSState); ok && state.Supported {
		if err := stateLine(w, text(w, "cli.output.printsystemdns.system_dns", "System DNS"), state.Status); err != nil {
			return err
		}
		if len(state.Targets) > 0 {
			if err := line(w, renderText(w, "cli.output.printsystemdns.dns_targets.message", map[string]any{"targets": fmt.Sprint(strings.Join(state.Targets, ", "))}, "DNS targets: "+fmt.Sprint(strings.Join(state.Targets, ", ")))); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"system_dns_error", "system_dns_recovery_error"} {
		if message, ok := data[key].(string); ok && message != "" {
			message = fieldText(w, "/"+key, message)
			if err := line(w, renderText(w, "cli.output.printsystemdns.system_dns_needs_attention.message", map[string]any{"detail": fmt.Sprint(RedactError(message))}, "System DNS needs attention: "+fmt.Sprint(RedactError(message))+". Inspect `nagi tun status` or retry `nagi stop`.")); err != nil {
				return err
			}
		}
	}
	return nil
}
