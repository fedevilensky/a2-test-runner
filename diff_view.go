package main

import (
	"regexp"
	"strconv"
	"strings"
)

// outputLine is rendered as text by html/template, which escapes student output.
type outputLine struct {
	Text    string
	Changed bool
}

var diffHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// diffOutputLines maps unified diff hunks back to the complete original outputs.
// When a diff cannot be parsed, callers can still show the original text.
func diffOutputLines(expected, actual, diff string) ([]outputLine, []outputLine, bool) {
	makeLines := func(s string) []outputLine {
		if s == "" {
			return nil
		}
		parts := strings.Split(s, "\n")
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		lines := make([]outputLine, len(parts))
		for i, part := range parts {
			lines[i].Text = strings.TrimSuffix(part, "\r")
		}
		return lines
	}
	left, right := makeLines(expected), makeLines(actual)
	oldLine, newLine, inHunk, foundChange := 0, 0, false, false
	for _, line := range strings.Split(diff, "\n") {
		if matches := diffHunkHeader.FindStringSubmatch(line); matches != nil {
			oldLine, _ = strconv.Atoi(matches[1])
			newLine, _ = strconv.Atoi(matches[2])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case ' ':
			oldLine++
			newLine++
		case '-':
			if oldLine < 1 || oldLine > len(left) {
				return nil, nil, false
			}
			left[oldLine-1].Changed = true
			oldLine++
			foundChange = true
		case '+':
			if newLine < 1 || newLine > len(right) {
				return nil, nil, false
			}
			right[newLine-1].Changed = true
			newLine++
			foundChange = true
		case '\\': // diff's "No newline at end of file" marker
		default:
			return nil, nil, false
		}
	}
	return left, right, foundChange
}
