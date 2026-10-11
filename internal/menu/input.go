package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/output"
)

var ErrQuit = errors.New("quit menu")
var ErrBack = errors.New("back")

func (s *Session) readLine(label string) (string, error) {
	if label != "" {
		fmt.Fprint(s.out, output.RedactError(label))
	}
	line, err := s.interruptibleRead()
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", ErrQuit
	}
	return strings.TrimSpace(line), nil
}

// Prompt treats all text literally. Ctrl-C cancels the current flow.
func (s *Session) Prompt(label string) (string, error) {
	return s.readLine(label + " (Ctrl-C cancels): ")
}

func (s *Session) Select(label string, choices []string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no choices available")
	}
	page := 0
	filter := ""
	redraw := true
	for {
		shown := []string{}
		for _, choice := range choices {
			if strings.Contains(strings.ToLower(choice), strings.ToLower(filter)) {
				shown = append(shown, choice)
			}
		}
		start := page * pageSize
		end := start + pageSize
		if end > len(shown) {
			end = len(shown)
		}
		if redraw {
			s.WriteLine(output.RedactError(label))
			for i, value := range shown[start:end] {
				s.Printf("%d. %s\n", i+1, output.RedactError(value))
			}
			s.WriteLine("b. Cancel  q. Exit menu  /. Filter  n/p. Next/previous")
			redraw = false
		}
		value, err := s.readLine("Choice: ")
		if err != nil {
			return "", err
		}
		lower := strings.ToLower(value)
		if strings.HasPrefix(value, "/") {
			filter = strings.TrimSpace(value[1:])
			page = 0
			redraw = true
			continue
		}
		switch lower {
		case "q":
			return "", ErrQuit
		case "b":
			return "", ErrBack
		case "n":
			if end < len(shown) {
				page++
				redraw = true
			}
			continue
		case "p":
			if page > 0 {
				page--
				redraw = true
			}
			continue
		case "":
			continue
		}
		n, e := strconv.Atoi(value)
		if e == nil && n >= 1 && start+n <= end {
			return shown[start+n-1], nil
		}
		s.WriteLine("Choose a listed number, b, or q.")
	}
}

// Field validation happens locally; submit remains the existing CLI operation.
type Field struct {
	Key      string
	Label    string
	Required bool
	Validate func(string) error
	Choices  []string
}

// Form preserves entered fields after validation or command failure. A failed
// submission is never retried until the user explicitly chooses Submit.
func (s *Session) Form(ctx context.Context, fields []Field, values map[string]string, submit func(map[string]string) Result) (Result, error) {
	if values == nil {
		values = map[string]string{}
	}
	edit := func(field Field) error {
		for {
			label := field.Label
			if values[field.Key] != "" {
				label += " [blank keeps current value]"
			}
			var value string
			var err error
			if len(field.Choices) > 0 {
				value, err = s.Select(label, field.Choices)
			} else {
				value, err = s.Prompt(label)
			}
			if err != nil {
				return err
			}
			if value == "" && values[field.Key] != "" {
				value = values[field.Key]
			}
			if field.Required && strings.TrimSpace(value) == "" {
				s.WriteLine("This field is required.")
				continue
			}
			if field.Validate != nil {
				if err := field.Validate(value); err != nil {
					s.Printf("Invalid value: %s\n", output.RedactError(err.Error()))
					continue
				}
			}
			values[field.Key] = value
			return nil
		}
	}
	for _, field := range fields {
		if err := edit(field); err != nil {
			return Result{}, err
		}
	}
	result := submit(values)
	for result.Code != 0 || result.Error != nil {
		s.WriteLine("Submission failed. Values are retained. Choose a field to edit, s to submit again, or Ctrl-C to cancel.")
		for i, field := range fields {
			s.Printf("%d. %s\n", i+1, field.Label)
		}
		for {
			value, err := s.readLine("Choice: ")
			if err != nil {
				return result, err
			}
			if value == "s" {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				result = submit(values)
				break
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > len(fields) {
				s.WriteLine("Choose a field number or s.")
				continue
			}
			if err := edit(fields[n-1]); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
