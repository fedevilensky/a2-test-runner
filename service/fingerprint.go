package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fingerprintRunner identifies the running implementation for cache invalidation.
func fingerprintRunner() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func fingerprintExercise(
	ctx context.Context, dir string, n int, flags []string, timeout time.Duration,
	runnerIdentity string,
) string {
	if runnerIdentity == "" {
		return ""
	}
	for _, name := range []string{
		"CPATH", "CPLUS_INCLUDE_PATH", "C_INCLUDE_PATH", "LIBRARY_PATH", "LD_LIBRARY_PATH",
		"GCC_EXEC_PREFIX", "COMPILER_PATH", "CLASSPATH", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS",
		"_JAVA_OPTIONS",
	} {
		if os.Getenv(name) != "" {
			return ""
		}
	}
	cpp := filepath.Join(dir, fmt.Sprintf("ejercicio%d.cpp", n))
	java := filepath.Join(dir, fmt.Sprintf("Ejercicio%d.java", n))
	tools := []string{"g++"}
	if _, err := os.Stat(cpp); err != nil {
		if _, err = os.Stat(java); err != nil {
			return ""
		}
		tools = []string{"javac", "java"}
	} else {
		for _, f := range flags {
			switch {
			case strings.HasPrefix(f, "-std="), strings.HasPrefix(f, "-O"),
				len(f) > 2 && strings.HasPrefix(f, "-I"),
				len(f) > 2 && strings.HasPrefix(f, "-D"),
				len(f) > 2 && strings.HasPrefix(f, "-U"),
				f == "-Wall", f == "-Wextra", f == "-Werror", f == "-Wpedantic",
				f == "-g", f == "-pedantic", f == "-pedantic-errors":
			default:
				return ""
			}
		}
	}
	h := sha256.New()
	writeFingerprint(h, "a2-cache-v3", runnerIdentity, dir, fmt.Sprint(n), timeout.String())
	if len(tools) == 1 {
		writeFingerprint(h, flags...)
	}
	env := os.Environ()
	sort.Strings(env)
	writeFingerprint(h, env...)
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	writeFingerprint(h, cwd)
	for _, name := range tools {
		path, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return ""
		}
		info, err := os.Stat(path)
		if err != nil {
			return ""
		}
		out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
		if err != nil {
			return ""
		}
		writeFingerprint(h, path, info.ModTime().String(), fmt.Sprint(info.Size()), string(out))
	}
	var dependencies []string
	if len(tools) == 1 {
		dependencies, err = cppDependencies(ctx, dir, cpp, flags)
	} else {
		dependencies, err = javaDependencies(ctx, dir, java)
	}
	if err != nil || len(dependencies) == 0 {
		return ""
	}
	for _, path := range dependencies {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		// A conditional presence check can change without appearing in the
		// compiler's dependency list, so its results must not be reused.
		if len(tools) == 1 && strings.Contains(string(data), "__has_include") {
			return ""
		}
		writeFingerprint(h, "file", path, fmt.Sprint(info.Mode().Perm()), string(data))
	}

	return hex.EncodeToString(h.Sum(nil))
}

func fingerprintCase(base string, tc testCase) string {
	if base == "" {
		return ""
	}
	h := sha256.New()
	writeFingerprint(h, base, tc.name)
	for _, p := range []string{tc.input, tc.expected} {
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return ""
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		writeFingerprint(h, string(data))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFingerprint(h hash.Hash, values ...string) {
	for _, v := range values {
		_, _ = fmt.Fprintf(h, "%d:", len(v))
		_, _ = io.WriteString(h, v)
	}
}
