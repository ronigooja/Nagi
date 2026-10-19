package output

import (
	"encoding/json"
	"io"
	"time"
)

// CaptureResult renders explicitly authorized original values in full.
type CaptureResult struct{ Value any }

func (c CaptureResult) MarshalJSON() ([]byte, error) { return json.Marshal(c.Value) }
func writeCapture(w io.Writer, c CaptureResult) error {
	b, e := json.Marshal(c.Value)
	if e != nil {
		return e
	}
	var v any
	if e = json.Unmarshal(b, &v); e != nil {
		return e
	}
	var walk func(any)
	walk = func(x any) {
		switch a := x.(type) {
		case map[string]any:
			for k, v := range a {
				if k == "started" {
					if s, ok := v.(string); ok {
						if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
							a[k] = t.Local().Format("2006-01-02 15:04")
						}
					}
				} else {
					walk(v)
				}
			}
		case []any:
			for _, v := range a {
				walk(v)
			}
		}
	}
	walk(v)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
