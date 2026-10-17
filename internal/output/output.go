package output

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/engine"
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
	OK    bool     `json:"ok"`
	Data  any      `json:"data,omitempty"`
	Error *Failure `json:"error,omitempty"`
}

func Write(w io.Writer, jsonMode bool, data any) error {
	if jsonMode {
		return json.NewEncoder(w).Encode(Envelope{OK: true, Data: data})
	}
	switch v := data.(type) {
	case profile.ProviderInterval:
		return line(w, fmt.Sprintf("provider %s: download interval %d seconds (%s); enabled %t", v.Name, v.IntervalSeconds, v.Source, v.Enabled))
	case profile.Healthcheck:
		enabled := "unknown"
		if v.Enabled != nil {
			enabled = fmt.Sprint(*v.Enabled)
		}
		if v.Kind == "subscription" && v.IntervalDefaulted {
			if err := line(w, fmt.Sprintf("subscription %s: follows subscription configuration and mihomo defaults", v.Name)); err != nil {
				return err
			}
		} else if err := line(w, fmt.Sprintf("%s %s: interval %d seconds (%s); enabled %s; lazy %t", v.Kind, v.Name, v.IntervalSeconds, v.Source, enabled, v.Lazy)); err != nil {
			return err
		}
		if v.TestURL != "" {
			if err := line(w, "Test URL: "+v.TestURL+" ("+v.URLSource+")"); err != nil {
				return err
			}
		}
		return line(w, v.Reason)
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
			if err := stateLine(w, "Diagnostics", "healthy"); err != nil {
				return err
			}
		} else {
			if err := line(w, "Diagnostics: needs "+stateValue(w, "attention")); err != nil {
				return err
			}
		}
		for _, check := range v.Checks {
			if err := line(w, "["+stateValue(w, strings.ToUpper(check.Status))+"] "+check.Name+": "+check.Message); err != nil {
				return err
			}
		}
		return nil
	case diagnostic.Report:
		if v.Healthy {
			if err := stateLine(w, "Nagi doctor", "healthy"); err != nil {
				return err
			}
		} else {
			if err := line(w, "Nagi doctor: needs "+stateValue(w, "attention")); err != nil {
				return err
			}
		}
		for _, check := range v.Checks {
			if err := line(w, "["+stateValue(w, strings.ToUpper(check.Status))+"] "+check.Name+": "+check.Message); err != nil {
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
			if err := line(w, "No rules configured."); err != nil {
				return err
			}
		}
		for _, conflict := range v.Conflicts {
			if err := line(w, fmt.Sprintf("Conflict: rule %d shadows rule %d (%s)", conflict.Earlier, conflict.Later, conflict.Reason)); err != nil {
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
		return line(w, fmt.Sprintf("Proxy %s delay: %s (URL: %s, timeout: %d ms)", v.Proxy, latencyText(v.DelayMS), v.URL, v.TimeoutMS))
	case proxy.BatchDelayResult:
		if err := line(w, "Proxy group "+v.Group+" latency:"); err != nil {
			return err
		}
		if v.AutomaticReset && v.SavedSelectionRemoved {
			if err := line(w, "Automatic selection resumed; saved choice removed."); err != nil {
				return err
			}
		}
		if len(v.Results) == 0 {
			return line(w, "No nodes in this group.")
		}
		for _, item := range v.Results {
			value := "latency unavailable"
			if item.DelayMS != nil {
				value = latencyText(*item.DelayMS)
			}
			if item.Error != "" {
				value = item.Error
			}
			if item.Unmeasurable != "" {
				value = "no latency (" + item.Unmeasurable + ")"
			}
			if err := line(w, item.Proxy+": "+value); err != nil {
				return err
			}
		}
		return nil
	case proxy.RestoreResult:
		if len(v.Restored) == 0 && len(v.Unavailable) == 0 && len(v.Failed) == 0 {
			return line(w, "No saved proxy selections for this profile.")
		}
		for _, item := range v.Restored {
			if err := line(w, "Restored: "+item); err != nil {
				return err
			}
		}
		for _, item := range v.Unavailable {
			if err := line(w, stateValue(w, "unavailable")+" or "+stateValue(w, "removed")+": "+item); err != nil {
				return err
			}
		}
		for _, item := range v.Failed {
			if err := line(w, "Could not restore: "+item); err != nil {
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
		if err := line(w, fmt.Sprintf("Downloaded subscription %s (%d bytes).", v.Name, v.Bytes)); err != nil {
			return err
		}
		if v.Preview != nil {
			if err := writePreview(w, *v.Preview); err != nil {
				return err
			}
		}
		return line(w, fmt.Sprintf("Run `nagi subscription apply %s` to select its profile.", v.Name))
	case subscription.Preview:
		return writePreview(w, v)
	case traffic.State:
		state := "disabled"
		if v.Enabled {
			state = "enabled"
		}
		if err := line(w, "System proxy: "+stateValue(w, state)+" ("+stateValue(w, v.Backend)+")"); err != nil {
			return err
		}
		for _, item := range []struct {
			name  string
			value traffic.Setting
		}{{"HTTP", v.HTTP}, {"HTTPS", v.HTTPS}, {"SOCKS", v.SOCKS}} {
			label := "off"
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
		if err := line(w, fmt.Sprintf("Previous PID: %d", v.StalePID)); err != nil {
			return err
		}
	}
	if v.SocketPath != "" {
		if err := line(w, "Socket: "+v.SocketPath); err != nil {
			return err
		}
	}
	if v.LogPath != "" {
		if err := line(w, "Log: "+v.LogPath); err != nil {
			return err
		}
	}
	if v.UnexpectedExit {
		return line(w, "The previous mihomo process exited unexpectedly. Inspect logs with `nagi logs`.")
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
		return line(w, "  No nodes available in this group.")
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
	if err := line(w, fmt.Sprintf("%s (%s, %s): %d nodes", p.Name, p.Type, p.VehicleType, len(p.Nodes))); err != nil {
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
	if message, ok := v["error"].(string); ok && len(v) == 1 {
		if message == "" {
			return line(w, "No background update failure is pending.")
		}
		return line(w, "Last background update error: "+RedactError(message))
	}
	if action, ok := v["action"].(string); ok && (v["completed"] == true || v["scheduled"] == true) {
		if v["scheduled"] == true {
			if v["background"] == true {
				return line(w, "Update scheduled. Nagi will reopen after the background update completes.")
			}
			return line(w, "Update opened in Terminal. Quit Nagi.app to continue; it will reopen after a successful update.")
		}
		switch action {
		case "app-install":
			return line(w, "Installed Nagi.app. Open it from Applications.")
		case "app-uninstall":
			return line(w, "Removed Nagi.app. The CLI and user data are preserved.")
		case "uninstall":
			if v["user_data_removed"] == true {
				return line(w, "Removed Nagi and its user data.")
			}
			return line(w, "Removed Nagi. User profiles, subscriptions, settings, and logs are preserved.")
		case "update":
			return line(w, "Updated Nagi CLI, mihomo, and any installed App. The default profile was refreshed.")
		}
	}
	if app, ok := v["app"].(string); ok && v["managed"] == true {
		if v["installed"] == true {
			return line(w, "Managed Nagi App: "+app)
		}
		return line(w, "Nagi App is not installed. Run `nagi app install`.")
	}
	if v["quit"] == true {
		return line(w, "Nagi "+stateValue(w, "stopped")+" for this login session. Login startup preference is unchanged.")
	}
	if v["flushed"] == true {
		return line(w, "Cleared mihomo DNS cache.")
	}
	if v["exception_changed"] == true {
		return line(w, fmt.Sprintf("Updated DNS exception for %v.", v["domain"]))
	}
	if v["saved"] == true && v["action"] != nil && v["profile"] != nil && v["reloaded"] != nil {
		state := "Saved; mihomo is stopped. Run `nagi start` to activate."
		if v["reloaded"] == true {
			state = "Saved and reloaded mihomo."
		}
		if v["kind"] == "subscription" {
			if v["action"] == "bind" {
				state += " Entire-subscription policy survives apply and scheduled refresh; new nodes inherit the exit."
			} else {
				state += " Removed entire-subscription policy; retained the Tailscale node."
			}
		}
		if targets, ok := v["targets"].([]string); ok {
			return line(w, fmt.Sprintf("Tailscale %s: %d %s targets in profile %s. %s", v["action"], len(targets), v["kind"], v["profile"], state))
		}
		return line(w, fmt.Sprintf("Tailscale %s in profile %s. %s", v["action"], v["profile"], state))
	}

	if v["saved"] == true && v["profile"] != nil && v["action"] == nil {
		if enabled, ok := v["tun_enabled"].(bool); ok {
			return line(w, "Saved TUN DNS interception setting: "+stateValue(w, yesNo(enabled))+".")
		}
		return line(w, fmt.Sprintf("Saved DNS policy for profile %v.", v["profile"]))
	}
	if domain, ok := v["domain"].(string); ok && v["response"] != nil {
		return line(w, fmt.Sprintf("Mihomo DNS response for %s (%v): %v", domain, v["type"], v["response"]))
	}
	if _, ok := v["upstream_hosts"].([]string); ok {
		if err := line(w, fmt.Sprintf("DNS profile: %v | enabled: %s | IPv6: %s | DoH policy: %s | TUN: %s", v["profile"], stateValue(w, yesNo(v["enabled"] == true)), stateValue(w, yesNo(v["ipv6"] == true)), stateValue(w, fmt.Sprint(v["policy"])), stateValue(w, yesNo(v["tun_enabled"] == true)))); err != nil {
			return err
		}
		if queryOK, ok := v["engine_query_ok"].(bool); ok {
			if err := line(w, fmt.Sprintf("Mihomo DNS query succeeded: %t", queryOK)); err != nil {
				return err
			}
		}
		for _, issue := range v["issues"].([]string) {
			if err := line(w, "Warning: "+issue); err != nil {
				return err
			}
		}
		if scope, ok := v["scope"].(string); ok {
			return line(w, "Scope: "+scope)
		}
		return nil
	}
	if yaml, ok := v["yaml"].(string); ok {
		return writeRaw(w, yaml)
	}
	if lines, ok := v["lines"].([]string); ok {
		if len(lines) == 0 {
			return line(w, "No log lines available.")
		}
		return writeRaw(w, strings.Join(lines, "\n"))
	}
	if profiles, ok := v["profiles"].([]string); ok {
		current, _ := v["current"].(string)
		if len(profiles) == 0 {
			if err := line(w, "No profiles found. Import one with `nagi profile import NAME FILE`."); err != nil {
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
			return line(w, "Warning: selected profile is unavailable: "+currentErr)
		}
		return nil
	}
	if groups, ok := v["groups"].([]proxy.Group); ok {
		if len(groups) == 0 {
			if _, searched := v["query"]; searched {
				return line(w, "No matching nodes found.")
			}
			return line(w, "No proxy groups configured. Check the selected profile with `nagi config show`.")
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
			return line(w, "No proxy providers configured.")
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
		if _, err := fmt.Fprintln(w, "Scheduler: "+stateValue(w, scheduler)); err != nil {
			return err
		}
		if len(entries) == 0 {
			return line(w, "No subscriptions configured. Add one with `nagi subscription add NAME URL`.")
		}
		for _, entry := range entries {
			label := entry.Name
			if entry.KeyConfigured {
				label += "  Age key configured"
			}
			if !entry.UpdatedAt.IsZero() {
				label += "  Updated: " + entry.UpdatedAt.Format("2006-01-02 15:04 MST")
			} else {
				label += "  Never updated"
			}
			if !entry.ExpiresAt.IsZero() {
				label += "  Expires: " + entry.ExpiresAt.Format("2006-01-02 15:04 MST")
			}
			if entry.Total > 0 {
				label += fmt.Sprintf("  Traffic: %d/%d bytes", entry.Upload+entry.Download, entry.Total)
			}
			if entry.ScheduleSeconds == 0 {
				label += "  Schedule: off"
			} else {
				label += fmt.Sprintf("  Schedule: %ds", entry.ScheduleSeconds)
			}
			if !entry.NextDue.IsZero() {
				label += "  Next due: " + entry.NextDue.Format("2006-01-02 15:04 MST")
			}
			if entry.Profile != "" {
				label += "  Profile: " + entry.Profile
			}
			if entry.CacheVersion != "" {
				label += "  Cache version: " + entry.CacheVersion
			}
			if entry.AppliedCacheVersion != "" {
				label += "  Applied version: " + entry.AppliedCacheVersion
			}
			if entry.LastFailureStage != "" {
				label += "  Last failure: " + entry.LastFailureStage + " (" + entry.LastFailureReason + ")"
			}
			if entry.PauseReason != "" {
				label += "  Automatic apply paused: " + entry.PauseReason
			}

			if err := line(w, label); err != nil {
				return err
			}
		}
		return nil
	}
	if connections, ok := v["connections"].([]proxy.Connection); ok {
		if len(connections) == 0 {
			return line(w, "No active connections.")
		}
		for _, connection := range connections {
			destination := connectionDestination(connection.Metadata)
			if destination == "" {
				destination = "Destination unavailable"
			}
			label := fmt.Sprintf("%s  ID: %s  Upload: %d B  Download: %d B", destination, connection.ID, connection.Upload, connection.Download)
			if len(connection.Chains) > 0 {
				label += "  Chain: " + strings.Join(connection.Chains, " -> ")
			}
			if err := line(w, label); err != nil {
				return err
			}
		}
		return nil
	}
	if entries, ok := v["entries"].([]rules.Entry); ok {
		if name, ok := v["profile"].(string); ok {
			if err := line(w, "Profile: "+name); err != nil {
				return err
			}
		}
		if len(entries) == 0 {
			return line(w, "No custom rules. Add one with `nagi rules add NAME TYPE PAYLOAD TARGET`.")
		}
		for _, entry := range entries {
			state := "disabled"
			if entry.Enabled {
				state = "enabled"
			}
			if entry.Kind == "rules" {
				if err := line(w, fmt.Sprintf("%s (%s): %d imported rules", entry.Name, stateValue(w, state), len(entry.Items))); err != nil {
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
			return line(w, "No active rules reported by mihomo.")
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
			return line(w, "No active rule providers.")
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
			return line(w, fmt.Sprintf("Connection %s matched: %s %v", id, rule, v["rule_payload"]))
		}
	}
	if name, ok := v["name"].(string); ok && v["saved"] == true {
		if profile, ok := v["profile"].(string); ok {
			if err := line(w, "Profile: "+profile); err != nil {
				return err
			}
		}
		return line(w, fmt.Sprintf("Rule %s: %v. Effective configuration was validated; running mihomo was reloaded if active.", name, v["action"]))
	}
	if running, ok := v["running"].(bool); ok {
		state := "stopped"
		if running {
			state = "running"
		}
		if err := stateLine(w, "Mihomo", state); err != nil {
			return err
		}
		if err := printField(w, "Profile", v, "profile"); err != nil {
			return err
		}
		if profileErr, ok := v["profile_error"].(string); ok && profileErr != "" {
			if err := line(w, "Warning: selected profile is unavailable: "+profileErr); err != nil {
				return err
			}
		}
		for _, field := range []struct{ key, label string }{{"pid", "PID"}, {"stale_pid", "Previous PID"}, {"version", "Mihomo version"}, {"mixed_port", "Mixed port"}, {"socket_path", "Socket"}, {"log_path", "Log"}} {
			if err := printField(w, field.label, v, field.key); err != nil {
				return err
			}
		}
		if err := printSystemDNS(w, v); err != nil {
			return err
		}
		if v["unexpected_exit"] == true {
			return line(w, "The previous mihomo process exited unexpectedly. Inspect logs with `nagi logs`.")
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
			kind = "LAN access"
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
				return line(w, "Adapter "+name+": "+stateValue(w, status))
			}
			return stateLine(w, "Adapter", status)
		}
		return printField(w, "Bind address", v, "bind_address")
	}
	if _, ok := v["http"]; ok {
		for _, item := range []struct{ key, label string }{{"http", "HTTP/HTTPS port"}, {"socks", "SOCKS port"}, {"mixed", "Mixed HTTP/SOCKS port"}, {"bind_address", "Bind address"}, {"allow_lan", "LAN access"}} {
			if err := printField(w, item.label, v, item.key); err != nil {
				return err
			}
		}
		return nil
	}
	if valid, ok := v["valid"].(bool); ok && valid {
		return line(w, fmt.Sprintf("Profile %v is valid.", v["profile"]))
	}
	if name, ok := v["name"].(string); ok {
		switch {
		case v["exported"] == true:
			return line(w, fmt.Sprintf("Exported profile %s to %v.", name, v["file"]))
		case v["backed_up"] == true:
			return line(w, "Backed up profile "+name+".")
		case v["restored"] == true:
			return line(w, "Restored profile "+name+" from its backup. If selected and running, mihomo was reloaded.")
		case v["override_saved"] == true:
			return line(w, "Saved local override for profile "+name+".")
		case v["override_cleared"] == true:
			return line(w, "Cleared local override for profile "+name+".")
		case v["default_override_restored"] == true:
			return line(w, "Restored the previous local override for profile "+name+".")
		case v["default_override_applied"] == true:
			return line(w, "Local default settings are applied to profile "+name+".")
		case v["default_override_applied"] == false:
			return line(w, "Local default settings are not applied to profile "+name+".")
		case v["changed"] != nil:
			if v["changed"] == false {
				return line(w, "No differences.")
			}
			if err := line(w, v["diff"].(string)); err != nil {
				return err
			}
			if warnings, ok := v["warnings"].([]string); ok {
				for _, warning := range warnings {
					if err := line(w, "Warning: "+warning); err != nil {
						return err
					}
				}
			}
			return nil
		case v["imported"] == true:
			return line(w, fmt.Sprintf("Imported profile %s. Run `nagi profile use %s` to select it.", name, name))
		case v["removed"] == true:
			if kind, _ := v["kind"].(string); kind == "profile" {
				return line(w, "Removed profile "+name+" and its backups, local overrides, and saved proxy choices. Subscriptions and cached downloads were kept.")
			}
			return line(w, "Removed subscription "+name+" and its cache.")
		case v["applied"] == true:
			return line(w, fmt.Sprintf("Applied subscription %s as selected profile %v. If mihomo was running, its configuration was reloaded.", name, v["profile"]))
		case v["schedule_seconds"] != nil:
			seconds, _ := v["schedule_seconds"].(int64)
			if seconds == 0 {
				return line(w, "Subscription "+name+" scheduling is off.")
			}
			return line(w, fmt.Sprintf("Subscription %s will refresh every %d seconds while the scheduler is running.", name, seconds))
		case v["key_configured"] == true:
			return line(w, "Saved private age identity for subscription "+name+". Run `nagi subscription update "+name+"` to refresh it.")
		case v["key_configured"] == false:
			return line(w, "Cleared private age identity for subscription "+name+".")
		case len(v) == 1:
			return line(w, fmt.Sprintf("Saved subscription %s. Run `nagi subscription update %s` to download it.", name, name))
		}
	}
	if current, ok := v["current"].(string); ok && len(v) == 1 {
		return line(w, "Selected profile "+current+". If mihomo was running, its configuration was reloaded.")
	}
	if group, ok := v["group"].(string); ok {
		if v["automatic"] == true {
			return line(w, stateValue(w, "Automatic")+" selection resumed for "+group+"; saved choice removed.")
		}
		if node, ok := v["node"].(string); ok {
			return line(w, fmt.Sprintf("Selected %s in proxy group %s.", node, group))
		}
	}
	if nodes, ok := v["nodes"].([]profile.TailscaleNode); ok {
		if len(nodes) == 0 {
			return line(w, "No Tailscale nodes configured in profile "+fmt.Sprint(v["profile"])+".")
		}
		for _, n := range nodes {
			if err := line(w, fmt.Sprintf("%s: exit node %s; hostname %s; control URL %s; auth key configured: %t", n.Name, n.ExitNode, n.Hostname, n.ControlURL, n.AuthKeyConfigured)); err != nil {
				return err
			}
		}
		return nil
	}
	if n, ok := v["node"].(profile.TailscaleNode); ok {
		if err := line(w, fmt.Sprintf("Tailscale %s: exit node %s; hostname %s; control URL %s; auth key configured: %t", n.Name, n.ExitNode, n.Hostname, n.ControlURL, n.AuthKeyConfigured)); err != nil {
			return err
		}
		bindings, _ := v["bindings"].([]profile.TailscaleBinding)
		if len(bindings) == 0 {
			return line(w, "No targets bound to this node.")
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
			return line(w, "No eligible proxy nodes, providers, or applied subscriptions.")
		}
		for _, t := range targets {
			dialer := t.DialerProxy
			if dialer == "" {
				dialer = "none"
			}
			if err := line(w, fmt.Sprintf("%s %s (%s): dialer proxy %s", t.Kind, t.Name, t.Type, dialer)); err != nil {
				return err
			}
		}
		return nil
	}
	if provider, ok := v["provider"].(string); ok {
		if v["removed"] == true {
			return line(w, "Removed proxy provider "+provider+" from profile "+fmt.Sprint(v["profile"])+".")
		}
		if v["added"] == true {
			return line(w, fmt.Sprintf("Saved proxy provider %s to group %s in profile %s (update interval: %v seconds).", provider, v["group"], v["profile"], v["interval_seconds"]))
		}
		if v["updated"] == true {
			return line(w, "Updated proxy provider "+provider+".")
		}
		if v["healthcheck_started"] == true {
			return line(w, "Started healthcheck for proxy provider "+provider+".")
		}
	}
	if mode, ok := v["mode"].(string); ok {
		if v["persistent"] == true {
			if v["changed"] == true {
				return line(w, "Saved "+stateValue(w, mode)+" mode in the selected profile. If mihomo was running, its configuration was reloaded.")
			}
			return line(w, "Saved profile mode: "+stateValue(w, mode))
		}
		if v["changed"] == true {
			return line(w, "Set temporary mihomo mode to "+stateValue(w, mode)+".")
		}
		return stateLine(w, "Mihomo mode", mode)
	}
	if v["reloaded"] == true {
		return line(w, fmt.Sprintf("Reloaded validated profile %v in the running mihomo instance.", v["profile"]))
	}
	if closed, ok := v["closed"].(bool); ok && closed {
		if id, ok := v["id"].(string); ok {
			return line(w, "Closed connection "+id+".")
		}
		return line(w, "Closed all connections.")
	}
	if _, ok := v["nagi"].(string); ok {
		for _, field := range []struct{ key, label string }{{"nagi", "Nagi"}, {"mihomo", "Mihomo"}, {"mihomo_commit", "Mihomo commit"}, {"os", "OS"}, {"arch", "Architecture"}} {
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
	if err := line(w, "Service manager: "+stateValue(w, result.Manager)); err != nil {
		return err
	}
	for _, item := range []struct {
		label string
		value bool
	}{{"Installed", result.Installed}, {"Enabled", result.Enabled}, {"Active", result.Active}} {
		if err := stateLine(w, item.label, yesNo(item.value)); err != nil {
			return err
		}
	}
	return line(w, "Service file: "+result.Path)
}

func writePreview(w io.Writer, p subscription.Preview) error {
	if err := line(w, "Subscription "+p.Name+" ("+p.Format+") node changes:"); err != nil {
		return err
	}
	for _, item := range []struct {
		label string
		names []string
	}{{"Added", p.Added}, {"Removed", p.Removed}, {"Changed", p.Changed}} {
		value := "none"
		if len(item.names) > 0 {
			value = strings.Join(item.names, ", ")
		}
		if err := line(w, item.label+": "+value); err != nil {
			return err
		}
	}
	if err := line(w, "Configuration changes:"); err != nil {
		return err
	}
	if len(p.Changes) == 0 {
		if err := line(w, "  none"); err != nil {
			return err
		}
	}
	for _, change := range p.Changes {
		if err := line(w, "  "+change.Kind+" "+change.Path+": "+change.Behavior); err != nil {
			return err
		}
	}
	if !p.RemoteNodesCompared {
		return line(w, "Remote provider nodes were not compared.")
	}
	return nil
}

func writeConnection(w io.Writer, c proxy.Connection) error {
	label := fmt.Sprintf("Connection %s", c.ID)
	if destination := connectionDestination(c.Metadata); destination != "" {
		label += " -> " + destination
	}
	if len(c.Chains) > 0 {
		label += "\nProxy chain: " + strings.Join(c.Chains, " -> ")
	}
	label += fmt.Sprintf("\nUpload: %d B\nDownload: %d B", c.Upload, c.Download)
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
	if label == "Profile" {
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
		_, _ = io.WriteString(w, "Run `nagi --help` for usage.\n")
	}
}

func printSystemDNS(w io.Writer, data map[string]any) error {
	if state, ok := data["system_dns"].(traffic.DNSState); ok && state.Supported {
		if err := stateLine(w, "System DNS", state.Status); err != nil {
			return err
		}
		if len(state.Targets) > 0 {
			if err := line(w, "DNS targets: "+strings.Join(state.Targets, ", ")); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"system_dns_error", "system_dns_recovery_error"} {
		if message, ok := data[key].(string); ok && message != "" {
			if err := line(w, "System DNS needs attention: "+RedactError(message)+". Inspect `nagi tun status` or retry `nagi stop`."); err != nil {
				return err
			}
		}
	}
	return nil
}
