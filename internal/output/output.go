package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/service"
	"github.com/ronigooja/Nagi/internal/subscription"
)

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
	case engine.Status:
		return writeLifecycle(w, v)
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
	case proxy.Group:
		return writeGroup(w, v)
	case proxy.DelayResult:
		return line(w, fmt.Sprintf("Proxy %s delay: %d ms (URL: %s, timeout: %d ms)", v.Proxy, v.DelayMS, v.URL, v.TimeoutMS))
	case subscription.RefreshResult:
		return line(w, fmt.Sprintf("Downloaded subscription %s (%d bytes). Run `nagi subscription apply %s` to select its profile.", v.Name, v.Bytes, v.Name))
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
			if err := line(w, fmt.Sprintf("%s  ID: %s  Upload: %d B  Download: %d B", destination, connection.ID, connection.Upload, connection.Download)); err != nil {
				return err
			}
		}
		return nil
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
	if valid, ok := v["valid"].(bool); ok && valid {
		return line(w, fmt.Sprintf("Profile %v is valid.", v["profile"]))
	}
	if name, ok := v["name"].(string); ok {
		switch {
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
		if v["changed"] == true {
			return line(w, "Set mihomo mode to "+mode+".")
		}
		return line(w, "Mihomo mode: "+mode)
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
	if jsonMode {
		_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: &Failure{Code: code, Message: message}})
		return
	}
	_, _ = fmt.Fprintf(w, "Error (%s): %s\n", code, message)
	if code == "usage" {
		_, _ = io.WriteString(w, "Run `nagi --help` for usage.\n")
	}
}
