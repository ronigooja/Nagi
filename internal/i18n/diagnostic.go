package i18n

// SafeDiagnostic applies the ordinary diagnostic URL redaction at message
// origins. Raw content explicitly viewed or exported never passes through it.
func SafeDiagnostic(value string) string {
	return diagnosticURL.ReplaceAllString(value, "[redacted URL]")
}
