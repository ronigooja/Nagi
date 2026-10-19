package command

import (
	"strconv"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func subscriptionAddOptions(args []string) (subscription.AddOptions, error) {
	var options subscription.AddOptions
	if len(args) < 4 {
		return options, messageError("cli.error.subscription_input.name_and_url_are_required", "NAME and URL are required")
	}
	seen := map[string]bool{}
	for i := 4; i < len(args); i += 2 {
		flag := args[i]
		if (flag != "--schedule" && flag != "--test-url") || seen[flag] || i+1 >= len(args) {
			return options, messageError("cli.error.subscription_input.expected_optional_schedule_seconds_off_and_test_url_url_each_once", "expected optional --schedule SECONDS|off and --test-url URL, each once")
		}
		seen[flag] = true
		if flag == "--schedule" {
			seconds := int64(0)
			if args[i+1] != "off" {
				var err error
				seconds, err = strconv.ParseInt(args[i+1], 10, 64)
				if err != nil || seconds < 1 || seconds > subscription.MaxScheduleSeconds {
					return options, messageError("cli.error.subscription_input.schedule_interval_must_be_1_31536000_seconds_or_off", "schedule interval must be 1..31536000 seconds or off")
				}
			}
			options.ScheduleSeconds = &seconds
		} else {
			value := args[i+1]
			if !subscription.ValidHealthcheckURL(value) {
				return options, messageError("cli.error.subscription_input.test_url_must_be_absolute_http_s_without_user_information_or_fragmen", "test URL must be absolute HTTP(S) without user information or fragment")
			}
			options.HealthcheckURL = &value
		}
	}
	return options, nil
}
