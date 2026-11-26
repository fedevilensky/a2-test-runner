package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fedevilensky/a2-test-runner/models"
)

var diffHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// diffOutputLines maps unified diff hunks back to the complete original outputs.
// When a diff cannot be parsed, callers can still show the original text.
func diffOutputLines(expected, actual, diff string) ([]models.OutputLine, []models.OutputLine, bool) {
	makeLines := func(s string) []models.OutputLine {
		if s == "" {
			return nil
		}
		parts := strings.Split(s, "\n")
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		lines := make([]models.OutputLine, len(parts))
		for i, part := range parts {
			lines[i].Text = strings.TrimSuffix(part, "\r")
		}
		return lines
	}
	left, right := makeLines(expected), makeLines(actual)
	oldLine, newLine, inHunk, foundChange := 0, 0, false, false
	for line := range strings.SplitSeq(diff, "\n") {
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

// unifiedDiff uses the system diff utility when available. Expected and actual
// output remain in the report even when the utility cannot be run.
func unifiedDiff(expected, actual string) string {
	dir, err := os.MkdirTemp("", "a2-test-runner-diff-*")
	if err != nil {
		return ""
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "expected"), []byte(expected), 0o600); err != nil {
		return ""
	}
	if err := os.WriteFile(filepath.Join(dir, "actual"), []byte(actual), 0o600); err != nil {
		return ""
	}
	cmd := exec.Command("diff", "-u", "expected", "actual")
	cmd.Dir = dir
	out, err := cmd.Output()
	// diff exits with status 1 when differences are found.
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return ""
		}
	}
	return string(out)
}

func EnrichDiff(item *models.TestCaseResult) {
	if item.FailureType != "diff_error" || (item.Expected == "" && item.Actual == "") {
		return
	}
	item.ExpectedLines, item.ActualLines, item.HasDiffLines = diffOutputLines(item.Expected, item.Actual, unifiedDiff(item.Expected, item.Actual))
}
