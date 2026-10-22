package command

import (
	"regexp"
	"strconv"
)

var rejectedRulePath = regexp.MustCompile(`^rules\[([0-9]+)\]$`)

// Display native zero-based positions as one-based rule numbers.
func rejectedRuleNumber(path string) string {
	match := rejectedRulePath.FindStringSubmatch(path)
	if match == nil {
		return ""
	}
	index, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(index+1, 10)
}
