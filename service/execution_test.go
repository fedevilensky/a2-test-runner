package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fedevilensky/a2-test-runner/models"
)

func TestEffectiveFlags(t *testing.T) {
	got, err := EffectiveFlags(models.BuildSettings{Optimization: "2", Extra: []string{"-Wall"}}, models.BuildSettings{Standard: "c++17", Optimization: "0"})
	if err != nil || !reflect.DeepEqual(got, []string{"-std=c++17", "-O0", "-Wall"}) {
		t.Fatalf("flags = %v, %v", got, err)
	}
	if _, err := EffectiveFlags(models.BuildSettings{Extra: []string{"-std=c++20"}}, models.BuildSettings{}); err == nil {
		t.Fatal("expected conflicting standard to be rejected")
	}
}
func TestPartialRunRecordsCompileFailure(t *testing.T) {
	root := t.TempDir()
	testDir := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "student")
	for _, dir := range []string{testDir, student} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"1.in.txt", "2.in.txt"} {
		if err := os.WriteFile(filepath.Join(testDir, name), []byte("input"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := Run(context.Background(), Config{TestsFolder: filepath.Dir(testDir), Students: []models.StudentConfig{{Path: student}}, Selection: models.ResultSelection{Exercises: map[int][]string{1: {"2.in.txt"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Total != 1 || reports[0].Groups[0].Results[0].InputFile != "2.in.txt" || reports[0].Groups[0].Results[0].FailureType != "source_missing" {
		t.Fatalf("unexpected result: %+v", reports)
	}
	r := reports[0].Groups[0].Results[0]
	if r.ExecutedAt.IsZero() || len(r.BuildFlags) != 0 {
		t.Fatalf("missing provenance: %+v", r)
	}
}

func TestRunCPP(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ unavailable")
	}
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "student")
	for _, dir := range []string{tests, student} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{filepath.Join(student, "ejercicio1.cpp"): `#include <iostream>
int main() { int n; std::cin >> n; std::cout << n * 2 << "\n"; }`, filepath.Join(tests, "3.in.txt"): "3\n", filepath.Join(tests, "3.out.txt"): "6\n"}
	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := Run(context.Background(), Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: student, Build: models.BuildSettings{Optimization: "2"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := reports[0].Groups[0].Results[0]
	if !r.Passed || !reflect.DeepEqual(r.BuildFlags, []string{"-std=c++11", "-O2"}) {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRunRejectsEmptyTestsWithoutReport(t *testing.T) {
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "student")
	for _, dir := range []string{tests, student} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := Run(context.Background(), Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: student}}}, nil)
	if err == nil || reports != nil {
		t.Fatalf("empty tests must not generate a report: %v, %v", reports, err)
	}
}
func TestRunHonorsAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Config{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
func TestEffectiveFlagsRejectsInvalidExtras(t *testing.T) {
	for _, flag := range []string{"", "-std=c++17", "-O2", "-DVALUE=bad\x00value", "source.cpp", "-o", "-o/tmp/output", "--output=other", "-c", "-xjava", "@args"} {
		if _, err := EffectiveFlags(models.BuildSettings{Extra: []string{flag}}, models.BuildSettings{}); err == nil {
			t.Errorf("accepted invalid extra flag %q", flag)
		}
	}
	flags, err := EffectiveFlags(models.BuildSettings{Optimization: "-O3"}, models.BuildSettings{Optimization: "default"})
	if err != nil || !reflect.DeepEqual(flags, []string{"-std=c++11"}) {
		t.Fatalf("default optimization: %v, %v", flags, err)
	}
}

func TestPartialRunRejectsEmptySelectedExercise(t *testing.T) {
	root := t.TempDir()
	tests := filepath.Join(root, "tests")
	student := filepath.Join(root, "student")
	for _, dir := range []string{filepath.Join(tests, "exercise1"), filepath.Join(tests, "exercise2"), student} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tests, "exercise1", "1.in.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	reports, err := Run(context.Background(), Config{TestsFolder: tests, Students: []models.StudentConfig{{Path: student}}, Selection: models.ResultSelection{Exercises: map[int][]string{1: nil, 2: nil}}}, nil)
	if err == nil || reports != nil {
		t.Fatalf("expected error for empty selected exercise: %v, %v", reports, err)
	}
}

func TestJavaDoesNotRecordCPPFlags(t *testing.T) {
	if _, err := exec.LookPath("javac"); err != nil {
		t.Skip("javac unavailable")
	}
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java unavailable")
	}
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "student")
	for _, dir := range []string{tests, student} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{filepath.Join(student, "Ejercicio1.java"): `public class Ejercicio1 { public static void main(String[] args) { System.out.println("ok"); } }`, filepath.Join(tests, "1.in.txt"): "", filepath.Join(tests, "1.out.txt"): "ok\n"}
	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := Run(context.Background(), Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: student}}, Build: models.BuildSettings{Optimization: "2", Extra: []string{"-Wall"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := reports[0].Groups[0].Results[0]
	if !r.Passed || len(r.BuildFlags) != 0 {
		t.Fatalf("unexpected Java result: %+v", r)
	}
}

func TestMissingSourceReportsEveryCase(t *testing.T) {
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "student")
	for _, dir := range []string{tests, student} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"1.in.txt", "2.in.txt"} {
		if err := os.WriteFile(filepath.Join(tests, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := Run(context.Background(), Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: student}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reports[0].Total != 2 || reports[0].Failed != 2 || len(reports[0].Groups) != 1 {
		t.Fatalf("unexpected totals: %+v", reports[0])
	}
	for _, r := range reports[0].Groups[0].Results {
		if r.FailureType != "source_missing" || r.Passed || r.Message == "" || len(r.BuildFlags) != 0 {
			t.Errorf("unexpected result: %+v", r)
		}
	}
}

func TestRunProgressCountsCompilationAndFailedCases(t *testing.T) {
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	alice := filepath.Join(root, "alice")
	bob := filepath.Join(root, "bob")
	for _, dir := range []string{tests, alice, bob} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"1.in.txt", "2.in.txt"} {
		if err := os.WriteFile(filepath.Join(tests, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var updates []models.RunProgress
	completed := 0
	cfg := Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: alice}, {Path: bob}}, OnProgress: func(p models.RunProgress) { updates = append(updates, p) }}
	err := RunWithCompleted(context.Background(), cfg, nil, func(models.StudentReportData) error {
		completed++
		last := updates[len(updates)-1]
		if last.CompletedStudents != completed-1 {
			t.Errorf("student counted before report publication: %+v", last)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	last := updates[len(updates)-1]
	if last.TotalUnits != 6 || last.CompletedUnits != 6 || last.TotalStudents != 2 || last.CompletedStudents != 2 || completed != 2 {
		t.Fatalf("unexpected final progress: %+v; completed=%d", last, completed)
	}
	previous := 0
	for _, update := range updates {
		if update.TotalUnits != 6 || update.CompletedUnits < previous || update.CompletedUnits > 6 {
			t.Fatalf("invalid progress sequence: %+v", updates)
		}
		previous = update.CompletedUnits
	}
}
