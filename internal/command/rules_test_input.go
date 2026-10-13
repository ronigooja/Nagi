package command

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ronigooja/Nagi/internal/rules"
)

func ruleTestArguments(args []string) (rules.TestRequest, error) {
	var req rules.TestRequest
	if len(args) < 3 {
		return req, errors.New("TARGET is required")
	}
	req.Target = args[2]
	seen := map[string]bool{}
	for i := 3; i < len(args); i += 2 {
		flag := args[i]
		if flag != "--ip" && flag != "--port" && flag != "--network" && flag != "--process" {
			return req, fmt.Errorf("unknown option %s", flag)
		}
		if seen[flag] || i+1 >= len(args) || args[i+1] == "" {
			return req, fmt.Errorf("%s requires one value and cannot be repeated", flag)
		}
		seen[flag] = true
		switch flag {
		case "--ip":
			req.IP = args[i+1]
		case "--network":
			req.Network = args[i+1]
		case "--process":
			req.Process = args[i+1]
		case "--port":
			port, err := strconv.ParseUint(args[i+1], 10, 16)
			if err != nil || port == 0 {
				return req, errors.New("--port must be an integer from 1 to 65535")
			}
			req.Port = uint16(port)
		}
	}
	return req, req.Validate()
}
