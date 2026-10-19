package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/profile"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type rewriteInputKey struct{}
type rewriteArguments struct {
	path    string
	values  []string
	profile string
}

func rewriteID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func parseRewriteArguments(args []string) (rewriteArguments, error) {
	o := rewriteArguments{}
	if len(args) < 2 {
		return o, errors.New("missing subcommand")
	}
	n := 2
	if args[1] == "scripts" || args[1] == "rules" || args[1] == "ca" {
		n = 3
	}
	if len(args) >= 3 && args[1] == "ca" && args[2] == "trust" {
		n = 4
	}
	if len(args) < n {
		return o, errors.New("missing subcommand")
	}
	o.path = strings.Join(args[:n], " ")
	spec := findSpec(o.path)
	if spec == nil || spec.maxArgs == -1 {
		return o, errors.New("unknown subcommand")
	}
	for i := n; i < len(args); i++ {
		if args[i] == "--profile" {
			if o.profile != "" || i+1 >= len(args) || !rewriteID(args[i+1]) {
				return o, errors.New("--profile requires one valid profile name")
			}
			o.profile = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--") {
			return o, errors.New("unknown option")
		}
		o.values = append(o.values, args[i])
	}
	allowProfile := args[1] != "status" && args[1] != "enable" && args[1] != "disable" && args[1] != "ca"
	if o.profile != "" && !allowProfile {
		return o, errors.New("--profile is only valid for scripts, rules, import, export, validate and preview")
	}
	min, max := rewriteArity(o.path)
	if len(o.values) < min {
		return o, errors.New("missing argument")
	}
	if len(o.values) > max {
		return o, errors.New("too many arguments")
	}
	action := args[n-1]
	if (args[1] == "scripts" || args[1] == "rules") && (action == "show" || action == "source" || action == "update" || action == "remove" || action == "enable" || action == "disable") && !rewriteID(o.values[0]) {
		return o, errors.New("ID must contain 1..128 ASCII letters, digits, _ or -")
	}
	if input := o.inputFile(); input != "" && input != "-" && (strings.Contains(input, "://") || strings.ContainsAny(input, "\x00\r\n")) {
		return o, errors.New("FILE must be a local private file or - for stdin")
	}
	return o, nil
}
func rewriteArity(path string) (int, int) {
	switch path {
	case "request-rewrite scripts add", "request-rewrite scripts show", "request-rewrite scripts source", "request-rewrite scripts remove", "request-rewrite rules add", "request-rewrite rules show", "request-rewrite rules remove", "request-rewrite rules enable", "request-rewrite rules disable", "request-rewrite rules reorder", "request-rewrite import", "request-rewrite export", "request-rewrite preview", "request-rewrite ca export":
		return 1, 1
	case "request-rewrite scripts update", "request-rewrite rules update":
		return 2, 2
	case "request-rewrite validate":
		return 0, 1
	}
	return 0, 0
}
func (o rewriteArguments) inputFile() string {
	switch o.path {
	case "request-rewrite scripts add", "request-rewrite rules add", "request-rewrite rules reorder", "request-rewrite import", "request-rewrite preview":
		return o.values[0]
	case "request-rewrite scripts update", "request-rewrite rules update":
		return o.values[1]
	case "request-rewrite validate":
		if len(o.values) > 0 {
			return o.values[0]
		}
	}
	return ""
}
func prepareRewriteInput(o rewriteArguments, stdin io.Reader) ([]byte, error) {
	path := o.inputFile()
	if path == "" {
		return nil, nil
	}
	limit := rewrite.MaxDocumentBytes
	if o.path == "request-rewrite preview" {
		limit = rewrite.MaxPreviewBytes
	}
	var reader io.Reader = stdin
	if path != "-" {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, rewriteFailure("rewrite_input_private_file_required")
		}
		f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return nil, rewriteFailure("rewrite_input_unavailable")
		}
		defer f.Close()
		actual, e := f.Stat()
		if e != nil || !os.SameFile(info, actual) || !actual.Mode().IsRegular() || actual.Mode().Perm()&0077 != 0 {
			return nil, rewriteFailure("rewrite_input_private_file_required")
		}
		owner, ok := actual.Sys().(*syscall.Stat_t)
		if !ok || int(owner.Uid) != os.Getuid() {
			return nil, rewriteFailure("rewrite_input_private_file_required")
		}
		reader = f
	}
	data, e := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if e != nil {
		return nil, rewriteFailure("rewrite_input_unavailable")
	}
	if len(data) > limit {
		return nil, rewriteFailure("rewrite_limit")
	}
	return data, nil
}

