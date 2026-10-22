package i18n

import "github.com/ronigooja/Nagi/internal/sensitive"

// SafeDiagnostic removes URL authentication values while retaining useful
// destination details. Raw explicitly viewed or exported content bypasses it.
func SafeDiagnostic(value string) string {
	return sensitive.RedactURLCredentials(value)
}
