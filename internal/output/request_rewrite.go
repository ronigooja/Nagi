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
		if err := line(w, "Sensitive output: may contain request or rule credentials."); err != nil {
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
		if err := line(w, fmt.Sprintf("Request rewrite: %v", actual)); err != nil {
			return err
		}
	}
	for _, p := range []struct{ key, label string }{{"profile", "Profile"}, {"enabled_intent", "Enabled intent"}, {"tun_running", "TUN running"}, {"backend", "Backend"}, {"service_running", "Rewrite service running"}, {"configured_rule_version", "Configured rule version"}, {"active_rule_version", "Active rule version"}, {"generation", "Generation"}, {"engine_epoch", "Engine epoch"}, {"last_error", "Last error"}, {"recovery", "Recovery"}, {"saved", "Saved"}, {"valid", "Valid"}, {"exported", "Exported"}, {"path", "File"}, {"state", "CA"}, {"trust", "OS trust"}, {"fingerprint", "Fingerprint"}} {
		if value, ok := v[p.key]; ok {
			if value == nil || value == "" {
				value = "unknown"
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
				if err := line(w, strings.ReplaceAll(key, "_", " ")+": "+t.Local().Format("2006-01-02 15:04")); err != nil {
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
			return line(w, "No local rewrite scripts.")
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
			return line(w, "No request rewrite rules.")
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
	return line(w, fmt.Sprintf("%s%v  enabled=%v  stage=%v  range=%v  match=%v  operations=%v  on_error=%v", prefix, m["id"], m["enabled"], m["stage"], m["range_mode"], m["match"], m["operations"], m["on_error"]))
}

func writeRewriteScript(w io.Writer, v any) error {
	m, _ := v.(map[string]any)
	return line(w, fmt.Sprintf("%v  body_access=%v  source_bytes=%v  rule_ids=%v", m["id"], m["body_access"], m["source_bytes"], m["rule_ids"]))
}
