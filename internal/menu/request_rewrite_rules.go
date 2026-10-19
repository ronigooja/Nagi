package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func rewriteRulesPage(profile string) *Page {
	return &Page{Title: "Ordered Rules", Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Context: "Profile: " + profile + "\nRules match original domain, method and escaped path in stored order. Each enabled rule domain grants interception scope. Values are hidden until explicit Edit.", Actions: []Item{{Label: "Add rule", Impact: rewriteEffect, Next: func() *Page {
			return rewriteRuleDraft(profile, rewrite.Rule{Enabled: true, Stage: rewrite.StageRequest, RangeMode: "fragment", Match: rewrite.Match{Methods: []string{"GET"}, PathPrefix: "/"}, OnError: "block", RedirectConsent: []rewrite.Consent{}, Operations: []rewrite.Operation{}}, false)
		}}}}
		r := s.Fetch(ctx, rewriteArgs(profile, "rules", "list")...)
		if rewriteFailed(r) {
			return v, rewriteReadError(r)
		}
		var data struct {
			Profile string                `json:"profile"`
			Rules   []rewrite.RuleSummary `json:"rules"`
		}
		if decodeMenuResult(r, &data) != nil || data.Profile != profile {
			return v, fmt.Errorf("rule profile mismatch or invalid response")
		}
		for i, rule := range data.Rules {
			rule := rule
			v.Items = append(v.Items, Item{Label: fmt.Sprintf("%d. %s — enabled %t | %s | %s | %s", i+1, rule.ID+" ["+rule.Stage+"]", rule.Enabled, rule.Match.Domain, strings.Join(rule.Match.Methods, ","), rule.Match.PathPrefix), Next: func() *Page { return rewriteRuleDetail(profile, rule.ID) }})
		}
		v.Empty = "No rules."
		return v, nil
	}}
}
func rewriteRuleDetail(profile, id string) *Page {
	return &Page{Title: "Rule — " + id, Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Context: "Profile: " + profile}
		r := s.Fetch(ctx, rewriteArgs(profile, "rules", "show", id)...)
		if rewriteFailed(r) {
			return v, rewriteReadError(r)
		}
		var data struct {
			Profile string              `json:"profile"`
			Rule    rewrite.RuleSummary `json:"rule"`
		}
		if decodeMenuResult(r, &data) != nil || data.Profile != profile || data.Rule.ID != id {
			return v, fmt.Errorf("rule profile or ID mismatch")
		}
		rule := data.Rule
		toggle := "enable"
		if rule.Enabled {
			toggle = "disable"
		}
		v.Context += fmt.Sprintf(" | Rule: %s | Enabled: %t\nMatch: %s | %s | %s\nOn error: %s | Operations: %s", id+" ["+rule.Stage+"]", rule.Enabled, rule.Match.Domain, strings.Join(rule.Match.Methods, ","), rule.Match.PathPrefix, rule.OnError, strings.Join(rule.Operations, ", "))
		v.Actions = []Item{
			{Label: "Edit rule", Impact: rewriteSensitive + " Explicitly exports complete values into memory.", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				raw := rewriteRaw(ctx, s, profile, "export", "-")
				if rewriteFailed(raw) {
					s.ShowResult(nil, raw)
					return Navigation{}, nil
				}
				text, ok := raw.Data.(string)
				if !ok {
					s.feedback(feedbackError, "Invalid export response.")
					return Navigation{}, nil
				}
				snapshot, err := rewrite.Parse([]byte(text))
				if err != nil || snapshot.Document().Enabled {
					s.feedback(feedbackError, "Invalid portable rule document.")
					return Navigation{}, nil
				}
				for _, full := range snapshot.Document().Rules {
					if full.ID == id {
						return Navigation{Next: rewriteRuleDraft(profile, full, true)}, nil
					}
				}
				s.feedback(feedbackWarning, "Rule no longer exists. Return to Ordered Rules.")
				return Navigation{}, nil
			}},
			{Label: strings.Title(toggle) + " rule", Impact: rewriteEffect, Action: func(ctx context.Context, s *Session) error {
				rewriteSubmit(ctx, s, profile, nil, "rules", toggle, id)
				return nil
			}},
			{Label: "Move rule to position", Impact: rewriteEffect, Action: func(ctx context.Context, s *Session) error { return rewriteRuleReorder(ctx, s, profile, id) }},
			{Label: "Remove rule", Impact: rewriteEffect, Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := rewriteSubmit(ctx, s, profile, nil, "rules", "remove", id)
				return Navigation{Back: !rewriteFailed(r)}, nil
			}},
		}
		return v, nil
	}}
}
func rewriteRuleReorder(ctx context.Context, s *Session, profile, id string) error {
	r := s.Fetch(ctx, rewriteArgs(profile, "rules", "list")...)
	if rewriteFailed(r) {
		s.ShowResult(nil, r)
		return nil
	}
	var data struct {
		Profile string                `json:"profile"`
		Rules   []rewrite.RuleSummary `json:"rules"`
	}
	if decodeMenuResult(r, &data) != nil || data.Profile != profile {
		return fmt.Errorf("rule profile mismatch or invalid response")
	}
	ids := []string{}
	index := -1
	for i, rule := range data.Rules {
		ids = append(ids, rule.ID)
		if rule.ID == id {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("rule no longer exists")
	}
	_, err := s.rewriteForm(ctx, []Field{{Key: "position", Label: fmt.Sprintf("Position (1–%d)", len(ids)), Required: true, Validate: func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > len(ids) {
			return fmt.Errorf("choose an existing position")
		}
		return nil
	}}}, nil, func(v map[string]string) Result {
		position, _ := strconv.Atoi(v["position"])
		ordered := append([]string{}, ids...)
		ordered = append(ordered[:index], ordered[index+1:]...)
		ordered = append(ordered, "")
		copy(ordered[position:], ordered[position-1:])
		ordered[position-1] = id
		return rewriteSubmit(ctx, s, profile, ordered, "rules", "reorder", "-")
	})
	return err
}

