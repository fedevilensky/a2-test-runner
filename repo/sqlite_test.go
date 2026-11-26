package repo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
)

func TestReplaceMergeAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "results.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original := models.StudentReportData{StudentName: "alice", Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{InputFile: "a", Passed: true}, {InputFile: "b", Passed: true}}}, {Exercise: 2, Results: []models.TestCaseResult{{InputFile: "c", Passed: true}}}}}
	if err := db.Replace(ctx, original); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	update := models.StudentReportData{StudentName: "alice", Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{InputFile: "a", FailureType: "runtime_error", ExecutedAt: now, BuildFlags: []string{"-O2"}}}}}}
	if err := db.Merge(ctx, update, models.ResultSelection{Exercises: map[int][]string{1: {"a"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 2 || len(got.Groups[0].Results) != 2 || got.Passed != 2 || got.Failed != 1 || got.RuntimeFails != 1 {
		t.Fatalf("bad merge: %+v", got)
	}
	if got.Groups[0].Results[0].ExecutedAt != now || got.Groups[0].Results[0].BuildFlags[0] != "-O2" {
		t.Fatal("metadata lost")
	}
	// A duplicate case violates the key and rolls the whole replacement back.
	bad := original
	bad.Groups[0].Results = append(bad.Groups[0].Results, bad.Groups[0].Results[0])
	if err := db.Replace(ctx, bad); err == nil {
		t.Fatal("expected duplicate failure")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	reports, err := db.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Failed != 1 {
		t.Fatalf("rollback or reopen failed: %+v", reports)
	}
}

func TestMergeValidatesSelectionAndRollsBack(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "results.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	original := models.StudentReportData{StudentName: "alice", Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{InputFile: "a", Passed: true}}}}}
	if err := db.Replace(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := db.Merge(ctx, original, models.ResultSelection{Exercises: map[int][]string{1: {"missing"}}}); err == nil {
		t.Fatal("expected missing case error")
	}
	got, err := db.Get(ctx, "alice")
	if err != nil || len(got.Groups[0].Results) != 1 || !got.Groups[0].Results[0].Passed {
		t.Fatalf("original changed: %+v, %v", got, err)
	}
	incoming := models.StudentReportData{StudentName: "bob", Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{InputFile: "a"}, {InputFile: "b"}}}}}
	if err := db.Merge(ctx, incoming, models.ResultSelection{Exercises: map[int][]string{1: {"b"}}}); err != nil {
		t.Fatal(err)
	}
	got, err = db.Get(ctx, "bob")
	if err != nil || len(got.Groups[0].Results) != 1 || got.Groups[0].Results[0].InputFile != "b" {
		t.Fatalf("unselected case persisted: %+v, %v", got, err)
	}
	if err := db.Replace(ctx, models.StudentReportData{StudentName: "../bob"}); err == nil {
		t.Fatal("invalid identity accepted")
	}
}
