package command

import (
	"context"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
)

type languageContextKey struct{}

// LanguageContext exposes the invocation's explicit, immutable rendering context.
func LanguageContext(ctx context.Context) i18n.Context {
	locale, _ := ctx.Value(languageContextKey{}).(i18n.Context)
	return locale
}

// parseLanguage runs after private dispatch and before --json extraction. Only leading
// language options are consumed; the command and its operands remain untouched.
func parseLanguage(args []string, getenv func(string) string) ([]string, i18n.Context, *i18n.Message) {
	explicit := ""
	seen := false

	filtered := make([]string, 0, len(args))
	failure := func(id, text string) ([]string, i18n.Context, *i18n.Message) {
		language, _ := i18n.Resolve(explicit, getenv)
		var args map[string]any
		if id == "cli.language.invalid" {
			args = map[string]any{"value": explicit}
			text = "Unsupported language " + explicit + "; use en or zh-Hans."
		}
		return nil, i18n.New(language), i18n.NewMessage(id, args, text, nil)
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--json" {
			filtered = append(filtered, arg)
			continue
		}
		if arg != "--lang" && !strings.HasPrefix(arg, "--lang=") {
			filtered = append(filtered, args[index:]...)
			return filtered, resolveLanguage(explicit, getenv), nil
		}
		if seen {
			return failure("cli.language.duplicate", "--lang may be specified only once.")
		}
		seen = true
		if arg == "--lang" {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return failure("cli.language.missing", "--lang requires a language; use en or zh-Hans.")
			}
			index++
			explicit = args[index]
		} else {
			explicit = strings.TrimPrefix(arg, "--lang=")
		}
		if explicit == "" {
			return failure("cli.language.missing", "--lang requires a language; use en or zh-Hans.")
		}
		if _, ok := i18n.Normalize(explicit); !ok {
			return failure("cli.language.invalid", "Unsupported language; use en or zh-Hans.")
		}
	}
	language, _ := i18n.Resolve(explicit, getenv)
	return filtered, i18n.New(language), nil
}

func resolveLanguage(explicit string, getenv func(string) string) i18n.Context {
	language, _ := i18n.Resolve(explicit, getenv)
	return i18n.New(language)
}
