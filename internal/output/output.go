package output

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/engine"
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
			if err := line(w, "Diagnostics: healthy"); err != nil {
				return err
			}
		} else {
			if err := line(w, "Diagnostics: needs attention"); err != nil {
				return err
			}
		}
		for _, check := range v.Checks {
			if err := line(w, fmt.Sprintf("[%s] %s: %s", strings.ToUpper(check.Status), check.Name, check.Message)); err != nil {
				return err
			}
		}
		return nil
	case diagnostic.Report:
		if v.Healthy {
			if err := line(w, "Nagi doctor: healthy"); err != nil {
				return err
			}
		} else {
			if err := line(w, "Nagi doctor: needs attention"); err != nil {
				return err
			}
		}
		for _, check := range v.Checks {
			if err := line(w, fmt.Sprintf("[%s] %s: %s", strings.ToUpper(check.Status), check.Name, check.Message)); err != nil {
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
	case proxy.Group:
		return writeGroup(w, v)
	case proxy.Connection:
		return writeConnection(w, v)
	case proxy.DelayResult:
		return line(w, fmt.Sprintf("Proxy %s delay: %d ms (URL: %s, timeout: %d ms)", v.Proxy, v.DelayMS, v.URL, v.TimeoutMS))
	case proxy.BatchDelayResult:
		if err := line(w, "Proxy group "+v.Group+" latency:"); err != nil {
			return err
		}
		if len(v.Results) == 0 {
			return line(w, "No nodes in this group.")
		}
		for _, item := range v.Results {
			value := "latency unavailable"
			if item.DelayMS != nil {
				value = fmt.Sprintf("%d ms", *item.DelayMS)
			}
			if item.Error != "" {
				value = item.Error
			}
			if err := line(w, item.Proxy+": "+value); err != nil {
				return err
			}
		}
		return nil
	case proxy.RestoreResult:
		if len(v.Restored) == 0 && len(v.Unavailable) == 0 {
			return line(w, "No saved proxy selections for this profile.")
		}
		for _, item := range v.Restored {
			if err := line(w, "Restored: "+item); err != nil {
				return err
			}
		}
		for _, item := range v.Unavailable {
			if err := line(w, "Unavailable or removed: "+item); err != nil {
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
		if err := line(w, "System proxy: "+state+" ("+v.Backend+")"); err != nil {
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
		return line(w, fmt.Sprintf("Service manager: %s\nService file: %s", v.Manager, v.Path))
	case map[string]any:
		return writeHumanMap(w, v)
	default:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	}
}

func line(w io.Writer, s string) error {
	_, err := io.WriteString(w, s+"\n")
	return err
}

func writeLifecycle(w io.Writer, v engine.Status) error {
	state := "stopped"
	if v.Running {
		state = "running"
	}
	if err := line(w, "Mihomo: "+state); err != nil {
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
		heading += " (" + g.SelectedStatus + ")"
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

func writeHumanMap(w io.Writer, v map[string]any) error {
	if v["flushed"] == true {
		return line(w, "Cleared mihomo DNS cache.")
	}
	if v["exception_changed"] == true {
		return line(w, fmt.Sprintf("Updated DNS exception for %v.", v["domain"]))
	}
	if v["saved"] == true {
		if enabled, ok := v["tun_enabled"].(bool); ok {
			return line(w, fmt.Sprintf("Saved TUN DNS interception setting: %t.", enabled))
		}
		return line(w, fmt.Sprintf("Saved DNS policy for profile %v.", v["profile"]))
	}
	if domain, ok := v["domain"].(string); ok && v["response"] != nil {
		return line(w, fmt.Sprintf("Mihomo DNS response for %s (%v): %v", domain, v["type"], v["response"]))
	}
	if _, ok := v["leak_protection_verified"].(bool); ok {
		if err := line(w, fmt.Sprintf("DNS profile: %v | enabled: %v | IPv6: %v | DoH policy: %v | TUN: %v", v["profile"], v["enabled"], v["ipv6"], v["policy"], v["tun_enabled"])); err != nil {
			return err
		}
		if queryOK, ok := v["engine_query_ok"].(bool); ok {
			if err := line(w, fmt.Sprintf("Mihomo DNS query succeeded: %t", queryOK)); err != nil {
				return err
			}
		}
		if err := line(w, fmt.Sprintf("Leak protection verified: %v", v["leak_protection_verified"])); err != nil {
			return err
		}
		for _, issue := range v["issues"].([]string) {
			if err := line(w, "Warning: "+issue); err != nil {
				return err
			}
		}
		if evidence, ok := v["system_evidence"].(map[string]any); ok {
			routes, _ := evidence["route_interfaces"].(map[string]string)
			resolvers, _ := evidence["resolvers"].([]string)
			if err := line(w, fmt.Sprintf("System routes (%v): IPv4 %q, IPv6 %q (empty means unavailable)", evidence["route_source"], routes["ipv4"], routes["ipv6"])); err != nil {
				return err
			}
			if err := line(w, fmt.Sprintf("System resolvers (%v): %v", evidence["resolver_source"], resolvers)); err != nil {
				return err
			}
			if device, ok := evidence["configured_tun_device"].(string); ok {
				if err := line(w, fmt.Sprintf("Configured TUN device: %s | present: %v | default route matches: %v", device, evidence["configured_tun_present"], evidence["default_route_matches_tun"])); err != nil {
					return err
				}
			}
			if issues, ok := evidence["issues"].([]string); ok {
				for _, issue := range issues {
					if err := line(w, "Observation: "+issue); err != nil {
						return err
					}
				}
			}
		}
		if sample, ok := v["packet_sample"].(map[string]any); ok {
			if err := line(w, fmt.Sprintf("Packet sample: %v | capture interfaces: %v | matched interfaces: %v", sample["status"], sample["capture_interfaces"], sample["matched_interfaces"])); err != nil {
				return err
			}
			if issues, ok := sample["issues"].([]string); ok {
				for _, issue := range issues {
					if err := line(w, "Packet sample: "+issue); err != nil {
						return err
					}
				}
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
	if entries, ok := v["subscriptions"].([]subscription.Entry); ok {
		if len(entries) == 0 {
			return line(w, "No subscriptions configured. Add one with `nagi subscription add NAME URL`.")
		}
		for _, entry := range entries {
			label := entry.Name
			if !entry.UpdatedAt.IsZero() {
				label += "  Updated: " + entry.UpdatedAt.Format("2006-01-02 15:04:05 MST")
			} else {
				label += "  Never updated"
			}
			if !entry.ExpiresAt.IsZero() {
				label += "  Expires: " + entry.ExpiresAt.Format("2006-01-02 15:04:05 MST")
			}
			if entry.Total > 0 {
				label += fmt.Sprintf("  Traffic: %d/%d bytes", entry.Upload+entry.Download, entry.Total)
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
		if len(entries) == 0 {
			return line(w, "No custom rules. Add one with `nagi rules add NAME TYPE PAYLOAD TARGET`.")
		}
		for _, entry := range entries {
			state := "disabled"
			if entry.Enabled {
				state = "enabled"
			}
			if err := line(w, entry.Name+" ("+state+"): "+rules.Line(entry)); err != nil {
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
		return line(w, fmt.Sprintf("Rule %s: %v. Effective configuration was validated; running mihomo was reloaded if active.", name, v["action"]))
	}
	if running, ok := v["running"].(bool); ok {
		state := "stopped"
		if running {
			state = "running"
		}
		if err := line(w, "Mihomo: "+state); err != nil {
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
		if err := line(w, kind+": "+label); err != nil {
			return err
		}
		if status, ok := v["adapter_status"].(string); ok {
			name, _ := v["adapter_name"].(string)
			if name != "" {
				return line(w, "Adapter "+name+": "+status)
			}
			return line(w, "Adapter: "+status)
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
				return line(w, "Removed profile "+name+". Any existing backup was retained.")
			}
			return line(w, "Removed subscription "+name+" and its cache.")
		case v["applied"] == true:
			return line(w, fmt.Sprintf("Applied subscription %s as selected profile %v. If mihomo was running, its configuration was reloaded.", name, v["profile"]))
		case len(v) == 1:
			return line(w, fmt.Sprintf("Saved subscription %s. Run `nagi subscription update %s` to download it.", name, name))
		}
	}
	if current, ok := v["current"].(string); ok && len(v) == 1 {
		return line(w, "Selected profile "+current+". If mihomo was running, its configuration was reloaded.")
	}
	if group, ok := v["group"].(string); ok {
		if node, ok := v["node"].(string); ok {
			return line(w, fmt.Sprintf("Selected %s in proxy group %s.", node, group))
		}
	}
	if mode, ok := v["mode"].(string); ok {
		if v["persistent"] == true {
			if v["changed"] == true {
				return line(w, "Saved "+mode+" mode in the selected profile. If mihomo was running, its configuration was reloaded.")
			}
			return line(w, "Saved profile mode: "+mode)
		}
		if v["changed"] == true {
			return line(w, "Set temporary mihomo mode to "+mode+".")
		}
		return line(w, "Mihomo mode: "+mode)
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
	return line(w, fmt.Sprintf("%s: %v", label, value))
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
	_, _ = fmt.Fprintf(w, "Error (%s): %s\n", code, message)
	if code == "usage" {
		_, _ = io.WriteString(w, "Run `nagi --help` for usage.\n")
	}
}
