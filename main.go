package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli"
)

const (
	reportRootFolder      = "test-results"
	summarySubfolder      = "summary"
	maxDiagnosticTextSize = 8000
)

type failureType string

const (
	failureNone          failureType = ""
	failureCompile       failureType = "compilation_error"
	failureRuntime       failureType = "runtime_error"
	failureDiff          failureType = "diff_error"
	failureSourceMissing failureType = "source_missing"
)

var errSourceMissing = errors.New("source missing")

type config struct {
	testsFolder          string
	testNumbersInput     string
	studentFolder        string
	studentFolderPattern string
	timeout              time.Duration
}

type testCase struct {
	name         string
	inputPath    string
	expectedPath string
	inputSize    int
}

type testCaseResult struct {
	Exercise    int
	CaseName    string
	InputFile   string
	InputSize   int
	Duration    time.Duration
	Passed      bool
	FailureType failureType
	Message     string
	Expected    string
	Actual      string
}

type studentReport struct {
	StudentName  string
	StudentPath  string
	StartedAt    time.Time
	FinishedAt   time.Time
	Results      []testCaseResult
	Total        int
	Passed       int
	Failed       int
	CompileFails int
	RuntimeFails int
	DiffFails    int
	MissingFails int
}

type summaryRow struct {
	Exercise     int
	Total        int
	AllPass      int
	Partial      int
	AllFail      int
	CompileFail  int
	NotSubmitted int
}

type summaryReport struct {
	GeneratedAt time.Time
	Rows        []summaryRow
}

type spaData struct {
	Students []studentReportData
	Summary  summaryReport
}

type exerciseGroup struct {
	Exercise     int
	Results      []testCaseResult
	Passed       int
	Failed       int
	HasChartData bool
}

type studentReportData struct {
	studentReport
	Groups      []exerciseGroup
	IndexPath   string
	SummaryPath string
	PrevName    string
	PrevPath    string
	NextName    string
	NextPath    string
}

type executable struct {
	Language string
	Runner   []string
	Cleanup  func() error
}

func main() {
	app := cli.NewApp()
	app.Name = "test-runner"
	app.Usage = "Run programming exercise tests and generate HTML reports"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "tests-folder, t",
			Usage: "Folder containing tests",
			Value: "./tests",
		},
		cli.StringFlag{
			Name:  "test-numbers, n",
			Usage: "Comma-separated test ranges (e.g. 1-3,5,7-9)",
		},
		cli.StringFlag{
			Name:  "student-folder, s",
			Usage: "Folder containing student source code",
		},
		cli.StringFlag{
			Name:  "student-folder-pattern, p",
			Usage: "Glob pattern for student folders",
		},
		cli.IntFlag{
			Name:  "timeout, T",
			Usage: "Per-test execution timeout in seconds",
			Value: 30,
		},
	}

	app.Action = func(c *cli.Context) error {
		cfg, err := parseConfig(c)
		if err != nil {
			return err
		}
		return run(cfg)
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parseConfig(c *cli.Context) (config, error) {
	cfg := config{
		testsFolder:          c.String("tests-folder"),
		testNumbersInput:     c.String("test-numbers"),
		studentFolder:        c.String("student-folder"),
		studentFolderPattern: c.String("student-folder-pattern"),
		timeout:              time.Duration(c.Int("timeout")) * time.Second,
	}

	if strings.TrimSpace(cfg.testsFolder) == "" {
		return cfg, errors.New("--tests-folder/-t is required")
	}

	if strings.TrimSpace(cfg.studentFolderPattern) == "" && strings.TrimSpace(cfg.studentFolder) == "" {
		return cfg, errors.New("--student-folder/-s is required when --student-folder-pattern/-p is not provided")
	}

	absTests, err := filepath.Abs(cfg.testsFolder)
	if err != nil {
		return cfg, fmt.Errorf("invalid tests folder: %w", err)
	}
	cfg.testsFolder = absTests

	if cfg.studentFolder != "" {
		absStudent, err := filepath.Abs(cfg.studentFolder)
		if err != nil {
			return cfg, fmt.Errorf("invalid student folder: %w", err)
		}
		cfg.studentFolder = absStudent
	}

	return cfg, nil
}

func run(cfg config) error {
	studentFolders, err := resolveStudentFolders(cfg)
	if err != nil {
		return err
	}

	exerciseFolders, err := discoverExerciseFolders(cfg.testsFolder)
	if err != nil {
		return err
	}

	selectedNumbers, err := selectTestNumbers(cfg.testNumbersInput, exerciseFolders)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(reportRootFolder, 0o755); err != nil {
		return fmt.Errorf("error creating report folder: %w", err)
	}

	allReports := make([]studentReport, 0, len(studentFolders))
	for _, studentFolder := range studentFolders {
		report, err := runForStudent(studentFolder, cfg.testsFolder, exerciseFolders, selectedNumbers, cfg.timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error while running student folder %q: %v\n", studentFolder, err)
			continue
		}
		allReports = append(allReports, report)
	}

	if len(allReports) == 0 {
		return errors.New("no reports generated")
	}

	summary := buildSummary(allReports)

	studentDatas := make([]studentReportData, 0, len(allReports))
	for i, report := range allReports {
		data := studentReportData{
			studentReport: report,
			Groups:        groupResultsByExercise(report.Results),
			IndexPath:     "../index.html",
			SummaryPath:   "../summary/index.html",
		}
		if i > 0 {
			data.PrevName = allReports[i-1].StudentName
			data.PrevPath = fmt.Sprintf("../%s/report.html", sanitizeName(allReports[i-1].StudentName))
		}
		if i < len(allReports)-1 {
			data.NextName = allReports[i+1].StudentName
			data.NextPath = fmt.Sprintf("../%s/report.html", sanitizeName(allReports[i+1].StudentName))
		}
		studentDatas = append(studentDatas, data)
		if err := writeStudentReport(data); err != nil {
			fmt.Fprintf(os.Stderr, "error writing student report for %q: %v\n", report.StudentName, err)
		}
	}

	if err := writeIndexReport(studentDatas, summary); err != nil {
		fmt.Fprintf(os.Stderr, "error writing index report: %v\n", err)
	}

	if err := writeSummaryReport(summary); err != nil {
		return fmt.Errorf("error writing summary report: %w", err)
	}

	printConsoleSummary(allReports, summary)
	return nil
}

func resolveStudentFolders(cfg config) ([]string, error) {
	if strings.TrimSpace(cfg.studentFolderPattern) != "" {
		matches, err := filepath.Glob(cfg.studentFolderPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid student folder pattern: %w", err)
		}
		folders := make([]string, 0, len(matches))
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			if info.IsDir() {
				abs, err := filepath.Abs(match)
				if err != nil {
					continue
				}
				folders = append(folders, abs)
			}
		}
		sort.Strings(folders)
		if len(folders) == 0 {
			return nil, fmt.Errorf("no student folders match pattern %q", cfg.studentFolderPattern)
		}
		return folders, nil
	}

	info, err := os.Stat(cfg.studentFolder)
	if err != nil {
		return nil, fmt.Errorf("cannot read student folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("student folder is not a directory: %s", cfg.studentFolder)
	}
	return []string{cfg.studentFolder}, nil
}

func discoverExerciseFolders(testsFolder string) (map[int]string, error) {
	entries, err := os.ReadDir(testsFolder)
	if err != nil {
		return nil, fmt.Errorf("cannot read tests folder: %w", err)
	}

	numberRE := regexp.MustCompile(`\d+`)
	folders := make(map[int]string)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		match := numberRE.FindString(name)
		if match == "" {
			continue
		}
		n, err := strconv.Atoi(match)
		if err != nil {
			continue
		}
		folders[n] = filepath.Join(testsFolder, name)
	}

	if len(folders) == 0 {
		return nil, fmt.Errorf("no exercise folders with numeric suffix found inside %s", testsFolder)
	}

	return folders, nil
}

