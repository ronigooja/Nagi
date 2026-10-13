package command

import (
	"context"
	"errors"
	"fmt"
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
			if errors.As(err, &apiErr) && (apiErr.Status == 404 || apiErr.Status == 405) {
				return nil, fail("rule_error", errors.New("running mihomo does not support rule testing; install the matching engine and restart Nagi"))
			}
			return nil, fail("rule_error", fmt.Errorf("rule test failed; ensure mihomo is running with the matching engine: %w", err))
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
		return nil, fail("not_found", fmt.Errorf("connection %s is no longer active; run `nagi connections list`", args[2]))
	case "custom":
		doc, err := rules.New(configDir, nil).List()
		if err != nil {
			return nil, fail("rule_error", err)
		}
		return map[string]any{"entries": doc.Entries}, nil
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
		return nil, fail("rule_error", fmt.Errorf("select a valid profile first: %w", err))
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
			return fmt.Errorf("reload active rules: %w; saved rules were rolled back", err)
		}
		return nil
	}
	store := rules.New(configDir, check)
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
		target := ""
		if len(args) == 6 {
			target = args[5]
		}
		err = store.Import(ctx, args[2], args[3], args[4], target)
	default:
		return nil, errors.New("unsupported rules command")
	}
	if err != nil {
		return nil, fail("rule_error", err)
	}
	return map[string]any{"name": args[2], "action": args[1], "saved": true}, nil
}

// validateRuleImportArguments checks syntax before resolving runtime paths.
func validateRuleImportArguments(args []string) error {
	name, format, source := args[2], args[3], args[4]
	if len(name) < 1 || len(name) > 128 || strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		return errors.New("NAME must use 1..128 ASCII letters, digits, _ or -")
	}
	switch format {
	case "rules":
		if len(args) != 5 {
			return errors.New("FORMAT rules contains its own targets and does not accept TARGET")
		}
	case "classical", "domain", "ipcidr":
		if len(args) != 6 || strings.TrimSpace(args[5]) == "" {
			return errors.New("FORMAT classical, domain, and ipcidr require TARGET")
		}
		if strings.ContainsAny(args[5], ",\n\r") {
			return errors.New("TARGET must be DIRECT, REJECT, or a proxy group name without commas or newlines")
		}
	default:
		return errors.New("FORMAT must be rules, classical, domain, or ipcidr")
	}
	if strings.TrimSpace(source) == "" {
		return errors.New("SOURCE is required")
	}
	// Relative and absolute filesystem paths can contain characters url.Parse
	// rejects. Treat explicit URL schemes and authority-only URLs as URLs.
	u, err := url.Parse(source)
	if strings.Contains(source, "://") || strings.HasPrefix(source, "//") || (err == nil && u.Scheme != "") {
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
			return errors.New("SOURCE URL must be absolute HTTP(S) without userinfo")
		}
	}
	return nil
}
