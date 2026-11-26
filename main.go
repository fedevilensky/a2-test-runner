package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/service"
	"github.com/urfave/cli"
)

const (
	reportRootFolder = "test-results"
	summarySubfolder = "summary"
)

type config struct {
	testsFolder          string
	testNumbersInput     string
	studentFolder        string
	studentFolderPattern string
	timeout              time.Duration
	outputDir            string
	cppStandard          string
	optimization         string
	compilerFlags        []string
	incremental          bool
}

func main() {
	app := cli.NewApp()
	app.Name = "test-runner"
	app.Usage = "Run programming exercise tests and generate HTML reports"
	app.Description = `Without a command, run tests once using the options below and print progress and summaries in the terminal.
Use "serve" to configure and run tests interactively in a local browser, and browse saved reports.
Both modes share SQLite results and static HTML reports in the selected output directory.
Run "test-runner serve --help" for server options.`
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
		cli.StringFlag{Name: "output-dir, o", Usage: "Directory for reports and result data", Value: "test-results"},
		cli.StringFlag{Name: "cpp-standard", Usage: "C++ standard (default c++11)"},
		cli.StringFlag{Name: "optimization", Usage: "C++ optimization level, e.g. O0, O2, O3, Os"},
		cli.StringSliceFlag{Name: "compiler-flag", Usage: "Additional compiler argument (repeatable)"},
		cli.BoolFlag{Name: "incremental", Usage: "Reuse unchanged test results when possible"},
		cli.BoolFlag{Name: "force", Usage: "Run every selected test, even with --incremental"},
	}

	app.Commands = []cli.Command{{Name: "serve", Usage: "Start the local web interface", Description: `Start a local browser interface for configuring runs, selecting exercises or individual cases,
viewing live progress, and browsing saved reports. Choose tests, students, timeout, and build
settings in the interface; the one-shot CLI run options do not configure server runs.
Results and static reports use the same output directory as the CLI.`, Flags: []cli.Flag{
		cli.IntFlag{Name: "listen", Value: 8080, Usage: "Loopback port"},
		cli.StringFlag{Name: "output-dir, o", Value: "test-results", Usage: "Directory for reports and result data"},
	}, Action: func(c *cli.Context) error { return serveApplication(c.String("output-dir"), c.Int("listen")) }}}

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
		outputDir:            c.String("output-dir"),
		cppStandard:          c.String("cpp-standard"),
		optimization:         c.String("optimization"),
		compilerFlags:        c.StringSlice("compiler-flag"),
		incremental:          c.Bool("incremental") && !c.Bool("force"),
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
	folders, err := resolveStudentFolders(cfg)
	if err != nil {
		return err
	}
	exercises, err := service.DiscoverExercises(cfg.testsFolder)
	if err != nil {
		return err
	}
	selection, err := service.SelectExercises(cfg.testNumbersInput, exercises)
	if err != nil {
		return err
	}
	students := make([]models.StudentConfig, 0, len(folders))
	for _, folder := range folders {
		students = append(students, models.StudentConfig{Path: folder})
	}
	app, err := openApplication(cfg.outputDir)
	if err != nil {
		return err
	}
	defer func() { _ = app.results.Close() }()
	runCfg := service.Config{
		TestsFolder: cfg.testsFolder, Students: students, Timeout: cfg.timeout, Selection: selection, Incremental: cfg.incremental,
		Build: models.BuildSettings{Standard: cfg.cppStandard, Optimization: cfg.optimization, Extra: cfg.compilerFlags},
	}
	if err := app.execute(context.Background(), runCfg, func(message string) { fmt.Fprintln(os.Stderr, message) }); err != nil {
		return err
	}
	reports, err := app.results.List(context.Background())
	if err != nil {
		return err
	}
	views, summary := service.BuildReportViews(reports)
	printConsoleSummary(views, summary, app.staticDir)
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

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	if name == "" {
		return "student"
	}
	return name
}

//go:generate go tool templ generate
