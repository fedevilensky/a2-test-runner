package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// cppDependencies asks the compiler for the headers actually selected by the
// preprocessor, including conditional and transitive includes. System headers
// are covered by the toolchain identity. Dependencies may be outside the student
// directory, but symlinks disable caching because their targets can change.
func cppDependencies(ctx context.Context, dir, source string, flags []string) ([]string, error) {
	_ = dir // Compilation resolves paths relative to the process working directory.
	// Compilation resolves the source relative to the runner's working directory.
	absSource, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	args := append([]string{}, flags...)
	args = append(args, "-MM", "-MT", "a2_dependencies", absSource)
	out, err := exec.CommandContext(ctx, "g++", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("discover C++ dependencies: %w", err)
	}
	paths, err := parseMakeDependencies(string(out))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil || resolved != abs {
			return nil, fmt.Errorf("symlink or inaccessible C++ dependency: %s", path)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("invalid C++ dependency: %s", path)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		// An absent optional header is not listed in -MM output. Its later
		// creation could change the result, so do not cache such exercises.
		if bytes.Contains(data, []byte("__has_include")) {
			return nil, fmt.Errorf("optional include cannot be tracked: %s", path)
		}
		seen[abs] = true
	}
	if !seen[absSource] {
		return nil, fmt.Errorf("c++ dependency output did not include source")
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

// GCC emits a Make rule; escaped whitespace, backslashes, hashes, dollar
// signs, and continuation lines must not be mistaken for path separators.
func parseMakeDependencies(output string) ([]string, error) {
	const prefix = "a2_dependencies:"
	if !strings.HasPrefix(output, prefix) {
		return nil, fmt.Errorf("invalid compiler dependency output")
	}
	var paths []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			paths = append(paths, word.String())
			word.Reset()
		}
	}
	output = output[len(prefix):]
	for i := 0; i < len(output); i++ {
		switch output[i] {
		case '\\':
			if i+1 >= len(output) {
				return nil, fmt.Errorf("invalid compiler dependency escape")
			}
			i++
			switch output[i] {
			case '\n':
			case ' ', '\t', '#':
				word.WriteByte(output[i])
			default:
				return nil, fmt.Errorf("unsupported compiler dependency escape")
			}
		case '$':
			if i+1 >= len(output) || output[i+1] != '$' {
				return nil, fmt.Errorf("invalid compiler dependency variable")
			}
			i++
			word.WriteByte('$')
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			word.WriteByte(output[i])
		}
	}
	flush()
	return paths, nil
}
