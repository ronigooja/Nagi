package menu

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Session) Printf(format string, args ...any) { fmt.Fprintf(s.out, format, args...) }
func (s *Session) WriteLine(line string)             { fmt.Fprintln(s.out, line) }

func (s *Session) render(page *Page, view PageView, index int, depth int) {
	fmt.Fprintf(s.out, "\n%s\n", page.Title)
	if view.Context != "" {
		fmt.Fprintln(s.out, view.Context)
	}
	if len(view.Items) == 0 && view.Empty != "" {
		fmt.Fprintln(s.out, view.Empty)
	}
	start := index * pageSize
	end := start + pageSize
	if end > len(view.Items) {
		end = len(view.Items)
	}
	for n, item := range view.Items[start:end] {
		fmt.Fprintf(s.out, "%d. %s\n", n+1, item.Label)
	}
	if len(view.Items) > pageSize {
		fmt.Fprintf(s.out, "Page %d/%d\n", index+1, (len(view.Items)+pageSize-1)/pageSize)
	}
	if depth > 1 {
		fmt.Fprintln(s.out, "b. Back")
	} else {
		fmt.Fprintln(s.out, "0. Exit")
	}
	if end < len(view.Items) {
		fmt.Fprintln(s.out, "n. Next")
	}
	if index > 0 {
		fmt.Fprintln(s.out, "p. Previous")
	}
	fmt.Fprintln(s.out, "r. Refresh  h. Help  q. Quit")
}

func (s *Session) showResult(result Result) {
	if result.Error != nil || result.Code != 0 {
		message := "Command failed"
		if result.Error != nil {
			message = result.Error.Message
		}
		fmt.Fprintf(s.out, "Error: %s\n", message)
		return
	}
	if result.Data == nil {
		fmt.Fprintln(s.out, "Done.")
		return
	}
	switch value := result.Data.(type) {
	case string:
		fmt.Fprintln(s.out, value)
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			fmt.Fprintln(s.out, "Done.")
			return
		}
		fmt.Fprintln(s.out, strings.TrimSpace(string(data)))
	}
}
