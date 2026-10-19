package command

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
)

var messageFormat = regexp.MustCompile(`%[-+#0 .0-9]*[a-zA-Z%]`)

// messageError attaches an event descriptor at its source. Formatting is done
// once, preserves the English error/wrapping contract, and stores safe scalars.
func messageError(id, format string, values ...any) *i18n.Message {
	original := fmt.Errorf(format, values...)
	arguments := map[string]any{}
	index := 0
	for _, conversion := range messageFormat.FindAllString(format, -1) {
		if conversion == "%%" {
			continue
		}
		if index >= len(values) {
			break
		}
		if conversion[len(conversion)-1] == 'w' {
			conversion = strings.TrimSuffix(conversion, "w") + "v"
		}
		arguments[fmt.Sprintf("arg%d", index)] = output.RedactError(fmt.Sprintf(conversion, values[index]))
		index++
	}
	return i18n.NewMessage(id, arguments, original.Error(), original)
}
