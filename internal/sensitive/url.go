// Package sensitive contains narrow helpers for content that has a known
// structure and a known output boundary. It is not a general secret scanner.
package sensitive

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var urlPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)

// RedactURLCredentials keeps useful URL context while hiding authentication
// material carried in userinfo or commonly named secret query parameters.
func RedactURLCredentials(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, redactURL)
}

// RedactURLDetails keeps only the endpoint origin for reports intended to be
// shared, removing paths, query values, fragments, and userinfo.
func RedactURLDetails(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(raw string) string {
		trimmed, trailing := trimURLPunctuation(raw)
		u, err := url.Parse(trimmed)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "[redacted URL]" + trailing
		}
		return u.Scheme + "://" + u.Host + trailing
	})
}

func redactURL(raw string) string {
	trimmed, trailing := trimURLPunctuation(raw)
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	if u.User != nil {
		u.User = nil
	}
	if u.RawQuery != "" {
		u.RawQuery = redactParameters(u.RawQuery)
	}
	if u.Fragment != "" {
		u.Fragment = redactParameters(u.Fragment)
		u.RawFragment = ""
	}
	return u.String() + trailing
}

func redactParameters(raw string) string {
	parts := strings.Split(raw, "&")
	for i, part := range parts {
		key, _, hasValue := strings.Cut(part, "=")
		decoded, decodeErr := url.QueryUnescape(key)
		if hasValue && decodeErr == nil && secretQueryKey(decoded) {
			parts[i] = key + "=%5Bredacted%5D"
		}
	}
	return strings.Join(parts, "&")
}

func trimURLPunctuation(raw string) (string, string) {
	trimmed := raw
	trailing := ""
	for len(trimmed) > 0 && strings.ContainsRune(".,);]}", rune(trimmed[len(trimmed)-1])) {
		trailing = trimmed[len(trimmed)-1:] + trailing
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed, trailing
}

func secretQueryKey(key string) bool {
	var normalized strings.Builder
	previous := rune(0)
	for _, current := range key {
		if unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			normalized.WriteByte('_')
		}
		normalized.WriteRune(unicode.ToLower(current))
		previous = current
	}
	key = strings.ReplaceAll(strings.ReplaceAll(normalized.String(), "-", "_"), ".", "_")
	for _, marker := range []string{"token", "password", "passwd", "secret", "credential", "authorization", "auth", "key", "signature", "sig", "jwt", "private_key", "code"} {
		if key == marker || strings.HasPrefix(key, marker+"_") || strings.Contains(key, "_"+marker+"_") || strings.HasSuffix(key, "_"+marker) {
			return true
		}
	}
	return false
}
