package command

import (
	"errors"
	"github.com/ronigooja/Nagi/internal/profile"
	"strconv"
)

func providerSeconds(raw string) (int, error) {
	if raw == "off" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 31536000 {
		return 0, errors.New("SECONDS must be off or an integer from 1 to 31536000")
	}
	return n, nil
}
func providerAddOptions(args []string) (int, []string, error) {
	n := profile.DefaultProviderInterval
	rest := args[6:]
	if len(rest) > 0 && rest[0] != "--test-url" {
		var err error
		n, err = providerSeconds(rest[0])
		if err != nil {
			return 0, nil, err
		}
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return n, nil, nil
	}
	if len(rest) != 2 || rest[0] != "--test-url" {
		return 0, nil, errors.New("expected [SECONDS|off] [--test-url URL]")
	}
	if err := profile.ValidateHealthcheckURL(rest[1]); err != nil {
		return 0, nil, err
	}
	return n, []string{rest[1]}, nil
}
