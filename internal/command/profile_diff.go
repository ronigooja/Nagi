package command

import "strings"

// profileDiff reports changed lines after trimming the shared prefix and suffix.
// It deliberately avoids quadratic work on large subscription files.
func profileDiff(before, after []byte) string {
	if string(before) == string(after) {
		return ""
	}
	a, b := strings.SplitAfter(string(before), "\n"), strings.SplitAfter(string(after), "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	var out strings.Builder
	out.WriteString("--- current\n+++ comparison\n")
	for _, line := range a[start:endA] {
		out.WriteByte('-')
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	for _, line := range b[start:endB] {
		out.WriteByte('+')
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
