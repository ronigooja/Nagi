package command

import "strings"

func init() {
	type entry struct{ path, syntax, summary, detail, example string }
	entries := []entry{
		{"", "<command>", "Manage TUN request rewriting", "Default off. Configure explicit domain consent and ordered request rules; enable requires running TUN, compatible mihomo and a trusted local CA. Commands: status, enable, disable, domains, rules, import, export, validate, preview, ca.", "status"},
		{"status", "", "Show confirmed rewrite state", "Show saved intent separately from actual runtime state, versions, CA trust and recovery guidance.", "status"},
		{"enable", "", "Enable rewriting for selected profile", "Requires compatible mihomo, actually running TUN and trusted CA. Does not start TUN or install trust. --profile is not accepted.", "enable"},
		{"disable", "", "Disable rewriting for selected profile", "Drain and disable runtime rewriting. Retain domain/rule data, CA and OS trust. --profile is not accepted.", "disable"},
		{"domains", "<command>", "Manage explicit domain consent", "Commands: list, add, update, remove. Exact domain or explicit *.service.example wildcard; wildcard excludes apex. No global consent.", "domains list"},
		{"domains list", "[--profile NAME]", "List authorized domains", "List domain IDs and normalized patterns for the selected or named profile.", "domains list"},
		{"domains add", "FILE|- [--profile NAME]", "Add domain consent", "Read one {id,pattern} JSON object from a private local file or stdin; validate and save transactionally.", "domains add domain.json"},
		{"domains update", "ID FILE|- [--profile NAME]", "Replace domain consent", "Read one {id,pattern} JSON object with the same ID. Reject dangling rule references.", "domains update api domain.json"},
		{"domains remove", "ID [--profile NAME]", "Remove domain consent", "Refuse referenced consent; change dependent rules first.", "domains remove api"},
		{"rules", "<command>", "Manage ordered request rules", "Commands: list, show, add, update, remove, enable, disable, reorder. Inputs contain secret values; ordinary reads show redacted summaries.", "rules list"},
		{"rules list", "[--profile NAME]", "List ordered rule summaries", "Show IDs, enabled state, match, operation kinds and on_error. Omit operation values and redirect consent.", "rules list"},
		{"rules show", "ID [--profile NAME]", "Show redacted rule summary", "Show one rule summary. Use explicit export for complete sensitive values.", "rules show edit-api"},
		{"rules add", "FILE|- [--profile NAME]", "Append a rule", "Read one complete rule JSON object from private file or stdin (256 KiB maximum); validate against profile consent. Never enables rewriting.", "rules add rule.json"},
		{"rules update", "ID FILE|- [--profile NAME]", "Replace a rule", "Read one complete rule JSON object with the same ID. Save and activate transactionally when enabled.", "rules update edit-api rule.json"},
		{"rules remove", "ID [--profile NAME]", "Remove a rule", "Remove exactly one rule transactionally.", "rules remove edit-api"},
		{"rules enable", "ID [--profile NAME]", "Enable one rule", "Enable the rule without enabling request rewriting itself.", "rules enable edit-api"},
		{"rules disable", "ID [--profile NAME]", "Disable one rule", "Disable the rule while retaining its position and data.", "rules disable edit-api"},
		{"rules reorder", "FILE|- [--profile NAME]", "Replace rule order", "Read a JSON array containing every rule ID exactly once, including disabled rules.", "rules reorder order.json"},
		{"import", "FILE|- [--profile NAME]", "Import sensitive rules and domains", "Read a complete v1 JSON document (256 KiB maximum). Imported enabled must be false; retain existing enabled intent independently. Validate and replace transactionally.", "import rules.json"},
		{"export", "FILE|- [--profile NAME]", "Export complete sensitive rule data", "Explicit sensitive raw JSON export; values may contain credentials. Always exports enabled:false, excludes CA and runtime secrets. Refuse existing files, create mode 0600. - writes raw stdout. Reject --json.", "export rules.json"},
		{"validate", "[FILE|-] [--profile NAME]", "Validate without network or activation", "Validate a supplied v1 document or the persisted profile document. No DNS, forwarding, persistence or activation. Read private files or stdin, maximum 256 KiB.", "validate"},
		{"preview", "FILE|- [--profile NAME]", "Transform a sensitive local request fixture", "Explicit sensitive preview: {scheme,host,port,method,path,query,headers,body_base64}. No DNS, forwarding or persistence. Maximum 12 MiB encoded fixture and 8 MiB decoded body. Output contains transformed request values and may contain credentials; it does not prove live coverage.", "preview fixture.json"},
		{"ca", "<command>", "Manage local rewrite CA", "Commands: status, generate, export, trust. Generation and trust installation are separate explicit operations. No replacement/rotation command.", "ca status"},
		{"ca status", "", "Show local CA and trust metadata", "Show public CA metadata and assessed OS trust; unknown is not evidence of trust.", "ca status"},
		{"ca generate", "", "Generate local CA explicitly", "Create one private per-user CA. Refuse existing CA. Does not install OS trust.", "ca generate"},
		{"ca export", "FILE|-", "Export public CA certificate", "Public PEM only; never export private key. Refuse existing file, create mode 0600; - writes raw stdout. Reject --json.", "ca export nagi-ca.pem"},
		{"ca trust", "<command>", "Change OS trust explicitly", "Commands: install, remove. Affects platform user trust; platform verification is required. Unsupported stores return an actionable error.", "ca trust install"},
		{"ca trust install", "", "Install CA trust explicitly", "Install public certificate into the supported OS trust store and assess resulting trust. May require platform authorization.", "ca trust install"},
		{"ca trust remove", "", "Remove CA trust explicitly", "Remove OS trust without deleting the private CA. Disable rewriting first; verify trust removal in platform state.", "ca trust remove"},
	}
	for _, e := range entries {
		path := "request-rewrite"
		if e.path != "" {
			path += " " + e.path
		}
		syntax := path
		if e.syntax != "" {
			syntax += " " + e.syntax
		}
		min, max := rewriteArity(path)
		if e.path == "" || e.path == "domains" || e.path == "rules" || e.path == "ca" || e.path == "ca trust" {
			max = -1
		} else if e.path != "status" && e.path != "enable" && e.path != "disable" && e.path != "ca status" && e.path != "ca generate" && e.path != "ca export" && e.path != "ca trust install" && e.path != "ca trust remove" {
			max += 2
		}
		commandSpecs = append(commandSpecs, commandSpec{path, syntax, e.summary, e.detail, "nagi request-rewrite " + e.example, "Run `nagi request-rewrite status` for readiness and recovery.", min, max})
	}
	// Derive the complete public command tree from the compatibility entrypoint.
	// Arity, help and discovery must stay aligned as operations are added.
	for _, spec := range append([]commandSpec(nil), commandSpecs...) {
		if spec.path != "request-rewrite" && !strings.HasPrefix(spec.path, "request-rewrite ") {
			continue
		}
		spec.path = strings.Replace(spec.path, "request-rewrite", "rewrite", 1)
		spec.syntax = strings.Replace(spec.syntax, "request-rewrite", "rewrite", 1)
		spec.example = strings.Replace(spec.example, "request-rewrite", "rewrite", 1)
		spec.hint = strings.ReplaceAll(spec.hint, "nagi request-rewrite", "nagi rewrite")
		spec.detail += " Also available as nagi request-rewrite; both use the same implementation and JSON contract."
		commandSpecs = append(commandSpecs, spec)
	}
}
