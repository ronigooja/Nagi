package menu

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// Settings summaries describe the command's typed state; detail actions retain
// the complete result. Missing or malformed values must never imply Off.
func localizedSettingsSummary(language i18n.Context, result Result, command []string, label string, fields ...string) string {
	if result.Error != nil || result.Code != 0 || len(command) == 0 {
		return localizedpageStatusResult(language, result, label, fields...)
	}
	object := localizedmenuObject(language, result.Data)
	unknown := menuState(language, "unknown")
	text := func(id, fallback string) string { return menuText(language, "menu.summary."+id, fallback) }
	line := func(label, value string) string { return label + ": " + value }
	setting := func(raw json.RawMessage) string {
		flag, known := summaryBool(raw)
		if !known {
			return unknown
		}
		if flag {
			return menuState(language, "on")
		}
		return menuState(language, "off")
	}
	switch command[0] {
	case "tun", "system-proxy":
		return line(label, setting(object["enabled"]))
	case "lan":
		value := setting(object["enabled"])
		if enabled, known := summaryBool(object["enabled"]); known && enabled {
			var address string
			if json.Unmarshal(object["bind_address"], &address) != nil || address == "" {
				address = unknown
			}
			value += " — " + line(text("bind_address", "Bind Address"), address)
		}
		return line(label, value)
	case "ports":
		var values []string
		for _, port := range []struct{ key, label string }{{"http", "HTTP"}, {"socks", "SOCKS"}, {"mixed", text("mixed", "Mixed")}} {
			var number *int
			if json.Unmarshal(object[port.key], &number) != nil || number == nil || *number < 0 || *number > 65535 {
				values = append(values, port.label+" "+unknown)
			} else if *number > 0 {
				values = append(values, port.label+" "+strconv.Itoa(*number))
			}
		}
		if len(values) == 0 {
			values = append(values, text("none", "None"))
		}
		return line(label, strings.Join(values, ", "))
	case "dns":
		var lines []string
		for _, field := range fields {
			switch field {
			case "enabled":
				lines = append(lines, line("DNS", setting(object[field])))
			case "tun_enabled":
				// The CLI field reports TUN enablement, not verified DNS capture.
				lines = append(lines, line("TUN", setting(object[field])))
			case "policy":
				var policy string
				_ = json.Unmarshal(object[field], &policy)
				value := unknown
				switch policy {
				case "direct":
					value = text("direct", "Direct")
				case "proxy":
					value = text("proxy", "Proxy")
				case "rules":
					value = text("rules", "Follow Rules")
				case "mixed":
					value = text("mixed", "Mixed")
				}
				lines = append(lines, line(text("dns_policy", "Upstream Routing"), value))
			}
		}
		return strings.Join(lines, "\n")
	case "privileged-helper":
		var service map[string]json.RawMessage
		_ = json.Unmarshal(object["service"], &service)
		installation := unknown
		if installed, known := summaryBool(service["installed"]); known {
			installation = menuState(language, "not-installed")
			if installed {
				installation = menuState(language, "installed")
			}
		}
		activity := unknown
		if active, known := summaryBool(service["active"]); known {
			activity = menuState(language, "inactive")
			if active {
				activity = menuState(language, "active")
			}
		}
		var backend string
		_ = json.Unmarshal(object["selected_backend"], &backend)
		backendText := unknown
		switch backend {
		case "user":
			backendText = text("user_backend", "User")
		case "privileged":
			backendText = text("privileged_backend", "Privileged")
		}
		return strings.Join([]string{line(label, installation), line(text("service", "Service"), activity), line(text("engine_backend", "Engine Backend"), backendText)}, "\n")
	}
	return localizedpageStatusResult(language, result, label, fields...)
}

func summaryBool(raw json.RawMessage) (bool, bool) {
	var flag *bool
	if json.Unmarshal(raw, &flag) != nil || flag == nil {
		return false, false
	}
	return *flag, true
}
