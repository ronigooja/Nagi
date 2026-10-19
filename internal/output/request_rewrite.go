package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Raw is explicitly requested content, preserved byte for byte.
type Raw []byte

// RewriteResult retains the JSON contract while formatting terminal summaries.
type RewriteResult map[string]any

func writeRewrite(w io.Writer, v RewriteResult) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return e
	}
	if script, ok := v["script"].(map[string]any); ok && v["sensitive"] == true {
		if source, ok := script["source"].(string); ok {
			_, err := io.WriteString(w, source)
			return err
		}
	}
	if v["sensitive"] == true {
		if err := line(w, text(w, "cli.output.writerewrite.sensitive_output_may_contain_request_or_rule_credentials", "Sensitive output: may contain request or rule credentials.")); err != nil {
			return err
		}
	}
	if v["request"] != nil || v["response"] != nil || v["message"] != nil || v["stages"] != nil {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		return line(w, string(b))
	}
	if actual, ok := v["actual"]; ok {
		if err := line(w, renderText(w, "cli.output.writerewrite.request_rewrite_v", map[string]any{"state": stateText(w, fmt.Sprint(actual))}, fmt.Sprintf("Request rewrite: %v", actual))); err != nil {
			return err
		}
	}
	for _, p := range []struct{ key, label string }{{"profile", text(w, "cli.output.writerewrite.profile", "Profile")}, {"enabled_intent", text(w, "cli.output.writerewrite.enabled_intent", "Enabled intent")}, {"tun_running", text(w, "cli.output.writerewrite.tun_running", "TUN running")}, {"backend", text(w, "cli.output.writerewrite.backend", "Backend")}, {"service_running", text(w, "cli.output.writerewrite.rewrite_service_running", "Rewrite service running")}, {"configured_rule_version", text(w, "cli.output.writerewrite.configured_rule_version", "Configured rule version")}, {"active_rule_version", text(w, "cli.output.writerewrite.active_rule_version", "Active rule version")}, {"generation", text(w, "cli.output.writerewrite.generation", "Generation")}, {"engine_epoch", text(w, "cli.output.writerewrite.engine_epoch", "Engine epoch")}, {"last_error", text(w, "cli.output.writerewrite.last_error", "Last error")}, {"recovery", text(w, "cli.output.writerewrite.recovery", "Recovery")}, {"saved", text(w, "cli.output.writerewrite.saved", "Saved")}, {"valid", text(w, "cli.output.writerewrite.valid", "Valid")}, {"exported", text(w, "cli.output.writerewrite.exported", "Exported")}, {"path", text(w, "cli.output.writerewrite.file", "File")}, {"state", text(w, "cli.output.writerewrite.ca", "CA")}, {"trust", text(w, "cli.output.writerewrite.os_trust", "OS trust")}, {"fingerprint", text(w, "cli.output.writerewrite.fingerprint", "Fingerprint")}} {
		if value, ok := v[p.key]; ok {
			if value == nil || value == "" {
				value = stateText(w, "unknown")
			}
			switch p.key {
			case "enabled_intent", "tun_running", "service_running", "saved", "valid", "exported", "backend", "trust", "state":
				value = stateText(w, fmt.Sprint(value))
			}
			if err := line(w, fmt.Sprintf("%s: %v", p.label, value)); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"not_before", "not_after"} {
		if value, ok := v[key].(string); ok {
			t, e := time.Parse(time.RFC3339Nano, value)
			if e == nil && !t.IsZero() {
				if err := line(w, text(w, "cli.display."+key, strings.ReplaceAll(key, "_", " "))+": "+t.Local().Format("2006-01-02 15:04")); err != nil {
					return err
				}
			}
		}
	}
	if ca, ok := v["ca"].(map[string]any); ok {
		if err := writeRewrite(w, RewriteResult(ca)); err != nil {
			return err
		}
	}
	if scripts, ok := v["scripts"].([]any); ok {
		if len(scripts) == 0 {
			return line(w, text(w, "cli.output.writerewrite.no_local_rewrite_scripts", "No local rewrite scripts."))
		}
		for _, script := range scripts {
			if err := writeRewriteScript(w, script); err != nil {
				return err
			}
		}
	}
	if script, ok := v["script"]; ok {
		return writeRewriteScript(w, script)
	}
	if rules, ok := v["rules"].([]any); ok {
		if len(rules) == 0 {
			return line(w, text(w, "cli.output.writerewrite.no_request_rewrite_rules", "No request rewrite rules."))
		}
		for i, r := range rules {
			if err := writeRewriteRule(w, i+1, r); err != nil {
				return err
			}
		}
	}
	if rule, ok := v["rule"]; ok {
		return writeRewriteRule(w, 0, rule)
	}
	return nil
}
func writeRewriteRule(w io.Writer, index int, v any) error {
	m, _ := v.(map[string]any)
	prefix := ""
	if index > 0 {
		prefix = fmt.Sprintf("%d. ", index)
	}
	return line(w, renderText(w, "cli.output.writerewriterule.s_v_enabled_v_stage_v_range_v_match_v_operations_v_on_error_v", map[string]any{"prefix": fmt.Sprint(prefix), "id": fmt.Sprint(m["id"]), "enabled": stateText(w, fmt.Sprint(m["enabled"])), "stage": stateText(w, fmt.Sprint(m["stage"])), "range": stateText(w, fmt.Sprint(m["range_mode"])), "match": fmt.Sprint(m["match"]), "operations": fmt.Sprint(m["operations"]), "on_error": stateText(w, fmt.Sprint(m["on_error"]))}, fmt.Sprintf("%s%v  enabled=%v  stage=%v  range=%v  match=%v  operations=%v  on_error=%v", prefix, m["id"], m["enabled"], m["stage"], m["range_mode"], m["match"], m["operations"], m["on_error"])))
}

func writeRewriteScript(w io.Writer, v any) error {
	m, _ := v.(map[string]any)
	return line(w, renderText(w, "cli.output.writerewritescript.v_body_access_v_source_bytes_v_rule_ids_v", map[string]any{"id": fmt.Sprint(m["id"]), "body_access": fmt.Sprint(m["body_access"]), "source_bytes": fmt.Sprint(m["source_bytes"]), "rule_ids": fmt.Sprint(m["rule_ids"])}, fmt.Sprintf("%v  body_access=%v  source_bytes=%v  rule_ids=%v", m["id"], m["body_access"], m["source_bytes"], m["rule_ids"])))
}