func selectTestNumbers(input string, exerciseFolders map[int]string) ([]int, error) {
	if strings.TrimSpace(input) == "" {
		numbers := make([]int, 0, len(exerciseFolders))
		for n := range exerciseFolders {
			numbers = append(numbers, n)
		}
		sort.Ints(numbers)
		return numbers, nil
	}

	selected := map[int]struct{}{}
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range start in %q", part)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range end in %q", part)
			}
			if start > end {
				return nil, fmt.Errorf("range start greater than end in %q", part)
			}
			for i := start; i <= end; i++ {
				selected[i] = struct{}{}
			}
			continue
		}

		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid test number %q", part)
		}
		selected[n] = struct{}{}
	}

	out := make([]int, 0, len(selected))
	for n := range selected {
		if _, ok := exerciseFolders[n]; ok {
			out = append(out, n)
		}
	}
	sort.Ints(out)

	if len(out) == 0 {
		return nil, errors.New("none of the selected test numbers are present in tests folder")
	}

	return out, nil
}

func runForStudent(studentFolder string, testsFolder string, exerciseFolders map[int]string, selected []int, timeout time.Duration) (studentReport, error) {
	_ = testsFolder
	report := studentReport{
		StudentName: filepath.Base(studentFolder),
		StudentPath: studentFolder,
		StartedAt:   time.Now(),
		Results:     make([]testCaseResult, 0),
	}

	if err := removeClassFiles(studentFolder); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove .class files in %q: %v\n", studentFolder, err)
	}

	for _, exercise := range selected {
		testFolder := exerciseFolders[exercise]
		testCases, err := collectTestCases(testFolder)
		if err != nil {
			return report, fmt.Errorf("exercise %d: %w", exercise, err)
		}
		if len(testCases) == 0 {
			continue
		}

		execInfo, compileErr, compileOutput := prepareExecutable(studentFolder, exercise)
		if execInfo.Cleanup != nil {
			defer execInfo.Cleanup()
		}

		if compileErr != nil {
			ft := failureCompile
			if errors.Is(compileErr, errSourceMissing) {
				ft = failureSourceMissing
			}
			for _, tc := range testCases {
				report.Results = append(report.Results, testCaseResult{
					Exercise:    exercise,
					CaseName:    tc.name,
					InputFile:   filepath.Base(tc.inputPath),
					InputSize:   tc.inputSize,
					Duration:    0,
					Passed:      false,
					FailureType: ft,
					Message:     trimDiagnostic(compileOutput),
				})
			}
			continue
		}

		for _, tc := range testCases {
			result := executeTestCase(execInfo, exercise, tc, timeout)
			report.Results = append(report.Results, result)
		}
	}

	report.FinishedAt = time.Now()
	computeStudentTotals(&report)
	return report, nil
}

