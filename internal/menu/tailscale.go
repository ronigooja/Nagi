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
	groups, err := selectNodeGroups(ctx, s, nil)
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