// A draft is presentation state only. Nested pages change this in-memory copy;
// only Save submits it to the CLI. Returning or EOF discards an unsaved draft.
func rewriteRuleDraft(profile string, initial rewrite.Rule, existing bool) *Page {
	rule := initial
	return &Page{Title: "Rule draft", Load: func(_ context.Context, _ *Session) (PageView, error) {
		summary := "Profile: " + profile + " | Rule: " + rule.ID + " | Enabled: " + strconv.FormatBool(rule.Enabled) + "\nMatch: " + rule.Match.Domain + " | " + strings.Join(rule.Match.Methods, ",") + " | " + rule.Match.PathPrefix + "\nOn error: " + rule.OnError + fmt.Sprintf(" | Operations: %d | Destination consents: %d", len(rule.Operations), len(rule.RedirectConsent)) + "\nChanges remain in memory until Save; Back discards unsaved changes."
		fields := []Field{{Key: "domain", Label: "Exact or wildcard matching domain", Required: true}, {Key: "methods", Label: "HTTP methods (comma separated uppercase tokens)", Required: true}, {Key: "path", Label: "Escaped path prefix", Required: true}, {Key: "enabled", Label: "Rule enabled", Choices: []string{"true", "false"}}, {Key: "error", Label: "On transformation error", Choices: []string{"block", "forward_original"}}}
		if !existing {
			fields = append([]Field{{Key: "id", Label: "Stable rule ID", Required: true}}, fields...)
		}
		actions := []Item{{Label: "Edit matching and behavior", Action: func(ctx context.Context, s *Session) error {
			values := map[string]string{"id": rule.ID, "domain": rule.Match.Domain, "methods": strings.Join(rule.Match.Methods, ","), "path": rule.Match.PathPrefix, "enabled": strconv.FormatBool(rule.Enabled), "error": rule.OnError}
			_, err := s.Form(ctx, fields, values, func(v map[string]string) Result {
				rule.ID = v["id"]
				rule.Match.Domain = v["domain"]
				rule.Match.Methods = rewriteCSV(v["methods"])
				rule.Match.PathPrefix = v["path"]
				rule.Enabled = v["enabled"] == "true"
				rule.OnError = v["error"]
				return Result{}
			})
			return err
		}},
			{Label: "Ordered operations", Impact: rewriteSensitive, Next: func() *Page { return rewriteOperationsPage(&rule) }},
			{Label: "Destination consent", Impact: "Required for request scheme/host/port changes; target domains also require enabled rule scope.", Next: func() *Page { return rewriteConsentPage(&rule) }},
			{Label: "Save rule", Impact: rewriteEffect, Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				args := []string{"rules", "add", "-"}
				if existing {
					args = []string{"rules", "update", rule.ID, "-"}
				}
				r := rewriteSubmit(ctx, s, profile, rewriteRuleWire(rule), args...)
				if rewriteFailed(r) {
					s.feedback(feedbackWarning, "Draft retained. Edit fields or choose Save again explicitly.")
				}
				return Navigation{Back: !rewriteFailed(r)}, nil
			}}}
		actions = append(actions, Item{Label: "Edit complete rule JSON", Impact: rewriteSensitive + " Includes stage, all match predicates, range mode and every operation.", Action: rewriteRuleJSONForm(&rule, existing)})
		return PageView{Context: summary + "\nStage: " + rule.Stage + " | Range mode: " + rule.RangeMode, Actions: actions}, nil
	}}
}
func rewriteCSV(v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func rewriteOperationsPage(rule *rewrite.Rule) *Page {
	return &Page{Title: "Ordered operations — draft", Load: func(_ context.Context, _ *Session) (PageView, error) {
		v := PageView{Context: "Explicit sensitive editing. Values remain in the draft until Save rule.", Actions: []Item{{Label: "Add operation", Action: func(ctx context.Context, s *Session) error {
			return rewriteOperationForm(ctx, s, nil, func(op rewrite.Operation) { rule.Operations = append(rule.Operations, op) })
		}}}}
		for index, op := range rule.Operations {
			index := index
			v.Items = append(v.Items, Item{Label: fmt.Sprintf("%d. %s", index+1, op.Op), Next: func() *Page { return rewriteOperationDetail(rule, index) }})
		}
		return v, nil
	}}
}
func rewriteOperationDetail(rule *rewrite.Rule, index int) *Page {
	return &Page{Title: "Operation draft", Load: func(_ context.Context, _ *Session) (PageView, error) {
		raw, _ := json.MarshalIndent(rule.Operations[index], "", "  ")
		actions := []Item{
			{Label: "Edit operation", Action: func(ctx context.Context, s *Session) error {
				return rewriteOperationForm(ctx, s, &rule.Operations[index], func(op rewrite.Operation) { rule.Operations[index] = op })
			}},
			{Label: "Move operation", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				err := rewriteDraftPosition(ctx, s, len(rule.Operations), func(to int) {
					item := rule.Operations[index]
					rule.Operations = append(rule.Operations[:index], rule.Operations[index+1:]...)
					rule.Operations = append(rule.Operations, rewrite.Operation{})
					copy(rule.Operations[to+1:], rule.Operations[to:])
					rule.Operations[to] = item
				})
				return Navigation{Back: err == nil}, err
			}},
			{Label: "Remove operation", Navigate: func(_ context.Context, _ *Session) (Navigation, error) {
				rule.Operations = append(rule.Operations[:index], rule.Operations[index+1:]...)
				return Navigation{Back: true}, nil
			}},
		}
		op := rule.Operations[index]
		if op.Op == "json_set" {
			actions = append(actions, Item{Label: "Use root JSON pointer", Impact: "Replaces the complete JSON body when this draft is saved.", Action: func(context.Context, *Session) error { rule.Operations[index].Pointer = ""; return nil }})
		}
		if op.Op == "header_set" || (op.Op == "url_set" && (op.Component == "query" || op.Component == "port")) {
			actions = append(actions, Item{Label: "Set empty value", Impact: "Clears the header value, query, or explicit port in this draft.", Action: func(context.Context, *Session) error {
				rule.Operations[index].Value = json.RawMessage(`""`)
				return nil
			}})
		}
		return PageView{Context: rewriteSensitive + "\n" + string(raw), Actions: actions}, nil
	}}
}
func rewriteDraftPosition(ctx context.Context, s *Session, count int, apply func(int)) error {
	_, err := s.Form(ctx, []Field{{Key: "position", Label: fmt.Sprintf("Position (1–%d)", count), Required: true, Validate: func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > count {
			return fmt.Errorf("choose an existing position")
		}
		return nil
	}}}, nil, func(v map[string]string) Result { n, _ := strconv.Atoi(v["position"]); apply(n - 1); return Result{} })
	return err
}
func rewriteOperationForm(ctx context.Context, s *Session, existing *rewrite.Operation, save func(rewrite.Operation)) error {
	kind := ""
	if existing != nil {
		kind = existing.Op
	} else {
		var err error
		kind, err = s.Select("Operation kind", []string{"header_set", "header_append", "header_delete", "url_set", "method_set", "status_set", "json_set", "json_delete", "body_set", "text_replace", "regex_replace", "trailer_set", "trailer_delete", "script"})
		if err != nil {
			return err
		}
	}
	if kind != "header_set" && kind != "header_delete" && kind != "url_set" && kind != "json_set" && kind != "json_delete" {
		op := rewrite.Operation{Op: kind}
		if existing != nil {
			op = *existing
		}
		raw, _ := json.Marshal(op)
		_, err := s.Form(ctx, []Field{{Key: "json", Label: "Complete operation JSON (single line)", Required: true, Validate: func(v string) error {
			var candidate rewrite.Operation
			if e := rewriteMenuJSON(v, &candidate); e != nil {
				return e
			}
			if candidate.Op != kind {
				return fmt.Errorf("operation kind must remain %s", kind)
			}
			return nil
		}}}, map[string]string{"json": string(raw)}, func(v map[string]string) Result {
			var candidate rewrite.Operation
			_ = rewriteMenuJSON(v["json"], &candidate)
			save(candidate)
			return Result{}
		})
		return err
	}
	values := map[string]string{}
	// Preserve every current value; dedicated draft actions clear optional values.
	if existing != nil {
		values["name"] = existing.Name
		values["component"] = existing.Component
		values["pointer"] = existing.Pointer
		if kind == "json_set" {
			values["value"] = string(existing.Value)
		} else {
			var value string
			_ = json.Unmarshal(existing.Value, &value)
			values["value"] = value
		}
	}

	fields := []Field{}
	switch kind {
	case "header_set", "header_delete":
		fields = append(fields, Field{Key: "name", Label: "Header name", Required: true})
	case "url_set":
		fields = append(fields, Field{Key: "component", Label: "URL component", Choices: []string{"scheme", "host", "port", "path", "query"}})
	case "json_set", "json_delete":
		fields = append(fields, Field{Key: "pointer", Label: "JSON Pointer (new set may use blank for root; edit preserves blank, use root action to clear)"})
	}
	if kind == "header_set" || kind == "url_set" {
		fields = append(fields, Field{Key: "value", Label: "Literal value (visible paste)"})
	}
	if kind == "json_set" {
		fields = append(fields, Field{Key: "value", Label: "JSON value (including quotes for a string)", Required: true, Validate: func(v string) error {
			if !json.Valid([]byte(v)) {
				return fmt.Errorf("enter a JSON value")
			}
			return nil
		}})
	}
	_, err := s.Form(ctx, fields, values, func(v map[string]string) Result {
		op := rewrite.Operation{Op: kind, Name: v["name"], Component: v["component"], Pointer: v["pointer"]}
		if kind == "json_set" {
			op.Value = json.RawMessage(v["value"])
		} else if kind == "header_set" || kind == "url_set" {
			op.Value, _ = json.Marshal(v["value"])
		}
		if existing != nil {
			op.MessageType = existing.MessageType
		}
		save(op)
		return Result{}
	})
	return err
}