func collectTestCases(testFolder string) ([]testCase, error) {
	entries, err := os.ReadDir(testFolder)
	if err != nil {
		return nil, fmt.Errorf("cannot read test folder: %w", err)
	}

	cases := make([]testCase, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".in.txt") {
			continue
		}
		inputPath := filepath.Join(testFolder, name)
		expectedPath := guessExpectedPath(inputPath)
		cases = append(cases, testCase{
			name:         strings.TrimSuffix(name, ".in.txt"),
			inputPath:    inputPath,
			expectedPath: expectedPath,
			inputSize:    extractInputSize(inputPath),
		})
	}

	sort.Slice(cases, func(i, j int) bool {
		return cases[i].inputPath < cases[j].inputPath
	})
	return cases, nil
}

func guessExpectedPath(inputPath string) string {
	if strings.HasSuffix(inputPath, ".in.txt") {
		return strings.TrimSuffix(inputPath, ".in.txt") + ".out.txt"
	}
	return strings.Replace(inputPath, ".in.", ".out.", 1)
}

func extractInputSize(inputPath string) int {
	m := regexp.MustCompile(`\d+`).FindString(filepath.Base(inputPath))
	if m == "" {
		return 0
	}
	n, err := strconv.Atoi(m)
	if err != nil {
		return 0
	}
	return n
}

func groupResultsByExercise(results []testCaseResult) []exerciseGroup {
	order := make([]int, 0)
	groups := make(map[int]*exerciseGroup)
	for _, r := range results {
		if _, ok := groups[r.Exercise]; !ok {
			order = append(order, r.Exercise)
			groups[r.Exercise] = &exerciseGroup{Exercise: r.Exercise}
		}
		g := groups[r.Exercise]
		g.Results = append(g.Results, r)
		if r.Passed {
			g.Passed++
		} else {
			g.Failed++
		}
		if r.InputSize > 0 && r.FailureType != failureCompile && r.FailureType != failureSourceMissing {
			g.HasChartData = true
		}
	}
	sort.Ints(order)
	out := make([]exerciseGroup, 0, len(order))
	for _, ex := range order {
		out = append(out, *groups[ex])
	}
	return out
}

func prepareExecutable(studentFolder string, exercise int) (executable, error, string) {
	javaFile := filepath.Join(studentFolder, fmt.Sprintf("Ejercicio%d.java", exercise))
	cppFile := filepath.Join(studentFolder, fmt.Sprintf("ejercicio%d.cpp", exercise))

	javaExists := fileExists(javaFile)
	cppExists := fileExists(cppFile)

	if !javaExists && !cppExists {
		msg := fmt.Sprintf("missing source file: expected %s or %s", filepath.Base(javaFile), filepath.Base(cppFile))
		return executable{}, fmt.Errorf("%w: %s", errSourceMissing, msg), msg
	}

	if cppExists {
		tmpDir, err := os.MkdirTemp("", "test-runner-cpp-*")
		if err != nil {
			return executable{}, err, ""
		}
		binary := filepath.Join(tmpDir, fmt.Sprintf("ejercicio%d.out", exercise))
		cmd := exec.Command("g++", cppFile, "-o", binary)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err = cmd.Run()
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return executable{}, err, out.String()
		}
		return executable{
			Language: "cpp",
			Runner:   []string{binary},
			Cleanup: func() error {
				return os.RemoveAll(tmpDir)
			},
		}, nil, out.String()
	}

	tmpDir, err := os.MkdirTemp("", "test-runner-java-*")
	if err != nil {
		return executable{}, err, ""
	}

	cmd := exec.Command("javac", "-d", tmpDir, javaFile)
	cmd.Dir = studentFolder

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return executable{}, err, out.String()
	}

	return executable{
		Language: "java",
		Runner:   []string{"java", "-cp", tmpDir, fmt.Sprintf("Ejercicio%d", exercise)},
		Cleanup: func() error {
			return os.RemoveAll(tmpDir)
		},
	}, nil, out.String()
}

func executeTestCase(execInfo executable, exercise int, tc testCase, timeout time.Duration) testCaseResult {
	result := testCaseResult{
		Exercise:  exercise,
		CaseName:  tc.name,
		InputFile: filepath.Base(tc.inputPath),
		InputSize: tc.inputSize,
	}

	inputFile, err := os.Open(tc.inputPath)
	if err != nil {
		result.Passed = false
		result.FailureType = failureRuntime
		result.Message = fmt.Sprintf("cannot open input file: %v", err)
		return result
	}
	defer inputFile.Close()

	expectedBytes, err := os.ReadFile(tc.expectedPath)
	if err != nil {
		result.Passed = false
		result.FailureType = failureDiff
		result.Message = fmt.Sprintf("cannot read expected output file: %v", err)
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command := execInfo.Runner[0]
	args := execInfo.Runner[1:]
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = inputFile

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	result.Duration = time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		result.Passed = false
		result.FailureType = failureRuntime
		result.Message = fmt.Sprintf("execution timed out after %s", timeout)
		result.Actual = trimDiagnostic(stdout.String() + "\n" + stderr.String())
		return result
	}

	if err != nil {
		result.Passed = false
		result.FailureType = failureRuntime
		result.Message = trimDiagnostic(stderr.String())
		if result.Message == "" {
			result.Message = err.Error()
		}
		result.Actual = trimDiagnostic(stdout.String())
		return result
	}

	expected := string(expectedBytes)
	actual := stdout.String()
	if !outputsEqual(expected, actual) {
		result.Passed = false
		result.FailureType = failureDiff
		result.Message = "output differs from expected"
		result.Expected = trimDiagnostic(expected)
		result.Actual = trimDiagnostic(actual)
		return result
	}

	result.Passed = true
	result.FailureType = failureNone
	return result
}

