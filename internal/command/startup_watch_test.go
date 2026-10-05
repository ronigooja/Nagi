package command

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStartupWatchStep(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check map[string]any
		want  string
		err   string
	}{
		{"healthy", map[string]any{"running": true, "control_api": true}, "startup check", ""},
		{"crashed", map[string]any{"recovered": true}, "startup check,start", ""},
		{"stopped", map[string]any{"running": false}, "startup check,start", ""},
		{"api unavailable", map[string]any{"running": true, "control_api": false}, "startup check", "control API is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			err := startupWatchStep(context.Background(), func(args ...string) (any, error) {
				calls = append(calls, strings.Join(args, " "))
				if args[0] == "startup" {
					return tc.check, nil
				}
				return map[string]any{"started": true}, nil
			})
			if strings.Join(calls, ",") != tc.want || (tc.err == "" && err != nil) || (tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err))) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
}

func TestStartupWatchStepPropagatesFailure(t *testing.T) {
	want := errors.New("startup failed")
	err := startupWatchStep(context.Background(), func(...string) (any, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
}
