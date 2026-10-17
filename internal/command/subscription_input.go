package command

import (
	"errors"
	"strconv"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func subscriptionAddOptions(args []string) (subscription.AddOptions, error) {
	var options subscription.AddOptions
	if len(args) < 4 {
		return options, errors.New("NAME and URL are required")
	}
	seen := map[string]bool{}
	for i := 4; i < len(args); i += 2 {
		flag := args[i]
		if (flag != "--schedule" && flag != "--test-url") || seen[flag] || i+1 >= len(args) {
			return options, errors.New("expected optional --schedule SECONDS|off and --test-url URL, each once")
		}
		seen[flag] = true
		if flag == "--schedule" {
			seconds := int64(0)
			if args[i+1] != "off" {
				var err error
				seconds, err = strconv.ParseInt(args[i+1], 10, 64)
				if err != nil || seconds < 1 || seconds > subscription.MaxScheduleSeconds {
					return options, errors.New("schedule interval must be 1..31536000 seconds or off")
				}
			}
			options.ScheduleSeconds = &seconds
		} else {
			value := args[i+1]
			if !subscription.ValidHealthcheckURL(value) {
				return options, errors.New("test URL must be absolute HTTP(S) without user information or fragment")
			}
			options.HealthcheckURL = &value
		}
	}
	return options, nil
}