func outputsEqual(expected string, actual string) bool {
	return normalizeOutput(expected) == normalizeOutput(actual)
}

func normalizeOutput(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmedRight := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmedRight) == "" {
			continue
		}
		normalized = append(normalized, trimmedRight)
	}
	return strings.Join(normalized, "\n")
}

func computeStudentTotals(report *studentReport) {
	report.Total = len(report.Results)
	for _, result := range report.Results {
		if result.Passed {
			report.Passed++
			continue
		}
		report.Failed++
		switch result.FailureType {
		case failureCompile:
			report.CompileFails++
		case failureRuntime:
			report.RuntimeFails++
		case failureDiff:
			report.DiffFails++
		case failureSourceMissing:
			report.MissingFails++
		}
	}
}

func buildSummary(reports []studentReport) summaryReport {
	rows := map[int]*summaryRow{}

	for _, report := range reports {
		byExercise := map[int][]testCaseResult{}
		for _, r := range report.Results {
			byExercise[r.Exercise] = append(byExercise[r.Exercise], r)
		}
		for exercise, results := range byExercise {
			if _, ok := rows[exercise]; !ok {
				rows[exercise] = &summaryRow{Exercise: exercise}
			}
			row := rows[exercise]
			row.Total++

			notSubmitted := false
			compileFail := false
			passed := 0
			for _, r := range results {
				switch r.FailureType {
				case failureSourceMissing:
					notSubmitted = true
				case failureCompile:
					compileFail = true
				}
				if r.Passed {
					passed++
				}
			}

			switch {
			case notSubmitted:
				row.NotSubmitted++
			case compileFail:
				row.CompileFail++
			case passed == len(results):
				row.AllPass++
			case passed == 0:
				row.AllFail++
			default:
				row.Partial++
			}
		}
	}

	sortedRows := make([]summaryRow, 0, len(rows))
	for _, row := range rows {
		sortedRows = append(sortedRows, *row)
	}
	sort.Slice(sortedRows, func(i, j int) bool {
		return sortedRows[i].Exercise < sortedRows[j].Exercise
	})

	return summaryReport{GeneratedAt: time.Now(), Rows: sortedRows}
}

func writeStudentReport(data studentReportData) error {
	studentDir := filepath.Join(reportRootFolder, sanitizeName(data.StudentName))
	if err := os.MkdirAll(studentDir, 0o755); err != nil {
		return err
	}
	reportPath := filepath.Join(studentDir, "report.html")
	f, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer f.Close()

	tpl, err := template.New("student").Funcs(template.FuncMap{
		"fmtDuration": func(d time.Duration) string {
			return fmt.Sprintf("%.3fs", d.Seconds())
		},
		"pct": func(a, b int) string {
			if b == 0 {
				return "0.0%"
			}
			return fmt.Sprintf("%.1f%%", float64(a)*100/float64(b))
		},
		"scatterData": func(results []testCaseResult) template.JS {
			type point struct {
				X     int     `json:"x"`
				Y     float64 `json:"y"`
				Label string  `json:"label"`
			}
			pts := make([]point, 0, len(results))
			for _, r := range results {
				if r.InputSize <= 0 || r.FailureType == failureCompile || r.FailureType == failureSourceMissing {
					continue
				}
				pts = append(pts, point{
					X:     r.InputSize,
					Y:     float64(r.Duration.Milliseconds()),
					Label: r.CaseName,
				})
			}
			b, _ := json.Marshal(pts)
			return template.JS(b)
		},
	}).Parse(studentReportTemplate)
	if err != nil {
		return err
	}

	return tpl.Execute(f, data)
}

func writeSummaryReport(summary summaryReport) error {
	summaryDir := filepath.Join(reportRootFolder, summarySubfolder)
	if err := os.MkdirAll(summaryDir, 0o755); err != nil {
		return err
	}
	summaryPath := filepath.Join(summaryDir, "index.html")
	f, err := os.Create(summaryPath)
	if err != nil {
		return err
	}
	defer f.Close()

	tpl, err := template.New("summary").Funcs(template.FuncMap{
		"pct": func(a, b int) string {
			if b == 0 {
				return "0.0%"
			}
			return fmt.Sprintf("%.1f%%", float64(a)*100/float64(b))
		},
	}).Parse(summaryReportTemplate)
	if err != nil {
		return err
	}

	return tpl.Execute(f, summary)
}

