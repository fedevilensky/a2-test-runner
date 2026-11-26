// Package service contains the test execution and discovery logic shared by the CLI and server.
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
)

type Config struct {
	TestsFolder string
	Students    []models.StudentConfig
	Timeout     time.Duration
	Selection   models.ResultSelection
	Build       models.BuildSettings
	OnProgress  func(models.RunProgress) `json:"-"`
	Incremental bool
	Cached      map[string]models.StudentReportData `json:"-"`
}

// EffectiveFlags resolves student overrides and returns compiler arguments.
func EffectiveFlags(global, student models.BuildSettings) ([]string, error) {
	standard := global.Standard
	if student.Standard != "" {
		standard = student.Standard
	}
	if standard == "" {
		standard = "c++11"
	}
	standard = strings.TrimPrefix(standard, "-std=")
	allowedStandards := map[string]bool{"c++11": true, "c++14": true, "c++17": true, "c++20": true, "c++23": true, "gnu++11": true, "gnu++14": true, "gnu++17": true, "gnu++20": true, "gnu++23": true}
	if !allowedStandards[standard] {
		return nil, fmt.Errorf("unsupported C++ standard %q", standard)
	}
	optimization := global.Optimization
	if student.Optimization != "" {
		optimization = student.Optimization
	}
	if optimization == "default" {
		optimization = ""
	}
	if optimization != "" && !strings.HasPrefix(optimization, "-O") {
		optimization = "-O" + optimization
	}
	switch optimization {
	case "", "-O0", "-O1", "-O2", "-O3", "-Os", "-Og", "-Ofast":
	default:
		return nil, fmt.Errorf("unsupported optimization %q", optimization)
	}
	extra := global.Extra
	if student.Extra != nil {
		extra = student.Extra
	}
	flags := []string{"-std=" + standard}
	if optimization != "" {
		flags = append(flags, optimization)
	}
	for _, flag := range extra {
		if !strings.HasPrefix(flag, "-") || (flag == "-" || flag == "--") || strings.ContainsRune(flag, '\x00') || strings.HasPrefix(flag, "-std=") || strings.HasPrefix(flag, "-O") || strings.HasPrefix(flag, "-o") || strings.HasPrefix(flag, "--output") || flag == "-c" || flag == "-S" || flag == "-E" || strings.HasPrefix(flag, "-x") {
			return nil, fmt.Errorf("invalid extra compiler argument %q", flag)
		}
		flags = append(flags, flag)
	}
	return flags, nil
}

