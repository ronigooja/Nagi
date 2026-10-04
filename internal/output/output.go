package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Envelope struct {
	OK    bool     `json:"ok"`
	Data  any      `json:"data,omitempty"`
	Error *Failure `json:"error,omitempty"`
}

func Write(w io.Writer, jsonMode bool, data any) error {
	if jsonMode {
		return json.NewEncoder(w).Encode(Envelope{OK: true, Data: data})
	}
	switch v := data.(type) {
	case string:
		_, err := fmt.Fprintln(w, v)
		return err
	case []string:
		_, err := fmt.Fprintln(w, strings.Join(v, "\n"))
		return err
	default:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	}
}

func WriteError(w io.Writer, jsonMode bool, code, message string) {
	if jsonMode {
		_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: &Failure{Code: code, Message: message}})
		return
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", code, message)
}
