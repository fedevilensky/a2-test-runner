package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/service"
)

func TestApplicationPersistsAndMergesPartialRerun(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not available")
	}
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "students", "alice")
	for _, dir := range []string{tests, student} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(student, "ejercicio1.cpp"), "#include <iostream>\nint main(){int x;std::cin >> x;std::cout << x << std::endl;}\n")
	for _, name := range []string{"1", "2"} {
		write(filepath.Join(tests, name+".in.txt"), name)
		write(filepath.Join(tests, name+".out.txt"), name+"\n")
	}
	app, err := openApplication(filepath.Join(root, "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.results.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := service.Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: student}}, Build: models.BuildSettings{Optimization: "-O2"}}
	if err = app.execute(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(tests, "1.out.txt"), "wrong\n")
	cfg.Selection = models.ResultSelection{Exercises: map[int][]string{1: {"1.in.txt"}}}
	cfg.Build.Optimization = "-O0"
	if err = app.execute(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	reports, err := app.results.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || len(reports[0].Groups) != 1 || len(reports[0].Groups[0].Results) != 2 {
		t.Fatalf("unexpected results: %#v", reports)
	}
	cases := reports[0].Groups[0].Results
	if cases[0].Passed || !cases[1].Passed || strings.Join(cases[0].BuildFlags, " ") != "-std=c++11 -O0" || strings.Join(cases[1].BuildFlags, " ") != "-std=c++11 -O2" {
		t.Fatalf("partial results not preserved: %#v", cases)
	}
	for _, name := range []string{"index.html", "summary/index.html", "alice/report.html"} {
		data, e := os.ReadFile(filepath.Join(root, "out", "static", name))
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(data), "Summary") {
			t.Fatalf("invalid report %s", name)
		}
	}
}

func TestApplicationPublishesEachStudentBeforeNextStarts(t *testing.T) {
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	alice := filepath.Join(root, "students", "alice")
	bob := filepath.Join(root, "students", "bob")
	for _, dir := range []string{tests, alice, bob} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"1.in.txt", "1.out.txt"} {
		if err := os.WriteFile(filepath.Join(tests, name), []byte("1"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := openApplication(filepath.Join(root, "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.results.Close() })
	checked := false
	err = app.execute(context.Background(), service.Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: alice}, {Path: bob}}}, func(message string) {
		if message != "bob: compiling exercise 1" {
			return
		}
		checked = true
		stored, err := app.results.List(context.Background())
		if err != nil || len(stored) != 1 || stored[0].StudentName != "alice" {
			t.Errorf("first student not persisted before second starts: %#v, %v", stored, err)
		}
		if _, err := os.Stat(filepath.Join(root, "out", "static", "alice", "report.html")); err != nil {
			t.Errorf("first student report not published: %v", err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("did not observe second student starting")
	}
}

func TestIncrementalRunPersistsAndInvalidates(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not available")
	}
	root := t.TempDir()
	tests := filepath.Join(root, "tests", "exercise1")
	student := filepath.Join(root, "students", "alice")
	for _, dir := range []string{tests, student} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(student, "ejercicio1.cpp"), "#include <iostream>\nint main(){int n;std::cin >> n;std::cout << n << '\\n';}\n")
	for _, n := range []string{"1", "2"} {
		write(filepath.Join(tests, n+".in.txt"), n)
		write(filepath.Join(tests, n+".out.txt"), n+"\n")
	}
	app, err := openApplication(filepath.Join(root, "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.results.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := service.Config{TestsFolder: filepath.Dir(tests), Students: []models.StudentConfig{{Path: student}}}
	run := func() []models.TestCaseResult {
		t.Helper()
		if err := app.execute(context.Background(), cfg, nil); err != nil {
			t.Fatal(err)
		}
		report, err := app.results.Get(context.Background(), "alice")
		if err != nil {
			t.Fatal(err)
		}
		return report.Groups[0].Results
	}
	initial := run()
	if initial[0].Fingerprint == "" || initial[0].Cached {
		t.Fatalf("initial result: %+v", initial[0])
	}
	cfg.Incremental = true
	reused := run()
	if !reused[0].Cached || !reused[1].Cached || !reused[0].ExecutedAt.Equal(initial[0].ExecutedAt) {
		t.Fatalf("results not reused: %+v", reused)
	}
	write(filepath.Join(tests, "1.in.txt"), "7")
	write(filepath.Join(tests, "1.out.txt"), "7\n")
	changed := run()
	if changed[0].Cached || !changed[1].Cached {
		t.Fatalf("incorrect invalidation: %+v", changed)
	}
	cfg.Incremental = false
	forced := run()
	if forced[0].Cached || forced[1].Cached {
		t.Fatalf("force run reused results: %+v", forced)
	}
}

func TestIncrementalRunOnlyRerunsAffectedExercise(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not available")
	}
	root := t.TempDir()
	student := filepath.Join(root, "students", "alice")
	if err := os.MkdirAll(student, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"1", "2"} {
		dir := filepath.Join(root, "tests", "exercise"+n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(dir, "1.in.txt"), "")
		write(filepath.Join(dir, "1.out.txt"), n)
		write(filepath.Join(student, "ejercicio"+n+".cpp"), "#include <iostream>\nint main(){std::cout << "+n+";}\n")
	}
	app, err := openApplication(filepath.Join(root, "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.results.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := service.Config{TestsFolder: filepath.Join(root, "tests"), Students: []models.StudentConfig{{Path: student}}}
	if err := app.execute(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	cfg.Incremental = true
	write(filepath.Join(student, "ejercicio2.cpp"), "#include <iostream>\nint main(){std::cout << 2;} // changed\n")
	if err := app.execute(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	report, err := app.results.Get(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Groups) != 2 || !report.Groups[0].Results[0].Cached || report.Groups[1].Results[0].Cached {
		t.Fatalf("unrelated exercise was invalidated: %+v", report.Groups)
	}
}
