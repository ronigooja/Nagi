package menu

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var ErrQuit = errors.New("quit menu")
var ErrBack = errors.New("back")

func (s *Session) readLine(label string) (string, error) {
	if label != "" {
		fmt.Fprint(s.out, label)
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

func (s *Session) Prompt(label string) (string, error) {
	value, err := s.readLine(label + " (b=back, q=quit): ")
	if err != nil {
		return "", err
	}
	switch strings.ToLower(value) {
	case "q":
		return "", ErrQuit
	case "b":
		return "", ErrBack
	}
	return value, nil
}

func (s *Session) Confirm(label string) (bool, error) {
	value, err := s.Prompt(label + " Type yes to confirm")
	return value == "yes", err
}

func (s *Session) Select(label string, choices []string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no choices available")
	}
	page := 0
	for {
		fmt.Fprintln(s.out, label)
		start := page * pageSize
		end := start + pageSize
		if end > len(choices) {
			end = len(choices)
		}
		for i, value := range choices[start:end] {
			fmt.Fprintf(s.out, "%d. %s\n", i+1, value)
		}
		fmt.Fprintln(s.out, "b. Back  q. Quit")
		if end < len(choices) {
			fmt.Fprintln(s.out, "n. Next")
		}
		if page > 0 {
			fmt.Fprintln(s.out, "p. Previous")
		}
		value, err := s.readLine("Choice: ")
		if err != nil {
			return "", err
		}
		switch strings.ToLower(value) {
		case "q":
			return "", ErrQuit
		case "b":
			return "", ErrBack
		case "n":
			if end < len(choices) {
				page++
			}
			continue
		case "p":
			if page > 0 {
				page--
			}
			continue
		}
		n, err := strconv.Atoi(value)
		if err == nil && n >= 1 && start+n <= end {
			return choices[start+n-1], nil
		}
		fmt.Fprintln(s.out, "Choose a listed number, b, or q.")
	}
}