// DiscoverStudents resolves a base directory and glob, returning directories in stable order.
func DiscoverStudents(base, pattern string) ([]string, error) {
	if strings.TrimSpace(pattern) == "" {
		pattern = "*"
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(base, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid student pattern: %w", err)
	}
	var out []string
	for _, p := range matches {
		info, e := os.Stat(p)
		if e == nil && info.IsDir() {
			abs, e := filepath.Abs(p)
			if e != nil {
				return nil, e
			}
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out, nil
}

func DiscoverExercises(root string) (map[int]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read tests folder: %w", err)
	}
	re := regexp.MustCompile(`\d+`)
	out := map[int]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		nstr := re.FindString(entry.Name())
		if nstr == "" {
			continue
		}
		n, e := strconv.Atoi(nstr)
		if e == nil {
			if _, exists := out[n]; exists {
				return nil, fmt.Errorf("multiple folders for exercise %d", n)
			}
			out[n] = filepath.Join(root, entry.Name())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no exercise folders found in %s", root)
	}
	return out, nil
}

type testCase struct {
	name, input, expected string
	size                  int
}

func collectCases(dir string) ([]testCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []testCase
	re := regexp.MustCompile(`\d+`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".in.txt") {
			continue
		}
		n, _ := strconv.Atoi(re.FindString(e.Name()))
		prefix := strings.TrimSuffix(e.Name(), ".in.txt")
		out = append(out, testCase{e.Name(), filepath.Join(dir, e.Name()), filepath.Join(dir, prefix+".out.txt"), n})
	}
	return out, nil
}

// DiscoverTestSuite returns exercises and the runnable input files in each folder.
func DiscoverTestSuite(root string) ([]models.AvailableExercise, error) {
	folders, err := DiscoverExercises(root)
	if err != nil {
		return nil, err
	}
	numbers := make([]int, 0, len(folders))
	for number := range folders {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	exercises := make([]models.AvailableExercise, 0, len(numbers))
	for _, number := range numbers {
		cases, err := collectCases(folders[number])
		if err != nil {
			return nil, fmt.Errorf("exercise %d: %w", number, err)
		}
		exercise := models.AvailableExercise{Number: number, Folder: filepath.Base(folders[number]), Cases: make([]string, 0, len(cases))}
		for _, test := range cases {
			exercise.Cases = append(exercise.Cases, test.name)
		}
		exercises = append(exercises, exercise)
	}
	return exercises, nil
}

// Run runs selected cases for each student.
func Run(ctx context.Context, cfg Config, progress func(string)) ([]models.StudentReportData, error) {
	var reports []models.StudentReportData
	err := RunWithCompleted(ctx, cfg, progress, func(report models.StudentReportData) error {
		reports = append(reports, report)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reports, nil
}

// RunWithCompleted calls completed synchronously as each student finishes. Returning
// an error stops the run, allowing callers to persist each report before proceeding.
func RunWithCompleted(ctx context.Context, cfg Config, progress func(string), completed func(models.StudentReportData) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if len(cfg.Students) == 0 {
		return errors.New("no students selected")
	}
	folders, err := DiscoverExercises(cfg.TestsFolder)
	if err != nil {
		return err
	}
	selected := cfg.Selection.Exercises
	numbers := make([]int, 0, len(folders))
	for n := range folders {
		if len(selected) == 0 {
			numbers = append(numbers, n)
		} else if _, ok := selected[n]; ok {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	if len(numbers) == 0 {
		return errors.New("no selected exercises found")
	}
	for n := range selected {
		if _, ok := folders[n]; !ok {
			return fmt.Errorf("exercise %d not found", n)
		}
	}
	tests := map[int][]testCase{}
	totalCases := 0
	for _, n := range numbers {
		cases, e := collectCases(folders[n])
		if e != nil {
			return e
		}
		wanted := selected[n]
		if len(wanted) > 0 {
			set := map[string]bool{}
			for _, name := range wanted {
				set[name] = true
			}
			filtered := cases[:0]
			for _, tc := range cases {
				if set[tc.name] {
					filtered = append(filtered, tc)
					delete(set, tc.name)
				}
			}
			if len(set) > 0 {
				return fmt.Errorf("some selected cases in exercise %d were not found", n)
			}
			cases = filtered
		}
		if len(selected) > 0 && len(cases) == 0 {
			return fmt.Errorf("no test cases found in selected exercise %d", n)
		}
		tests[n] = cases
		totalCases += len(cases)
	}
	if totalCases == 0 {
		return errors.New("no test cases found in selected exercises")
	}
	type preparedStudent struct {
		path, name string
		flags      []string
	}
	seen := map[string]bool{}
	students := make([]preparedStudent, 0, len(cfg.Students))
	for _, student := range cfg.Students {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := filepath.Abs(student.Path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("student %s is not a folder", path)
		}
		name := filepath.Base(path)
		if seen[name] {
			return fmt.Errorf("duplicate student folder name %q", name)
		}
		seen[name] = true
		flags, err := EffectiveFlags(cfg.Build, student.Build)
		if err != nil {
			return fmt.Errorf("student %s: %w", name, err)
		}
		students = append(students, preparedStudent{path: path, name: name, flags: flags})
	}
	runnerIdentity := fingerprintRunner()
	work := models.RunProgress{TotalStudents: len(students)}
	for _, cases := range tests {
		if len(cases) > 0 {
			work.TotalUnits += (1 + len(cases)) * len(students)
		}
	}
	emit := func(activity string) {
		work.Activity = activity
		if cfg.OnProgress != nil {
			cfg.OnProgress(work)
		}
	}
	emit("Starting run")
	for _, student := range students {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, name, flags := student.path, student.name, student.flags
		report := models.StudentReportData{ID: name, StudentName: name, StudentPath: path, StartedAt: time.Now()}
		for _, n := range numbers {
			if len(tests[n]) == 0 {
				continue
			}
			baseFingerprint := fingerprintExercise(ctx, path, n, flags, cfg.Timeout, runnerIdentity)
			fingerprints := map[string]string{}
			cached := map[string]models.TestCaseResult{}
			for _, tc := range tests[n] {
				fingerprints[tc.name] = fingerprintCase(baseFingerprint, tc)
			}
			if cfg.Incremental {
				for _, oldGroup := range cfg.Cached[name].Groups {
					if oldGroup.Exercise != n {
						continue
					}
					for _, old := range oldGroup.Results {
						if old.Passed && old.Fingerprint != "" && old.Fingerprint == fingerprints[old.InputFile] {
							cached[old.InputFile] = old
						}
					}
				}
			}
			activity := fmt.Sprintf("%s: compiling exercise %d", name, n)
			if len(cached) == len(tests[n]) {
				activity = fmt.Sprintf("%s: exercise %d cached", name, n)
			}
			emit(activity)
			if progress != nil {
				progress(activity)
			}
			var runner []string
			var cleanup func()
			var diagnostic, kind string
			var usedFlags []string
			if len(cached) < len(tests[n]) {
				runner, cleanup, diagnostic, kind, usedFlags = compile(ctx, path, n, flags)
			}
			if err := ctx.Err(); err != nil {
				if cleanup != nil {
					cleanup()
				}
				return err
			}
			work.CompletedUnits++
			emit(activity)
			group := models.ExerciseGroup{Exercise: n}
			for _, tc := range tests[n] {
				if err := ctx.Err(); err != nil {
					if cleanup != nil {
						cleanup()
					}
					return err
				}
				activity = fmt.Sprintf("%s: exercise %d, %s", name, n, tc.name)
				emit(activity)
				if progress != nil {
					progress(activity)
				}
				result := models.TestCaseResult{InputFile: tc.name, InputSize: tc.size, ExecutedAt: time.Now(), BuildFlags: append([]string(nil), usedFlags...)}
				if previous, ok := cached[tc.name]; ok {
					result = previous
					result.Cached = true
					work.CachedCases++
				} else if kind != "" {
					work.ExecutedCases++
					result.FailureType = kind
					result.Message = diagnostic
				} else {
					work.ExecutedCases++
					execute(ctx, runner, tc, cfg.Timeout, &result)
					if result.Passed {
						result.Fingerprint = fingerprints[tc.name]
					}
				}
				if result.Passed {
					group.Passed++
					report.Passed++
				} else {
					group.Failed++
					report.Failed++
					switch result.FailureType {
					case "compilation_error":
						report.CompileFails++
					case "runtime_error":
						report.RuntimeFails++
					case "diff_error":
						report.DiffFails++
					}
				}
				report.Total++
				if result.InputSize > 0 && result.Duration > 0 {
					group.HasChartData = true
				}
				group.Results = append(group.Results, result)
				if err := ctx.Err(); err != nil {
					if cleanup != nil {
						cleanup()
					}
					return err
				}
				work.CompletedUnits++
				emit(activity)
			}
			if cleanup != nil {
				cleanup()
			}
			// Only store fingerprints if source and test data stayed unchanged throughout execution.
			after := fingerprintExercise(ctx, path, n, flags, cfg.Timeout, runnerIdentity)
			for i := range group.Results {
				result := &group.Results[i]
				var tc testCase
				for _, candidate := range tests[n] {
					if candidate.name == result.InputFile {
						tc = candidate
						break
					}
				}
				if after == "" || after != baseFingerprint || fingerprintCase(after, tc) != result.Fingerprint {
					if result.Cached {
						return fmt.Errorf("student %s exercise %d changed during execution; retry the run", name, n)
					}
					result.Fingerprint = ""
				}
			}
			report.Groups = append(report.Groups, group)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		report.FinishedAt = time.Now()
		emit(fmt.Sprintf("%s: saving report", name))
		if completed != nil {
			if err := completed(report); err != nil {
				return err
			}
		}
		work.CompletedStudents++
		emit(fmt.Sprintf("%s: report available", name))
	}
	return nil
}

func compile(ctx context.Context, dir string, n int, flags []string) ([]string, func(), string, string, []string) {
	cpp := filepath.Join(dir, fmt.Sprintf("ejercicio%d.cpp", n))
	java := filepath.Join(dir, fmt.Sprintf("Ejercicio%d.java", n))
	source := cpp
	lang := "cpp"
	if _, e := os.Stat(cpp); e != nil {
		source = java
		lang = "java"
		if _, e = os.Stat(java); e != nil {
			return nil, nil, fmt.Sprintf("missing source file: expected %s or %s", filepath.Base(java), filepath.Base(cpp)), "source_missing", nil
		}
	}
	var usedFlags []string
	if lang == "cpp" {
		usedFlags = flags
	}
	tmp, e := os.MkdirTemp("", "test-runner-build-*")
	if e != nil {
		return nil, nil, e.Error(), "compilation_error", usedFlags
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	var cmd *exec.Cmd
	var runner []string
	if lang == "cpp" {
		binary := filepath.Join(tmp, "program")
		args := append(append([]string{}, flags...), source, "-o", binary)
		cmd = exec.CommandContext(ctx, "g++", args...)
		runner = []string{binary}
	} else {
		cmd = exec.CommandContext(ctx, "javac", "-d", tmp, source)
		cmd.Dir = dir
		runner = []string{"java", "-cp", tmp, fmt.Sprintf("Ejercicio%d", n)}
	}
	output, e := cmd.CombinedOutput()
	if e != nil {
		cleanup()
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = e.Error()
		}
		return nil, nil, message, "compilation_error", usedFlags
	}
	return runner, cleanup, "", "", usedFlags
}

func execute(parent context.Context, runner []string, tc testCase, timeout time.Duration, r *models.TestCaseResult) {
	input, e := os.Open(tc.input)
	if e != nil {
		r.FailureType = "runtime_error"
		r.Message = e.Error()
		return
	}
	defer func() { _ = input.Close() }()
	expected, e := os.ReadFile(tc.expected)
	if e != nil {
		r.FailureType = "diff_error"
		r.Message = e.Error()
		return
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, runner[0], runner[1:]...)
	cmd.Stdin = input
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	start := time.Now()
	e = cmd.Run()
	r.Duration = time.Since(start)
	if e != nil {
		r.FailureType = "runtime_error"
		r.Message = strings.TrimSpace(stderr.String())
		if ctx.Err() == context.DeadlineExceeded {
			r.Message = fmt.Sprintf("execution timed out after %s", timeout)
		} else if r.Message == "" {
			r.Message = e.Error()
		}
		r.Actual = out.String()
		return
	}
	if normalize(string(expected)) != normalize(out.String()) {
		r.FailureType = "diff_error"
		r.Message = "output differs from expected"
		r.Expected = string(expected)
		r.Actual = out.String()
		return
	}
	r.Passed = true
}

func normalize(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// SelectExercises parses comma-separated exercise numbers and inclusive ranges.
// An empty input selects all available exercises.
func SelectExercises(input string, available map[int]string) (models.ResultSelection, error) {
	selection := models.ResultSelection{}
	if strings.TrimSpace(input) == "" {
		return selection, nil
	}
	selection.Exercises = map[int][]string{}
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return selection, fmt.Errorf("invalid range %q", part)
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil || start < 0 {
			return selection, fmt.Errorf("invalid exercise %q", part)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || end < start {
				return selection, fmt.Errorf("invalid range %q", part)
			}
		}
		// Iterate discovered keys instead of untrusted range bounds.
		for n := range available {
			if n >= start && n <= end {
				selection.Exercises[n] = nil
			}
		}
	}
	if len(selection.Exercises) == 0 {
		return selection, errors.New("none of the selected exercises are present")
	}
	return selection, nil
}
