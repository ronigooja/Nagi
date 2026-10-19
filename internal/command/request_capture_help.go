package command

type rewriteHelpEntry struct{ path, syntax, summary, detail, example string }

func captureHelpEntries() []rewriteHelpEntry {
	entries := []rewriteHelpEntry{{"capture", "<command>", "Capture original TUN request content", "Commands: check, start, status, pause, resume, stop, apps, list, show, body, clear, export, import, fixture, draft. Output contains original credentials and bodies. Requires explicit session start.", "capture status"}, {"draft", "<command>", "Preview or save a capture-derived rule", "Commands: preview, save. Read a versioned insertion and optional original fixture from private JSON or stdin.", "draft preview -"}}
	for _, e := range []struct{ action, syntax, detail string }{
		{"check", "", "Check prerequisites in order, stopping at the first unmet condition. Read-only; does not start a recorder. Returns readiness even when capture cannot start."},
		{"start", "[FILE|-]", "Start a session; optional JSON StartOptions defines scope, owner and body_limit. Requires running TUN, trusted CA and capture-capable engine; never installs prerequisites."},
		{"status", "", "Show recorder session state and limits."}, {"pause", "SESSION", "Pause this session without interrupting open traffic streams."}, {"resume", "SESSION", "Resume the named paused session with a new policy revision."}, {"stop", "SESSION", "Stop collection and retain original history."}, {"apps", "", "List observed original process identities."}, {"list", "[FILE|-]", "List original records; optional JSON ListOptions contains query, filters, cursor and limit."}, {"show", "RECORD", "Show complete original URL and header metadata without body bytes."}, {"body", "RECORD SNAPSHOT OFFSET [LIMIT]", "Read decoded-byte chunks up to 256 KiB; body data is base64 in JSON."}, {"clear", "SESSION", "Clear the named retained session history and invalidate cursors."}, {"export", "FILE [native|har]", "Export raw archive, owner-only exclusive file; - writes stdout. Reject --json."}, {"import", "FILE|-", "Import a native archive as an offline read-only stopped session."}, {"fixture", "RECORD STAGE", "Create a local preview fixture from complete original snapshots."}, {"draft", "RECORD FILE|-", "Read a Selection JSON and derive one validated draft from the original record."},
	} {
		entries = append(entries, rewriteHelpEntry{"capture " + e.action, e.syntax, "Capture: " + e.action, e.detail, "capture " + e.action + " " + captureExample(e.action)})
	}
	for _, action := range []string{"preview", "save"} {
		entries = append(entries, rewriteHelpEntry{"draft " + action, "FILE|- [--profile NAME]", "Draft: " + action, "Read {base_rule_version,position,rule,fixture?,enable}. Preview runs locally; save inserts atomically with version conflict checking and ordinary activation rollback.", "draft " + action + " -"})
	}
	return entries
}
func captureExample(action string) string {
	switch action {
	case "pause", "resume", "stop", "clear":
		return "SESSION"
	case "show":
		return "RECORD"
	case "body":
		return "RECORD request_original 0"
	case "export":
		return "capture.ndjson native"
	case "import":
		return "capture.ndjson"
	case "fixture":
		return "RECORD request"
	case "draft":
		return "RECORD -"
	}
	return ""
}
