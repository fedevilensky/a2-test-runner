package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// javaDependencies asks javac which local source and class files it actually
// loads while compiling the exercise. A failed or unrecognized dependency scan
// must disable caching, not fall back to an incomplete list.
func javaDependencies(ctx context.Context, dir, source string) ([]string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || resolvedRoot != root {
		return nil, fmt.Errorf("student directory is not a real directory")
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	outDir, err := os.MkdirTemp("", "test-runner-java-deps-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(outDir) }()
	cmd := exec.CommandContext(
		ctx, "javac", "-J-Duser.language=en", "-J-Duser.country=US", "-verbose", "-d", outDir, source,
	)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("javac dependency scan: %w", err)
	}
	deps := map[string]bool{}
	add := func(path string) error {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		path = filepath.Clean(path)
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("external Java dependency %q", path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return fmt.Errorf("unresolvable or symlinked Java dependency %q", path)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("invalid Java dependency %q", path)
		}
		deps[path] = true
		return nil
	}
	if err := add(source); err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[loading ") {
			continue
		}
		if !strings.HasSuffix(line, "]") {
			return nil, fmt.Errorf("unrecognized javac dependency: %s", line)
		}
		path := strings.TrimSuffix(strings.TrimPrefix(line, "[loading "), "]")
		// Platform classes are covered by the javac/java identity in the
		// enclosing fingerprint, rather than treated as student files.
		if strings.HasPrefix(path, "/modules/") {
			continue
		}
		if !strings.HasSuffix(path, ".java") && !strings.HasSuffix(path, ".class") {
			return nil, fmt.Errorf("unrecognized javac dependency: %s", path)
		}
		if err := add(path); err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(deps))
	for path := range deps {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
