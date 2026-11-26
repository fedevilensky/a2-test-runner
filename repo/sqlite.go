// Package repo persists reports in SQLite.
package repo

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/georgysavva/scany/v2/sqlscan"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// SQLite stores the current result for each student folder.
type SQLite struct{ db *sql.DB }

func Open(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = provider.Up(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate results database: %w", err)
	}
	return &SQLite{db: db}, nil
}
func (s *SQLite) Close() error { return s.db.Close() }

type queryer interface{ sqlscan.Querier }
type metadataRow struct {
	Metadata string `db:"metadata"`
}
type exerciseRow struct {
	Exercise int    `db:"exercise"`
	Metadata string `db:"metadata"`
}
type caseRow struct {
	InputFile   string `db:"input_file"`
	InputSize   int    `db:"input_size"`
	DurationNS  int64  `db:"duration_ns"`
	Passed      bool   `db:"passed"`
	FailureType string `db:"failure_type"`
	Message     string `db:"message"`
	Expected    string `db:"expected"`
	Actual      string `db:"actual"`
	Metadata    string `db:"metadata"`
}

func get(ctx context.Context, q queryer, name string) (models.StudentReportData, error) {
	var out models.StudentReportData
	var row metadataRow
	if err := sqlscan.Get(ctx, q, &row, "SELECT metadata FROM students WHERE name = ?", name); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(row.Metadata), &out); err != nil {
		return out, err
	}
	var groups []exerciseRow
	if err := sqlscan.Select(ctx, q, &groups, "SELECT exercise, metadata FROM exercises WHERE student_name = ? ORDER BY position", name); err != nil {
		return out, err
	}
	for _, g := range groups {
		var group models.ExerciseGroup
		if err := json.Unmarshal([]byte(g.Metadata), &group); err != nil {
			return out, err
		}
		var cases []caseRow
		if err := sqlscan.Select(ctx, q, &cases, "SELECT input_file, input_size, duration_ns, passed, failure_type, message, expected, actual, metadata FROM cases WHERE student_name = ? AND exercise = ? ORDER BY position", name, g.Exercise); err != nil {
			return out, err
		}
		for _, c := range cases {
			var result models.TestCaseResult
			if err := json.Unmarshal([]byte(c.Metadata), &result); err != nil {
				return out, err
			}
			result.InputFile = c.InputFile
			result.InputSize = c.InputSize
			result.Duration = time.Duration(c.DurationNS)
			result.Passed = c.Passed
			result.FailureType = c.FailureType
			result.Message = c.Message
			result.Expected = c.Expected
			result.Actual = c.Actual
			group.Results = append(group.Results, result)
		}
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}
func (s *SQLite) Get(ctx context.Context, name string) (models.StudentReportData, error) {
	return get(ctx, s.db, name)
}
func (s *SQLite) List(ctx context.Context) ([]models.StudentReportData, error) {
	var names []string
	if err := sqlscan.Select(ctx, s.db, &names, "SELECT name FROM students ORDER BY name"); err != nil {
		return nil, err
	}
	out := make([]models.StudentReportData, 0, len(names))
	for _, name := range names {
		report, err := get(ctx, s.db, name)
		if err != nil {
			return nil, err
		}
		out = append(out, report)
	}
	return out, nil
}
func validName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}
func save(ctx context.Context, tx *sql.Tx, report models.StudentReportData) error {
	if !validName(report.StudentName) {
		return fmt.Errorf("invalid student folder name %q", report.StudentName)
	}
	groups := report.Groups
	report.Groups = nil
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO students(name,metadata) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET metadata=excluded.metadata", report.StudentName, string(data)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM exercises WHERE student_name=?", report.StudentName); err != nil {
		return err
	}
	for i, g := range groups {
		cases := g.Results
		g.Results = nil
		data, err = json.Marshal(g)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO exercises(student_name,exercise,position,metadata) VALUES(?,?,?,?)", report.StudentName, g.Exercise, i, string(data)); err != nil {
			return err
		}
		for j, c := range cases {
			metadata := c
			metadata.InputFile = ""
			metadata.InputSize = 0
			metadata.Duration = 0
			metadata.Passed = false
			metadata.FailureType = ""
			metadata.Message = ""
			metadata.Expected = ""
			metadata.Actual = ""
			data, err = json.Marshal(metadata)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO cases(student_name,exercise,input_file,position,input_size,duration_ns,passed,failure_type,message,expected,actual,metadata) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", report.StudentName, g.Exercise, c.InputFile, j, c.InputSize, int64(c.Duration), c.Passed, c.FailureType, c.Message, c.Expected, c.Actual, string(data)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *SQLite) Replace(ctx context.Context, report models.StudentReportData) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = save(ctx, tx, report); err != nil {
		return err
	}
	return tx.Commit()
}