func rewriteRuleWire(rule rewrite.Rule) map[string]any {
	return map[string]any{"id": rule.ID, "enabled": rule.Enabled, "stage": rule.Stage, "match": rule.Match, "on_error": rule.OnError, "range_mode": rule.RangeMode, "redirect_consent": rule.RedirectConsent, "operations": rule.Operations}
}
func rewriteRuleJSONForm(rule *rewrite.Rule, existing bool) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		raw, _ := json.Marshal(rewriteRuleWire(*rule))
		values := map[string]string{"json": string(raw)}
		_, err := s.Form(ctx, []Field{{Key: "json", Label: "Complete rule JSON (single line; blank retains draft)", Required: true, Validate: func(v string) error {
			var candidate rewrite.Rule
			if err := rewriteMenuJSON(v, &candidate); err != nil {
				return err
			}
			if existing && candidate.ID != rule.ID {
				return fmt.Errorf("existing rule ID cannot change")
			}
			return nil
		}}}, values, func(v map[string]string) Result {
			var candidate rewrite.Rule
			_ = rewriteMenuJSON(v["json"], &candidate)
			*rule = candidate
			return Result{}
		})
		return err
	}
}
func rewriteMenuJSON(value string, target any) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("enter valid UTF-8 JSON")
	}
	// Drafts must not collapse duplicate fields before CLI validation sees them.
	tokens := json.NewDecoder(strings.NewReader(value))
	tokens.UseNumber()
	var check func(int) error
	check = func(depth int) error {
		if depth > 256 {
			return fmt.Errorf("JSON nesting is too deep")
		}
		token, e := tokens.Token()
		if e != nil {
			return e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for tokens.More() {
				key, e := tokens.Token()
				if e != nil {
					return e
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate JSON field")
				}
				seen[name] = true
				if e := check(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for tokens.More() {
				if e := check(depth + 1); e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("invalid JSON object")
		}
		_, e = tokens.Token()
		return e
	}
	if check(0) != nil {
		return fmt.Errorf("enter valid JSON without duplicate fields")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return fmt.Errorf("enter a complete JSON object with known fields")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("enter exactly one JSON object")
	}
	return nil
}
func rewriteConsentPage(rule *rewrite.Rule) *Page {
	return &Page{Title: "Destination consent — draft", Load: func(_ context.Context, _ *Session) (PageView, error) {
		v := PageView{Context: "Explicit scheme/host/port destination consent. Each destination also requires enabled rule interception scope.", Actions: []Item{{Label: "Add destination consent", Action: func(ctx context.Context, s *Session) error {
			return rewriteConsentForm(ctx, s, nil, func(c rewrite.Consent) { rule.RedirectConsent = append(rule.RedirectConsent, c) })
		}}}}
		for index, c := range rule.RedirectConsent {
			index, c := index, c
			v.Items = append(v.Items, Item{Label: c.Domain + " | " + strings.Join(c.Schemes, ",") + fmt.Sprint(c.Ports), Next: func() *Page {
				return actionPage("Destination consent draft",
					Item{Label: "Edit destination consent", Action: func(ctx context.Context, s *Session) error {
						return rewriteConsentForm(ctx, s, &rule.RedirectConsent[index], func(c rewrite.Consent) { rule.RedirectConsent[index] = c })
					}},
					Item{Label: "Remove destination consent", Navigate: func(_ context.Context, _ *Session) (Navigation, error) {
						rule.RedirectConsent = append(rule.RedirectConsent[:index], rule.RedirectConsent[index+1:]...)
						return Navigation{Back: true}, nil
					}},
				)
			}})
		}
		return v, nil
	}}
}
func rewriteConsentForm(ctx context.Context, s *Session, c *rewrite.Consent, save func(rewrite.Consent)) error {
	values := map[string]string{}
	if c != nil {
		values["domain"] = c.Domain
		values["schemes"] = strings.Join(c.Schemes, ",")
		ports := []string{}
		for _, p := range c.Ports {
			ports = append(ports, strconv.Itoa(p))
		}
		values["ports"] = strings.Join(ports, ",")
	}
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: "Authorized destination domain", Required: true}, {Key: "schemes", Label: "Allowed schemes (http, https; comma separated)", Required: true}, {Key: "ports", Label: "Allowed ports (comma separated)", Required: true, Validate: func(v string) error {
		for _, part := range rewriteCSV(v) {
			p, e := strconv.Atoi(part)
			if e != nil || p < 1 || p > 65535 {
				return fmt.Errorf("ports must be between 1 and 65535")
			}
		}
		return nil
	}}}, values, func(v map[string]string) Result {
		ports := []int{}
		for _, part := range rewriteCSV(v["ports"]) {
			p, _ := strconv.Atoi(part)
			ports = append(ports, p)
		}
		save(rewrite.Consent{Domain: v["domain"], Schemes: rewriteCSV(v["schemes"]), Ports: ports})
		return Result{}
	})
	return err
}