func writeIndexReport(students []studentReportData, summary summaryReport) error {
	data := spaData{Students: students, Summary: summary}
	f, err := os.Create(filepath.Join(reportRootFolder, "index.html"))
	if err != nil {
		return err
	}
	defer f.Close()
	tpl, err := template.New("index").Funcs(template.FuncMap{
		"pct": func(a, b int) string {
			if b == 0 {
				return "0.0%"
			}
			return fmt.Sprintf("%.1f%%", float64(a)*100/float64(b))
		},
		"fmtDuration": func(d time.Duration) string {
			return fmt.Sprintf("%.3fs", d.Seconds())
		},
		"sanitizeID": sanitizeName,
		"chartConfig": func(results []testCaseResult) template.JS {
			type point struct {
				X     int     `json:"x"`
				Y     float64 `json:"y"`
				Label string  `json:"label"`
			}
			pts := make([]point, 0, len(results))
			for _, r := range results {
				if r.InputSize <= 0 || r.FailureType == failureCompile || r.FailureType == failureSourceMissing {
					continue
				}
				pts = append(pts, point{
					X:     r.InputSize,
					Y:     float64(r.Duration.Milliseconds()),
					Label: r.CaseName,
				})
			}
			cfg := map[string]interface{}{
				"type": "scatter",
				"data": map[string]interface{}{
					"datasets": []interface{}{
						map[string]interface{}{
							"label":           "Input Size vs Time (ms)",
							"data":            pts,
							"backgroundColor": "rgba(59,130,246,0.7)",
							"pointRadius":     5,
						},
					},
				},
				"options": map[string]interface{}{
					"plugins": map[string]interface{}{
						"legend": map[string]interface{}{"display": false},
						"title":  map[string]interface{}{"display": true, "text": "Input Size vs Time"},
					},
					"scales": map[string]interface{}{
						"x": map[string]interface{}{"title": map[string]interface{}{"display": true, "text": "Input Size"}, "type": "linear"},
						"y": map[string]interface{}{"title": map[string]interface{}{"display": true, "text": "Time (ms)"}, "beginAtZero": true},
					},
				},
			}
			b, _ := json.Marshal(cfg)
			return template.JS(b)
		},
	}).Parse(indexReportTemplate)
	if err != nil {
		return err
	}
	return tpl.Execute(f, data)
}

func printConsoleSummary(allReports []studentReport, summary summaryReport) {
	fmt.Println("\n=== Per student results ===")
	for _, report := range allReports {
		fmt.Printf(
			"%s -> total: %d, passed: %d, failed: %d (compile: %d, runtime: %d, diff: %d)\n",
			report.StudentName,
			report.Total,
			report.Passed,
			report.Failed,
			report.CompileFails,
			report.RuntimeFails,
			report.DiffFails,
		)
		fmt.Printf("report: %s\n", filepath.Join(reportRootFolder, sanitizeName(report.StudentName), "report.html"))
	}

	fmt.Println("\n=== Global summary by exercise ===")
	for _, row := range summary.Rows {
		fmt.Printf("Exercise %d -> students: %d  all pass: %d  partial: %d  all fail: %d  compile fail: %d  not submitted: %d\n",
			row.Exercise, row.Total, row.AllPass, row.Partial, row.AllFail, row.CompileFail, row.NotSubmitted)
	}
	fmt.Printf("summary report: %s\n", filepath.Join(reportRootFolder, summarySubfolder, "index.html"))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func removeClassFiles(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".class") {
			_ = os.Remove(path)
		}
		return nil
	})
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	if name == "" {
		return "student"
	}
	return name
}

func trimDiagnostic(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxDiagnosticTextSize {
		return s
	}
	return s[:maxDiagnosticTextSize] + "\n... (truncated)"
}

var studentReportTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Test Report - {{.StudentName}}</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js@4/dist/chart.umd.min.js"></script>
  <script>document.documentElement.setAttribute('data-theme',localStorage.getItem('theme')||'light');</script>
  <style>
    :root{--bg:#f7f7f9;--surface:#fff;--border:#ddd;--text:#1a1a1a;--muted:#555;--th:#f2f2f4;--row-border:#ececec;--metric:#fafafa;--metric-border:#e6e6e6;--pre:#f5f5f5;--link:#1a6fb5;}
    [data-theme=dark]{--bg:#18181b;--surface:#27272a;--border:#3f3f46;--text:#e4e4e7;--muted:#a1a1aa;--th:#333;--row-border:#3f3f46;--metric:#333;--metric-border:#52525b;--pre:#1e1e21;--link:#60a5fa;}
    body { font-family: "Helvetica Neue", Arial, sans-serif; margin: 24px; color: var(--text); background: var(--bg); }
    h1, h2 { margin: 0 0 12px; }
    a { color: var(--link); }
    .card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 16px; margin-bottom: 16px; }
    .metrics { display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 8px; }
    .metric { background: var(--metric); border: 1px solid var(--metric-border); border-radius: 8px; padding: 8px 10px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; margin-bottom: 12px; }
    th, td { border-bottom: 1px solid var(--row-border); padding: 8px; text-align: left; vertical-align: top; }
    th { background: var(--th); }
    .ok { color: #0f7a34; font-weight: 700; }
    .fail { color: #b11f1f; font-weight: 700; }
    pre { margin: 0; white-space: pre-wrap; word-break: break-word; font-size: 12px; background: var(--pre); border-radius: 6px; padding: 8px; }
    .ex-stats { font-size: 13px; color: var(--muted); margin: 0 0 8px; }
    .chart-wrap { max-width: 560px; margin-top: 8px; }
    #theme-toggle { background: none; border: 1px solid var(--border); border-radius: 6px; padding: 4px 10px; cursor: pointer; color: var(--text); font-size: 14px; }
  </style>
</head>
<body>
  <nav style="display:flex; gap:16px; align-items:center; flex-wrap:wrap; margin-bottom:16px; padding:8px 0; border-bottom:1px solid var(--border);">
    <a href="{{.IndexPath}}">&#8592; All Students</a>
    {{if .PrevPath}}<a href="{{.PrevPath}}">&#8592; {{.PrevName}}</a>{{end}}
    <span style="flex:1"></span>
    {{if .NextPath}}<a href="{{.NextPath}}">{{.NextName}} &#8594;</a>{{end}}
    <a href="{{.SummaryPath}}">Summary &#8594;</a>
    <button id="theme-toggle" onclick="toggleTheme()">🌙</button>
  </nav>
  <h1>Student Report: {{.StudentName}}</h1>
  <div class="card">
    <div><strong>Student folder:</strong> {{.StudentPath}}</div>
    <div><strong>Started:</strong> {{.StartedAt}}</div>
    <div><strong>Finished:</strong> {{.FinishedAt}}</div>
    <div class="metrics" style="margin-top:10px;">
      <div class="metric"><strong>Total:</strong> {{.Total}}</div>
      <div class="metric"><strong>Passed:</strong> {{.Passed}} ({{pct .Passed .Total}})</div>
      <div class="metric"><strong>Failed:</strong> {{.Failed}} ({{pct .Failed .Total}})</div>
      <div class="metric"><strong>Compile / Runtime / Diff:</strong> {{.CompileFails}} / {{.RuntimeFails}} / {{.DiffFails}}</div>
    </div>
  </div>

  {{range .Groups}}
  <div class="card">
    <h2>Exercise {{.Exercise}}</h2>
    <p class="ex-stats">Passed: <strong>{{.Passed}}</strong> &nbsp; Failed: <strong>{{.Failed}}</strong> &nbsp; Total: <strong>{{len .Results}}</strong></p>
    <table>
      <thead>
        <tr>
          <th>Case</th>
          <th>Status</th>
          <th>Failure Type</th>
          <th>Input Size</th>
          <th>Time</th>
          <th>Diagnostics</th>
          <th>Expected</th>
          <th>Actual</th>
        </tr>
      </thead>
      <tbody>
      {{range .Results}}
        <tr>
          <td>{{.InputFile}}</td>
          <td>{{if .Passed}}<span class="ok">PASS</span>{{else}}<span class="fail">FAIL</span>{{end}}</td>
          <td>{{.FailureType}}</td>
          <td>{{if gt .InputSize 0}}{{.InputSize}}{{end}}</td>
          <td>{{fmtDuration .Duration}}</td>
          <td>{{if .Message}}<pre>{{.Message}}</pre>{{end}}</td>
          <td>{{if .Expected}}<pre>{{.Expected}}</pre>{{end}}</td>
          <td>{{if .Actual}}<pre>{{.Actual}}</pre>{{end}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{if .HasChartData}}
    <div class="chart-wrap">
      <canvas id="chart-ex-{{.Exercise}}"></canvas>
    </div>
    <script>
    new Chart(document.getElementById('chart-ex-{{.Exercise}}'), {
      type: 'scatter',
      data: {
        datasets: [{
          label: 'Input Size vs Time (ms)',
          data: {{scatterData .Results}},
          backgroundColor: 'rgba(59,130,246,0.7)',
          pointRadius: 5
        }]
      },
      options: {
        plugins: {
          legend: { display: false },
          title: { display: true, text: 'Input Size vs Time' }
        },
        scales: {
          x: { title: { display: true, text: 'Input Size' }, type: 'linear' },
          y: { title: { display: true, text: 'Time (ms)' }, beginAtZero: true }
        }
      }
    });
    </script>
    {{end}}
  </div>
  {{end}}
  <script>
  function toggleTheme(){var n=document.documentElement.getAttribute('data-theme')==='dark'?'light':'dark';document.documentElement.setAttribute('data-theme',n);localStorage.setItem('theme',n);document.getElementById('theme-toggle').textContent=n==='dark'?'\u2600\ufe0f':'\ud83c\udf19';}
  document.getElementById('theme-toggle').textContent=document.documentElement.getAttribute('data-theme')==='dark'?'\u2600\ufe0f':'\ud83c\udf19';
  </script>
</body>
</html>`

var indexReportTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>Test Results</title>
<script>document.documentElement.setAttribute('data-theme',localStorage.getItem('theme')||'light');</script>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4/dist/chart.umd.min.js"></script>
<style>
:root{--bg:#f7f7f9;--surface:#fff;--border:#ddd;--text:#1a1a1a;--muted:#555;--th:#f2f2f4;--row-border:#ececec;--metric:#fafafa;--metric-border:#e6e6e6;--pre:#f5f5f5;--link:#1a6fb5;--sidebar:#ebebef;}
[data-theme=dark]{--bg:#18181b;--surface:#27272a;--border:#3f3f46;--text:#e4e4e7;--muted:#a1a1aa;--th:#333;--row-border:#3f3f46;--metric:#333;--metric-border:#52525b;--pre:#1e1e21;--link:#60a5fa;--sidebar:#1f1f22;}
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%}
body{display:flex;font-family:"Helvetica Neue",Arial,sans-serif;color:var(--text);background:var(--bg);font-size:14px}
a{color:var(--link);text-decoration:none}
h1{font-size:1.3rem;margin-bottom:14px}
h2{font-size:1.05rem;margin-bottom:8px}
#sidebar{width:220px;background:var(--sidebar);border-right:1px solid var(--border);display:flex;flex-direction:column;flex-shrink:0;position:sticky;top:0;height:100vh;overflow:hidden}
#sidebar-header{display:flex;justify-content:space-between;align-items:center;padding:12px 14px;border-bottom:1px solid var(--border);font-weight:700;flex-shrink:0}
#sidebar-nav{flex:1;overflow-y:auto;padding:6px}
.nav-link{display:block;padding:5px 10px;border-radius:6px;color:var(--text);font-size:13px;margin-bottom:1px}
.nav-link:hover,.nav-link.active{background:var(--th)}
.nav-link.active{font-weight:600}
.nav-sep{border:none;border-top:1px solid var(--border);margin:5px 2px}
#main{flex:1;overflow-y:auto;padding:24px;min-width:0}
.page{display:none}
.page.active{display:block}
.card{background:var(--surface);border:1px solid var(--border);border-radius:10px;padding:16px;margin-bottom:14px}
.metrics{display:grid;grid-template-columns:repeat(4,minmax(100px,1fr));gap:8px;margin-top:10px}
.metric{background:var(--metric);border:1px solid var(--metric-border);border-radius:8px;padding:7px 10px}
table{width:100%;border-collapse:collapse;margin-bottom:10px}
th,td{border-bottom:1px solid var(--row-border);padding:7px 8px;text-align:left;vertical-align:top}
th{background:var(--th);font-weight:600}
.ok{color:#0f7a34;font-weight:700}
.fail{color:#b11f1f;font-weight:700}
pre{white-space:pre-wrap;word-break:break-word;font-size:11px;background:var(--pre);border-radius:6px;padding:6px}
.ex-stats{font-size:12px;color:var(--muted);margin-bottom:6px}
.chart-wrap{max-width:520px;margin-top:6px}
.allpass{color:#0f7a34;font-weight:700}
.partial{color:#b07800;font-weight:700}
.allfail{color:#b11f1f;font-weight:700}
.compile{color:#7a1fb0;font-weight:700}
.missing{color:#888;font-weight:700}
.tc{text-align:center}
#theme-toggle{background:none;border:1px solid var(--border);border-radius:6px;padding:3px 8px;cursor:pointer;color:var(--text);font-size:13px}
</style>
</head>
<body>
<div id="sidebar">
  <div id="sidebar-header">
    <span>Test Results</span>
    <button id="theme-toggle" onclick="toggleTheme()">🌙</button>
  </div>
  <nav id="sidebar-nav">
    <a class="nav-link" href="#summary" onclick="return nav('summary',this)">&#128202; Summary</a>
    <hr class="nav-sep"/>
    {{range .Students}}
    <a class="nav-link" href="#{{sanitizeID .StudentName}}" onclick="return nav('{{sanitizeID .StudentName}}',this)">{{.StudentName}}</a>
    {{end}}
  </nav>
</div>
<div id="main">
  <div id="summary" class="page">
    <h1>Summary by Exercise</h1>
    <div class="card">
      <table>
        <thead><tr>
          <th>Exercise</th>
          <th class="tc">Students</th><th class="tc">All Pass</th><th class="tc">Partial</th>
          <th class="tc">All Fail</th><th class="tc">Compile Fail</th><th class="tc">Not Submitted</th>
        </tr></thead>
        <tbody>
        {{range .Summary.Rows}}
        <tr>
          <td style="font-weight:600">{{.Exercise}}</td>
          <td class="tc">{{.Total}}</td>
          <td class="tc allpass">{{.AllPass}}</td>
          <td class="tc partial">{{.Partial}}</td>
          <td class="tc allfail">{{.AllFail}}</td>
          <td class="tc compile">{{.CompileFail}}</td>
          <td class="tc missing">{{.NotSubmitted}}</td>
        </tr>
        {{end}}
        </tbody>
      </table>
    </div>
  </div>
  {{range .Students}}
  {{$sid := sanitizeID .StudentName}}
  <div id="{{$sid}}" class="page">
    <h1>{{.StudentName}}</h1>
    <div class="card">
      <div><strong>Path:</strong> {{.StudentPath}}</div>
      <div><strong>Started:</strong> {{.StartedAt}}</div>
      <div><strong>Finished:</strong> {{.FinishedAt}}</div>
      <div class="metrics">
        <div class="metric"><strong>Total:</strong> {{.Total}}</div>
        <div class="metric"><strong>Passed:</strong> {{.Passed}} ({{pct .Passed .Total}})</div>
        <div class="metric"><strong>Failed:</strong> {{.Failed}} ({{pct .Failed .Total}})</div>
        <div class="metric"><strong>Compile/Runtime/Diff:</strong> {{.CompileFails}}/{{.RuntimeFails}}/{{.DiffFails}}</div>
      </div>
    </div>
    {{range .Groups}}
    <div class="card">
      <h2>Exercise {{.Exercise}}</h2>
      <p class="ex-stats">Passed: <strong>{{.Passed}}</strong> &nbsp; Failed: <strong>{{.Failed}}</strong> &nbsp; Total: <strong>{{len .Results}}</strong></p>
      <table>
        <thead><tr>
          <th>Case</th><th>Status</th><th>Failure Type</th><th>Input Size</th>
          <th>Time</th><th>Diagnostics</th><th>Expected</th><th>Actual</th>
        </tr></thead>
        <tbody>
        {{range .Results}}
        <tr>
          <td>{{.InputFile}}</td>
          <td>{{if .Passed}}<span class="ok">PASS</span>{{else}}<span class="fail">FAIL</span>{{end}}</td>
          <td>{{.FailureType}}</td>
          <td>{{if gt .InputSize 0}}{{.InputSize}}{{end}}</td>
          <td>{{fmtDuration .Duration}}</td>
          <td>{{if .Message}}<pre>{{.Message}}</pre>{{end}}</td>
          <td>{{if .Expected}}<pre>{{.Expected}}</pre>{{end}}</td>
          <td>{{if .Actual}}<pre>{{.Actual}}</pre>{{end}}</td>
        </tr>
        {{end}}
        </tbody>
      </table>
      {{if .HasChartData}}<div class="chart-wrap"><canvas id="chart-{{$sid}}-{{.Exercise}}"></canvas></div>{{end}}
    </div>
    {{end}}
  </div>
  {{end}}
</div>
<script>
var _cc={
{{range .Students}}{{$sid := sanitizeID .StudentName}}{{range .Groups}}{{if .HasChartData}}"chart-{{$sid}}-{{.Exercise}}":{{chartConfig .Results}},
{{end}}{{end}}{{end}}};
var _ci={};
function _ic(el){el.querySelectorAll('canvas').forEach(function(c){if(!_ci[c.id]&&_cc[c.id]){_ci[c.id]=new Chart(c,_cc[c.id]);}});}
function nav(id,el){
  document.querySelectorAll('.page').forEach(function(p){p.classList.remove('active');});
  document.querySelectorAll('.nav-link').forEach(function(l){l.classList.remove('active');});
  var pg=document.getElementById(id);
  if(pg){pg.classList.add('active');_ic(pg);}
  if(el)el.classList.add('active');
  history.replaceState(null,'','#'+id);
  document.getElementById('main').scrollTop=0;
  return false;
}
function toggleTheme(){
  var n=document.documentElement.getAttribute('data-theme')==='dark'?'light':'dark';
  document.documentElement.setAttribute('data-theme',n);
  localStorage.setItem('theme',n);
  document.getElementById('theme-toggle').textContent=n==='dark'?'☀️':'🌙';
}
document.getElementById('theme-toggle').textContent=document.documentElement.getAttribute('data-theme')==='dark'?'☀️':'🌙';
(function(){
  var h=location.hash.slice(1)||'summary';
  nav(h,document.querySelector('[href="#'+h+'"]'));
}());
window.addEventListener('popstate',function(){
  var h=location.hash.slice(1)||'summary';
  nav(h,document.querySelector('[href="#'+h+'"]'));
});
</script>
</body>
</html>`

var summaryReportTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Global Exercise Summary</title>
  <script>document.documentElement.setAttribute('data-theme',localStorage.getItem('theme')||'light');</script>
  <style>
    :root{--bg:#f7f7f9;--surface:#fff;--border:#ddd;--text:#1a1a1a;--th:#f2f2f4;--row-border:#ececec;--link:#1a6fb5;}
    [data-theme=dark]{--bg:#18181b;--surface:#27272a;--border:#3f3f46;--text:#e4e4e7;--th:#333;--row-border:#3f3f46;--link:#60a5fa;}
    body { font-family: "Helvetica Neue", Arial, sans-serif; margin: 24px; color: var(--text); background: var(--bg); }
    h1 { margin: 0 0 12px; }
    a { color: var(--link); }
    .card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 16px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; }
    th, td { border-bottom: 1px solid var(--row-border); padding: 8px; text-align: center; }
    th { background: var(--th); }
    td:first-child { text-align: left; font-weight: 600; }
    .allpass { color: #0f7a34; font-weight: 700; }
    .partial  { color: #b07800; font-weight: 700; }
    .allfail  { color: #b11f1f; font-weight: 700; }
    .compile  { color: #7a1fb0; font-weight: 700; }
    .missing  { color: #888;    font-weight: 700; }
    #theme-toggle { background: none; border: 1px solid var(--border); border-radius: 6px; padding: 4px 10px; cursor: pointer; color: var(--text); font-size: 14px; }
  </style>
</head>
<body>
  <nav style="display:flex; align-items:center; gap:16px; margin-bottom:16px; padding:8px 0; border-bottom:1px solid var(--border);">
    <a href="../index.html">&#8592; All Students</a>
    <span style="flex:1"></span>
    <button id="theme-toggle" onclick="toggleTheme()">🌙</button>
  </nav>
  <h1>Summary by Exercise</h1>
  <div class="card">
    <div><strong>Generated:</strong> {{.GeneratedAt}}</div>
    <table style="margin-top: 12px;">
      <thead>
        <tr>
          <th>Exercise</th>
          <th>Students</th>
          <th>All Pass</th>
          <th>Partial</th>
          <th>All Fail</th>
          <th>Compile Fail</th>
          <th>Not Submitted</th>
        </tr>
      </thead>
      <tbody>
      {{range .Rows}}
        <tr>
          <td>{{.Exercise}}</td>
          <td>{{.Total}}</td>
          <td class="allpass">{{.AllPass}}</td>
          <td class="partial">{{.Partial}}</td>
          <td class="allfail">{{.AllFail}}</td>
          <td class="compile">{{.CompileFail}}</td>
          <td class="missing">{{.NotSubmitted}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>
  <script>
  function toggleTheme(){var n=document.documentElement.getAttribute('data-theme')==='dark'?'light':'dark';document.documentElement.setAttribute('data-theme',n);localStorage.setItem('theme',n);document.getElementById('theme-toggle').textContent=n==='dark'?'\u2600\ufe0f':'\ud83c\udf19';}
  document.getElementById('theme-toggle').textContent=document.documentElement.getAttribute('data-theme')==='dark'?'\u2600\ufe0f':'\ud83c\udf19';
  </script>
</body>
</html>`