// Merge atomically replaces selected exercises or cases, preserving all other results.
func (s *SQLite) Merge(ctx context.Context, report models.StudentReportData, selection models.ResultSelection) error {
	if !validName(report.StudentName) {
		return fmt.Errorf("invalid student folder name %q", report.StudentName)
	}
	if len(selection.Exercises) == 0 {
		return errors.New("partial rerun must select at least one exercise")
	}
	incomingByExercise := make(map[int]models.ExerciseGroup, len(report.Groups))
	for _, group := range report.Groups {
		if _, exists := incomingByExercise[group.Exercise]; exists {
			return fmt.Errorf("duplicate exercise %d", group.Exercise)
		}
		incomingByExercise[group.Exercise] = group
	}
	for exercise, names := range selection.Exercises {
		group, exists := incomingByExercise[exercise]
		if !exists {
			return fmt.Errorf("selected exercise %d has no result", exercise)
		}
		present := make(map[string]bool, len(group.Results))
		for _, c := range group.Results {
			if present[c.InputFile] {
				return fmt.Errorf("duplicate case %q in exercise %d", c.InputFile, exercise)
			}
			present[c.InputFile] = true
		}
		for _, name := range names {
			if !present[name] {
				return fmt.Errorf("selected case %q in exercise %d has no result", name, exercise)
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	old, err := get(ctx, tx, report.StudentName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		old = report
		old.Groups = nil
	}
	for _, incoming := range report.Groups {
		selected, ok := selection.Exercises[incoming.Exercise]
		if !ok {
			continue
		}
		idx := -1
		for i, g := range old.Groups {
			if g.Exercise == incoming.Exercise {
				idx = i
				break
			}
		}
		if len(selected) == 0 || idx < 0 {
			if idx < 0 && len(selected) > 0 {
				allowed := make(map[string]bool, len(selected))
				for _, name := range selected {
					allowed[name] = true
				}
				filtered := make([]models.TestCaseResult, 0, len(selected))
				for _, c := range incoming.Results {
					if allowed[c.InputFile] {
						filtered = append(filtered, c)
					}
				}
				incoming.Results = filtered
				incoming.Passed = 0
				incoming.Failed = 0
				for _, c := range filtered {
					if c.Passed {
						incoming.Passed++
					} else {
						incoming.Failed++
					}
				}
				incoming.HasChartData = false
				incoming.ChartConfig = ""
				incoming.FitAll = models.Fit{}
				incoming.FitPassing = models.Fit{}
			}
			if idx < 0 {
				old.Groups = append(old.Groups, incoming)
			} else {
				old.Groups[idx] = incoming
			}
			continue
		}
		group := old.Groups[idx]
		allowed := map[string]bool{}
		for _, name := range selected {
			allowed[name] = true
		}
		for _, c := range incoming.Results {
			if !allowed[c.InputFile] {
				continue
			}
			found := false
			for i, previous := range group.Results {
				if previous.InputFile == c.InputFile {
					group.Results[i] = c
					found = true
					break
				}
			}
			if !found {
				group.Results = append(group.Results, c)
			}
		}
		group.HasChartData = false
		group.ChartConfig = ""
		group.FitAll = models.Fit{}
		group.FitPassing = models.Fit{}
		group.Passed = 0
		group.Failed = 0
		for _, c := range group.Results {
			if c.Passed {
				group.Passed++
			} else {
				group.Failed++
			}
		}
		old.Groups[idx] = group
	}
	old.StartedAt = report.StartedAt
	old.FinishedAt = report.FinishedAt
	old.StudentPath = report.StudentPath
	old.Total = 0
	old.Passed = 0
	old.Failed = 0
	old.CompileFails = 0
	old.RuntimeFails = 0
	old.DiffFails = 0
	for _, g := range old.Groups {
		for _, c := range g.Results {
			old.Total++
			if c.Passed {
				old.Passed++
			} else {
				old.Failed++
				switch c.FailureType {
				case "compilation_error":
					old.CompileFails++
				case "runtime_error":
					old.RuntimeFails++
				case "diff_error":
					old.DiffFails++
				}
			}
		}
	}
	if err = save(ctx, tx, old); err != nil {
		return err
	}
	return tx.Commit()
}
