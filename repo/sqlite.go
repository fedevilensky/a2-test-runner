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

type (
	queryer     interface{ sqlscan.Querier }
	metadataRow struct {
		Metadata string `db:"metadata"`
	}
)

type exerciseRow struct {
	StudentName string `db:"student_name"`
	Exercise    int    `db:"exercise"`
	Metadata    string `db:"metadata"`
}
type caseRow struct {
	StudentName string `db:"student_name"`
	Exercise    int    `db:"exercise"`
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
	if err := sqlscan.Get(
		ctx, q, &row, "SELECT metadata FROM students WHERE name = ?", name,
	); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(row.Metadata), &out); err != nil {
		return out, err
	}
	var groups []exerciseRow
	if err := sqlscan.Select(ctx, q, &groups, `SELECT exercise, metadata FROM exercises
  WHERE student_name = ? ORDER BY position`, name); err != nil {
		return out, err
	}
	for _, g := range groups {
		var group models.ExerciseGroup
		if err := json.Unmarshal([]byte(g.Metadata), &group); err != nil {
			return out, err
		}
		var cases []caseRow
		if err := sqlscan.Select(ctx, q, &cases, `SELECT input_file, input_size, duration_ns, passed,
   failure_type, message, expected, actual, metadata FROM cases
   WHERE student_name = ? AND exercise = ? ORDER BY position`, name, g.Exercise); err != nil {
			return out, err
		}
		for _, c := range cases {
			result, err := decodeCase(c)
			if err != nil {
				return out, err
			}
			group.Results = append(group.Results, result)
		}
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}

func (s *SQLite) Get(ctx context.Context, name string) (models.StudentReportData, error) {
	return get(ctx, s.db, name)
}

type studentRow struct {
	Name     string `db:"name"`
	Metadata string `db:"metadata"`
}

type groupKey struct {
	student  string
	exercise int
}

// List reads a consistent snapshot in three queries, independent of report count.
func (s *SQLite) List(ctx context.Context) ([]models.StudentReportData, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var students []studentRow
	if err = sqlscan.Select(ctx, tx, &students,
		"SELECT name, metadata FROM students ORDER BY name"); err != nil {
		return nil, err
	}
	var groups []exerciseRow
	if err = sqlscan.Select(ctx, tx, &groups,
		`SELECT student_name, exercise, metadata FROM exercises
 ORDER BY student_name, position`); err != nil {
		return nil, err
	}
	var cases []caseRow
	if err = sqlscan.Select(ctx, tx, &cases, `SELECT student_name, exercise, input_file,
  input_size, duration_ns, passed, failure_type, message, expected, actual, metadata
  FROM cases ORDER BY student_name, exercise, position`); err != nil {
		return nil, err
	}
	out, err := assembleReports(students, groups, cases)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func assembleReports(
	students []studentRow, groups []exerciseRow, cases []caseRow,
) ([]models.StudentReportData, error) {
	out := make([]models.StudentReportData, 0, len(students))
	studentIndices := make(map[string]int, len(students))
	for _, row := range students {
		var report models.StudentReportData
		if err := json.Unmarshal([]byte(row.Metadata), &report); err != nil {
			return nil, err
		}
		studentIndices[row.Name] = len(out)
		out = append(out, report)
	}
	groupIndices := make(map[groupKey]int, len(groups))
	for _, row := range groups {
		var group models.ExerciseGroup
		if err := json.Unmarshal([]byte(row.Metadata), &group); err != nil {
			return nil, err
		}
		index := studentIndices[row.StudentName]
		groupIndices[groupKey{row.StudentName, row.Exercise}] = len(out[index].Groups)
		out[index].Groups = append(out[index].Groups, group)
	}
	for _, row := range cases {
		result, err := decodeCase(row)
		if err != nil {
			return nil, err
		}
		index := studentIndices[row.StudentName]
		group := groupIndices[groupKey{row.StudentName, row.Exercise}]
		out[index].Groups[group].Results = append(out[index].Groups[group].Results, result)
	}
	return out, nil
}

func decodeCase(c caseRow) (models.TestCaseResult, error) {
	var result models.TestCaseResult
	if err := json.Unmarshal([]byte(c.Metadata), &result); err != nil {
		return result, err
	}
	result.InputFile, result.InputSize = c.InputFile, c.InputSize
	result.Duration, result.Passed = time.Duration(c.DurationNS), c.Passed
	result.FailureType, result.Message = c.FailureType, c.Message
	result.Expected, result.Actual = c.Expected, c.Actual
	return result, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		!strings.ContainsAny(name, `/\\`)
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO students(name,metadata) VALUES(?,?)
  ON CONFLICT(name) DO UPDATE SET metadata=excluded.metadata`,
		report.StudentName, string(data)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(
		ctx, "DELETE FROM exercises WHERE student_name=?", report.StudentName,
	); err != nil {
		return err
	}
	for i, g := range groups {
		cases := g.Results
		g.Results = nil
		data, err = json.Marshal(g)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO exercises(student_name,exercise,position,metadata)
   VALUES(?,?,?,?)`, report.StudentName, g.Exercise, i, string(data)); err != nil {
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
			if _, err = tx.ExecContext(ctx, `INSERT INTO cases(student_name,exercise,input_file,position,
    input_size,duration_ns,passed,failure_type,message,expected,actual,metadata)
    VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, report.StudentName, g.Exercise, c.InputFile, j,
				c.InputSize, int64(c.Duration), c.Passed, c.FailureType, c.Message,
				c.Expected, c.Actual, string(data)); err != nil {
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
func (s *SQLite) Merge(
	ctx context.Context, report models.StudentReportData, selection models.ResultSelection,
) error {
	if !validName(report.StudentName) {
		return fmt.Errorf("invalid student folder name %q", report.StudentName)
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
	merged, err := models.MergeResults(old, report, selection)
	if err != nil {
		return err
	}
	if err = save(ctx, tx, merged); err != nil {
		return err
	}
	return tx.Commit()
}