// Embed entries verbatim in a complete document to reject duplicate and unknown fields.
func parseRewriteScript(data []byte, old *rewrite.Snapshot) (rewrite.Script, error) {
	scripts := old.Document().Scripts
	// Validate the candidate and every referencing rule together. Replacement must
	// not create a duplicate script ID in this temporary validation document.
	var candidate struct {
		ID string `json:"id"`
	}
	if rewriteStrictJSONScriptID(data, &candidate.ID) != nil {
		return rewrite.Script{}, &rewrite.Error{Code: "rewrite_invalid"}
	}
	kept := []rewrite.Script{}
	for _, script := range scripts {
		if script.ID != candidate.ID {
			kept = append(kept, script)
		}
	}
	rules, _ := json.Marshal(old.Document().Rules)
	existing, _ := json.Marshal(kept)
	b := append([]byte(`{"version":2,"enabled":false,"rules":`), rules...)
	b = append(b, []byte(`,"scripts":[`)...)
	if len(kept) > 0 {
		b = append(b, existing[1:len(existing)-1]...)
		b = append(b, ',')
	}
	b = append(b, data...)
	b = append(b, []byte(`]}`)...)
	snapshot, e := rewrite.Parse(b)
	if e != nil {
		return rewrite.Script{}, e
	}
	return snapshot.Document().Scripts[len(kept)], nil
}
func rewriteStrictJSONScriptID(data []byte, id *string) error {
	var value map[string]json.RawMessage
	if rewriteStrictJSON(data, &value) != nil || json.Unmarshal(value["id"], id) != nil {
		return &rewrite.Error{Code: "rewrite_invalid"}
	}
	return nil
}
func rewriteStrictJSON(data []byte, out any) error {
	if !utf8.Valid(data) {
		return &rewrite.Error{Code: "rewrite_invalid"}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return &rewrite.Error{Code: "rewrite_invalid"}
	}
	var rest any
	if d.Decode(&rest) != io.EOF {
		return &rewrite.Error{Code: "rewrite_invalid"}
	}
	return nil
}
func parseRewriteRule(data []byte, old *rewrite.Snapshot) (rewrite.Rule, error) {
	// Embed untrusted input verbatim so the domain parser detects duplicate keys.
	scripts, _ := json.Marshal(old.Document().Scripts)
	b := append([]byte(`{"version":2,"enabled":false,"scripts":`), scripts...)
	b = append(b, []byte(`,"rules":[`)...)
	b = append(b, data...)
	b = append(b, []byte(`]}`)...)
	s, e := rewrite.Parse(b)
	if e != nil {
		return rewrite.Rule{}, e
	}
	if len(s.Document().Rules) != 1 {
		return rewrite.Rule{}, &rewrite.Error{Code: "rewrite_invalid"}
	}
	return s.Document().Rules[0], nil
}
func rewriteResult(v any) (any, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var out output.RewriteResult
	e = json.Unmarshal(b, &out)
	return out, e
}
func rewriteExport(path string, data []byte) (any, error) {
	if path == "-" {
		return output.Raw(data), nil
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, rewriteFailure("rewrite_export_failed")
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, e = f.Write(data); e != nil {
		return nil, rewriteFailure("rewrite_export_failed")
	}
	if e = f.Sync(); e != nil {
		return nil, rewriteFailure("rewrite_export_failed")
	}
	if e = f.Close(); e != nil {
		return nil, rewriteFailure("rewrite_export_failed")
	}
	ok = true
	return output.RewriteResult{"exported": true, "sensitive": true, "path": path}, nil
}
func requestRewriteCommand(ctx context.Context, args []string, paths nagiruntime.Paths) (any, error) {
	o, e := parseRewriteArguments(args)
	if e != nil {
		return nil, fail("usage", e)
	}
	switch o.path {
	case "request-rewrite status":
		v, e := requestRewriteStatus(ctx, paths)
		if e != nil {
			return nil, e
		}
		return rewriteResult(v)
	case "request-rewrite enable", "request-rewrite disable":
		v, e := requestRewriteSetEnabled(ctx, paths, o.path == "request-rewrite enable")
		if e != nil {
			return nil, e
		}
		return rewriteResult(v)
	case "request-rewrite ca status":
		v, e := requestRewriteCAStatus(paths)
		if e != nil {
			return nil, e
		}
		return rewriteResult(v)
	case "request-rewrite ca generate":
		v, e := (rewrite.CAStore{ConfigDir: paths.ConfigDir}).Generate(time.Now())
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		return rewriteResult(v)
	case "request-rewrite ca export":
		b, e := (rewrite.CAStore{ConfigDir: paths.ConfigDir}).ExportPublic(time.Now())
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		v, e := rewriteExport(o.values[0], b)
		if m, ok := v.(output.RewriteResult); ok {
			m["sensitive"] = false
		}
		return v, e
	case "request-rewrite ca trust install", "request-rewrite ca trust remove":
		v, e := requestRewriteCATrust(ctx, paths, strings.HasSuffix(o.path, "install"))
		if e != nil {
			return nil, e
		}
		return rewriteResult(v)
	}
	name := o.profile
	if name == "" {
		name, e = profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if e != nil {
			return nil, rewriteFailure("rewrite_profile_required")
		}
	}
	data, _ := ctx.Value(rewriteInputKey{}).([]byte)
	if o.path == "request-rewrite validate" && len(o.values) > 0 {
		snapshot, e := rewrite.Parse(data)
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		return output.RewriteResult{"profile": name, "valid": true, "configured_rule_version": snapshot.Version()}, nil
	}
	store := rewriteStore(paths, name)
	snapshot, e := store.Load()
	if e != nil {
		return nil, rewriteDomainError(e)
	}
	result := output.RewriteResult{"profile": name, "configured_rule_version": snapshot.Version()}
	switch o.path {
	case "request-rewrite scripts list":
		result["scripts"] = snapshot.ScriptSummaries()
		return result, nil
	case "request-rewrite scripts show":
		for _, script := range snapshot.ScriptSummaries() {
			if script.ID == o.values[0] {
				result["script"] = script
				return result, nil
			}
		}
		return nil, rewriteFailure("rewrite_script_not_found")
	case "request-rewrite scripts source":
		for _, script := range snapshot.Document().Scripts {
			if script.ID == o.values[0] {
				result["sensitive"] = true
				result["script"] = script
				return result, nil
			}
		}
		return nil, rewriteFailure("rewrite_script_not_found")
	case "request-rewrite rules list":
		result["rules"] = snapshot.RuleSummaries()
		return result, nil
	case "request-rewrite rules show":
		for _, r := range snapshot.RuleSummaries() {
			if r.ID == o.values[0] {
				result["rule"] = r
				return result, nil
			}
		}
		return nil, rewriteFailure("rewrite_rule_not_found")
	case "request-rewrite export":
		return rewriteExport(o.values[0], snapshot.ExportSensitive())
	case "request-rewrite validate":
		result["valid"] = true
		result["configured_rule_version"] = snapshot.Version()
		return result, nil
	case "request-rewrite preview":
		v, e := snapshot.PreviewSensitive(ctx, data)
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		return rewriteResult(v)
	}
	var saved rewrite.SaveResult
	switch o.path {
	case "request-rewrite scripts add", "request-rewrite scripts update":
		var d rewrite.Script
		d, e = parseRewriteScript(data, snapshot)
		if e == nil {
			if strings.HasSuffix(o.path, "add") {
				saved, e = store.AddScript(ctx, d)
			} else {
				saved, e = store.UpdateScript(ctx, o.values[0], d)
			}
		}
	case "request-rewrite scripts remove":
		saved, e = store.RemoveScript(ctx, o.values[0])
	case "request-rewrite rules add", "request-rewrite rules update":
		var r rewrite.Rule
		r, e = parseRewriteRule(data, snapshot)
		if e == nil {
			if strings.HasSuffix(o.path, "add") {
				saved, e = store.AddRule(ctx, r)
			} else {
				saved, e = store.UpdateRule(ctx, o.values[0], r)
			}
		}
	case "request-rewrite rules remove":
		saved, e = store.RemoveRule(ctx, o.values[0])
	case "request-rewrite rules enable", "request-rewrite rules disable":
		saved, e = store.SetRuleEnabled(ctx, o.values[0], strings.HasSuffix(o.path, "enable"))
	case "request-rewrite rules reorder":
		var ids []string
		e = rewriteStrictJSON(data, &ids)
		if e == nil {
			saved, e = store.ReorderRules(ctx, ids)
		}
	case "request-rewrite import":
		saved, e = store.ImportSensitive(ctx, data)
	default:
		return nil, rewriteFailure("rewrite_invalid")
	}
	if e != nil {
		return nil, rewriteDomainError(e)
	}
	result["saved"] = saved.Saved
	result["configured_rule_version"] = saved.ConfiguredRuleVersion
	// Status does not acquire config.lock, which the mutation caller already holds.
	status, e := rewriteStatusFor(ctx, paths, name)
	if e != nil {
		return nil, e
	}
	result["active_rule_version"] = status.ActiveRuleVersion
	result["actual"] = status.Actual
	return result, nil
}

// Completion reads only IDs from local immutable snapshots; it never contacts mihomo.
func rewriteCandidates(args []string) (any, error) {
	paths, e := nagiruntime.Resolve()
	if e != nil {
		return nil, e
	}
	name := ""
	if len(args) == 4 {
		name = args[3]
	} else {
		name, e = rewriteSelected(paths)
		if e != nil {
			return nil, rewriteDomainError(e)
		}
	}
	s, e := (rewrite.Store{ConfigDir: paths.ConfigDir, Profile: name}).Load()
	if e != nil {
		return nil, rewriteDomainError(e)
	}
	out := output.Candidates{}
	if args[2] == "rewrite-scripts" {
		for _, d := range s.ScriptSummaries() {
			out = append(out, d.ID)
		}
	} else {
		for _, r := range s.RuleSummaries() {
			out = append(out, r.ID)
		}
	}
	return out, nil
}
