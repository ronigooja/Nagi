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
		fmt.Fprint(s.out, s.styled(stylePrompt, output.RedactError(label)))
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

// readChoice adds the menu-wide quit key to numeric choice prompts. Text
// prompts use readLine directly so values such as "q" remain literal.
func (s *Session) readChoice() (string, error) {
	value, err := s.readLine("Choice: ")
	if err == nil && value == "q" {
		return "", ErrQuit
	}
	return value, err
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
	view := PageView{}
	for _, value := range choices {
		view.Items = append(view.Items, Item{Label: value, Name: value})
	}
	redraw := true
	for {
		if redraw {
			s.render(&Page{Title: label}, view, page, 0)
			redraw = false
		}
		value, err := s.readChoice()
		if err != nil {
			return "", err
		}
		if value == "" {
			continue
		}
		if value == "0" {
			return "", ErrBack
		}
		numbered := numberedChoices(view, page)
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > len(numbered) || strconv.Itoa(n) != value {
			s.writeFeedback(feedbackWarning, "Choose a listed number.")
			continue
		}
		choice := numbered[n-1]
		if choice.PageDelta != 0 {
			page += choice.PageDelta
			redraw = true
			continue
		}
		return choice.Item.Name, nil
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
				s.writeFeedback(feedbackWarning, "This field is required.")
				continue
			}
			if field.Validate != nil {
				if err := field.Validate(value); err != nil {
					s.writeFeedback(feedbackWarning, "Invalid value: "+err.Error())
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
		s.writeFeedback(feedbackWarning, "Submission failed. Values are retained. Choose a field to edit or submit again.")
		for i, field := range fields {
			s.writeChoice(i+1, field.Label)
		}
		s.writeChoice(len(fields)+1, "Submit again")
		s.writeChoice(0, "Cancel")
		s.flushResult()
		for {
			value, err := s.readChoice()
			if err != nil {
				return result, err
			}
			if value == "" {
				continue
			}
			if value == "0" {
				return result, ErrBack
			}
			if value == strconv.Itoa(len(fields)+1) {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				result = submit(values)
				break
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > len(fields) || strconv.Itoa(n) != value {
				s.writeFeedback(feedbackWarning, "Choose a listed number.")
				continue
			}
			if err := edit(fields[n-1]); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
