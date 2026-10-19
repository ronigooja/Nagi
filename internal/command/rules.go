package command

import (
	"context"
	"errors"

	"net/url"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/rules"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func rulesCommand(ctx context.Context, args []string, configDir, binary string, client *control.Client, proxies *proxy.Service) (any, error) {
	switch args[1] {
	case "test":
		req, err := ruleTestArguments(args)
		if err != nil {
			return nil, usage(err.Error())
		}
		var result rules.TestResult
		if err := client.Post(ctx, "/rules/test", req, &result); err != nil {
			var apiErr *control.APIError
			if errors.As(err, &apiErr) {
				switch apiErr.Status {
				case 404, 405:
					return nil, fail("rule_error", messageError("cli.error.rules.running_mihomo_does_not_support_rule_testing_install_the_matching_en", "running mihomo does not support rule testing; install the matching engine and restart Nagi"))
				case 422:
					// Preserve mihomo's actionable destination-test failure
					// instead of relabeling it as engine unavailability.
					return nil, fail("rule_error", errors.New(apiErr.Message))
				}
			}
			return nil, fail("rule_error", messageError("cli.error.rules.rule_test_failed_ensure_mihomo_is_running_with_the_matching_engine_w", "rule test failed; ensure mihomo is running with the matching engine: %w", err))
		}
		return result, nil
	case "list":
		var result struct {
			Rules []map[string]any `json:"rules"`
		}
		if err := client.Get(ctx, "/rules", &result); err != nil {
			return nil, err
		}
		if result.Rules == nil {
			result.Rules = []map[string]any{}
		}
		return map[string]any{"rules": result.Rules}, nil
	case "providers":
		var result struct {
			Providers map[string]any `json:"providers"`
		}
		if err := client.Get(ctx, "/providers/rules", &result); err != nil {
			return nil, err
		}
		if result.Providers == nil {
			result.Providers = map[string]any{}
		}
		return map[string]any{"providers": result.Providers}, nil
	case "connection":
		connections, err := proxies.Connections(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range connections {
			if c.ID == args[2] {
				return map[string]any{"id": c.ID, "rule": c.Rule, "rule_payload": c.RulePayload, "chains": c.Chains}, nil
			}
		}
		return nil, fail("not_found", messageError("cli.error.rules.connection_s_is_no_longer_active_run_nagi_connections_list", "connection %s is no longer active; run `nagi connections list`", args[2]))
	case "custom":
		name, err := profile.NewStore(configDir, nil, nil).Current()
		if err != nil {
			return nil, fail("rule_error", messageError("cli.error.rules.select_a_valid_profile_first_w", "select a valid profile first: %w", err))
		}
		doc, err := rules.New(configDir, name, nil).List()
		if err != nil {
			return nil, fail("rule_error", err)
		}
		return map[string]any{"profile": name, "entries": doc.Entries}, nil
	case "conflicts":
		name, err := profile.NewStore(configDir, nil, nil).Current()
		if err != nil {
			return nil, fail("rule_error", err)
		}
		store := profile.NewStore(configDir, nil, nil)
		data, err := store.Effective(name)
		if err != nil {
			return nil, fail("rule_error", err)
		}
		report, err := rules.AnalyzeEffective(data)
		if err != nil {
			return nil, fail("rule_error", err)
		}
		return report, nil
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, err
	}
	name, err := profile.NewStore(configDir, nil, nil).Current()
	if err != nil {
		return nil, fail("rule_error", messageError("cli.error.rules.select_a_valid_profile_first_w_142", "select a valid profile first: %w", err))
	}
	path := filepath.Join(configDir, "profiles", name+".yaml")
	if err := ensureDefaultProfile(path, name); err != nil {
		return nil, fail("rule_error", err)
	}
	manager, err := engine.New(engine.Options{Binary: binary, ConfigPath: path, Paths: paths})
	if err != nil {
		return nil, err
	}
	var lifecycle lifecycleBackend = manager
	mode, err := readBackend(paths)
	if err != nil {
		return nil, err
	}
	if mode == privilegedBackend {
		lifecycle = helperBackend{profile: name, binary: binary, configPath: path, paths: paths}
	}
	var profileStore *profile.Store
	profileStore = newProfileStore(paths, binary, client, lifecycle)
	check := func(ctx context.Context) error {
		effective, err := profileStore.Effective(name)
		if err != nil {
			return err
		}
		if err := profileStore.Validate(effective); err != nil {
			return err
		}
		status, err := lifecycle.Status(ctx)
		if err != nil {
			return err
		}
		if !status.Running {
			return nil
		}
		path, err := materializeProfile(profileStore, configDir, name)
		if err != nil {
			return err
		}
		if err := activateConfig(ctx, client, path); err != nil {
			return messageError("cli.error.rules.reload_active_rules_w_saved_rules_were_rolled_back", "reload active rules: %w; saved rules were rolled back", err)
		}
		return nil
	}
	store := rules.New(configDir, name, check)
	switch args[1] {
	case "add":
		typ := strings.ToUpper(args[3])
		payload := args[4]
		if typ == "MATCH" && payload == "-" {
			payload = ""
		}
		err = store.Add(ctx, rules.Entry{Name: args[2], Enabled: true, Kind: "rule", Type: typ, Payload: payload, Target: args[5]})
	case "remove":
		err = store.Remove(ctx, args[2])
	case "enable":
		err = store.SetEnabled(ctx, args[2], true)
	case "disable":
		err = store.SetEnabled(ctx, args[2], false)
	case "import":
		err = store.Import(ctx, args[2], args[3])
	default:
		return nil, messageError("cli.error.rules.unsupported_rules_command", "unsupported rules command")
	}
	if err != nil {
		return nil, fail("rule_error", err)
	}
	return map[string]any{"profile": name, "name": args[2], "action": args[1], "saved": true}, nil
}

// validateRuleImportArguments checks syntax before resolving runtime paths.
func validateRuleImportArguments(args []string) error {
	name, source := args[2], args[3]
	if len(name) < 1 || len(name) > 128 || strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		return messageError("cli.error.rules.name_must_use_1_128_ascii_letters_digits_or", "NAME must use 1..128 ASCII letters, digits, _ or -")
	}
	if strings.TrimSpace(source) == "" {
		return messageError("cli.error.rules.source_is_required", "SOURCE is required")
	}
	// Relative and absolute filesystem paths can contain characters url.Parse
	// rejects. Treat explicit URL schemes and authority-only URLs as URLs.
	u, err := url.Parse(source)
	if strings.Contains(source, "://") || strings.HasPrefix(source, "//") || (err == nil && u.Scheme != "") {
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
			return messageError("cli.error.rules.source_url_must_be_absolute_http_s_without_userinfo", "SOURCE URL must be absolute HTTP(S) without userinfo")
		}
	}
	return nil
}
