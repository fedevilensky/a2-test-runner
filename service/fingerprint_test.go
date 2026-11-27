package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
)

func TestIncrementalCasesAndInvalidation(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ unavailable")
	}
	root := t.TempDir()
	student := filepath.Join(root, "student")
	tests := filepath.Join(root, "tests", "exercise1")
	for _, d := range []string{student, tests} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) { writeFingerprintFile(t, path, content) }
	write(filepath.Join(student, "ejercicio1.cpp"),
		"#include \"value.h\"\n#include <iostream>\nint main(){std::cout << VALUE;}\n")
	write(filepath.Join(student, "value.h"), "#define VALUE 1\n")
	for _, name := range []string{"1", "2"} {
		write(filepath.Join(tests, name+".in.txt"), "")
		write(filepath.Join(tests, name+".out.txt"), "1")
	}
	cfg := Config{
		TestsFolder: filepath.Dir(tests),
		Students:    []models.StudentConfig{{Path: student}}, Incremental: true,
	}
	run := func() models.StudentReportData {
		t.Helper()
		reports, err := Run(context.Background(), cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		return reports[0]
	}
	first := run()
	if !first.Groups[0].Results[0].Passed || first.Groups[0].Results[0].Fingerprint == "" {
		t.Fatalf("not cacheable: %+v", first)
	}
	cfg.Cached = map[string]models.StudentReportData{"student": first}
	second := run()
	if !second.Groups[0].Results[0].Cached || !second.Groups[0].Results[1].Cached {
		t.Fatal("unchanged cases not cached")
	}
	if !second.Groups[0].Results[0].ExecutedAt.Equal(first.Groups[0].Results[0].ExecutedAt) {
		t.Fatal("timestamp changed")
	}
	write(filepath.Join(student, "unrelated.txt"), "unrelated change")
	write(filepath.Join(student, "ejercicio2.cpp"), "int main(){return 1;}")
	unrelated := run()
	if !unrelated.Groups[0].Results[0].Cached || !unrelated.Groups[0].Results[1].Cached {
		t.Fatal("unrelated files invalidated exercise 1")
	}
	changed := false
	cfg.OnProgress = func(p models.RunProgress) {
		if p.CachedCases > 0 && !changed {
			changed = true
			write(filepath.Join(tests, "1.in.txt"), "changed during run")
		}
	}
	if _, err := Run(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected changed cached input to abort publication")
	}
	cfg.OnProgress = nil
	write(filepath.Join(tests, "1.in.txt"), "")
	write(filepath.Join(tests, "2.out.txt"), "different")
	third := run()
	if !third.Groups[0].Results[0].Cached ||
		third.Groups[0].Results[1].Cached ||
		third.Groups[0].Results[1].Passed {
		t.Fatalf("changed expected output: %+v", third.Groups[0].Results)
	}
	cfg.Cached["student"] = third
	retry := run()
	if retry.Groups[0].Results[1].Cached {
		t.Fatal("failed result reused")
	}
	cfg.Cached["student"] = first
	write(filepath.Join(tests, "1.in.txt"), "changed")
	if run().Groups[0].Results[0].Cached {
		t.Fatal("input change must invalidate")
	}
	write(filepath.Join(tests, "1.in.txt"), "")
	cfg.Build.Optimization = "2"
	if run().Groups[0].Results[0].Cached {
		t.Fatal("flags change must invalidate")
	}
	cfg.Build.Optimization = ""
	cfg.Timeout = time.Second
	if run().Groups[0].Results[0].Cached {
		t.Fatal("timeout change must invalidate")
	}
	cfg.Timeout = 0
	cfg.Incremental = false
	if run().Groups[0].Results[0].Cached {
		t.Fatal("forced run reused result")
	}
	cfg.Incremental = true
	write(filepath.Join(student, "value.h"), "#define VALUE 2\n")
	if run().Groups[0].Results[0].Cached {
		t.Fatal("header change must invalidate")
	}
}

func writeFingerprintFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintDisablesUnknownDependencies(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ unavailable")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "ejercicio1.cpp")
	if err := os.WriteFile(src, []byte("#include \"../external.h\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fingerprintExercise(context.Background(),
		dir,
		1,
		[]string{"-std=c++11"},
		1,
		"runner"); got != "" {
		t.Fatal("external include was cacheable")
	}
	if err := os.WriteFile(src, []byte("int main(){}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fingerprintExercise(context.Background(),
		dir,
		1,
		[]string{
			"-std=c++11",
			"-I/tmp",
		},
		1,
		"runner"); got == "" {
		t.Fatal("unused include path should not disable caching")
	}
	t.Setenv("CPATH", "/tmp")
	if got := fingerprintExercise(context.Background(),
		dir,
		1,
		[]string{"-std=c++11"},
		1,
		"runner"); got != "" {
		t.Fatal("external include environment was cacheable")
	}
	t.Setenv("CPATH", "")
	if err := os.Symlink(src, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if got := fingerprintExercise(context.Background(),
		dir,
		1,
		[]string{"-std=c++11"},
		1,
		"runner"); got == "" {
		t.Fatal("unrelated symlink should not disable caching")
	}
}

func TestIncrementalJava(t *testing.T) {
	for _, tool := range []string{"java", "javac"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable", tool)
		}
	}
	root := t.TempDir()
	student := filepath.Join(root, "student")
	tests := filepath.Join(root, "tests", "exercise1")
	for _, d := range []string{student, tests} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(student,
		"Ejercicio1.java"),
		"public class Ejercicio1 { public static void main(String[] args) { "+
			"System.out.print(Helper.value()); } }")
	write(filepath.Join(student, "Helper.java"), "class Helper { static int value() { return 7; } }")
	write(filepath.Join(tests, "1.in.txt"), "")
	write(filepath.Join(tests, "1.out.txt"), "7")
	cfg := Config{
		TestsFolder: filepath.Dir(tests),
		Students:    []models.StudentConfig{{Path: student}},
		Incremental: true,
	}
	run := func() models.StudentReportData {
		t.Helper()
		reports, err := Run(context.Background(), cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		return reports[0]
	}
	first := run()
	if !first.Groups[0].Results[0].Passed || first.Groups[0].Results[0].Fingerprint == "" {
		t.Fatalf("not cacheable: %+v", first)
	}
	cfg.Incremental = true
	cfg.Cached = map[string]models.StudentReportData{"student": first}
	if !run().Groups[0].Results[0].Cached {
		t.Fatal("Java result not reused")
	}
	write(filepath.Join(student, "Ejercicio2.java"), "class Ejercicio2 { int value = 10; }")
	if !run().Groups[0].Results[0].Cached {
		t.Fatal("unrelated Java source invalidated exercise 1")
	}
	write(filepath.Join(student, "Helper.java"), "class Helper { static int value() { return 8; } }")
	if run().Groups[0].Results[0].Cached {
		t.Fatal("Java dependency change must invalidate")
	}
}

func TestRunnerIdentityInvalidatesFingerprint(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ unavailable")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir,
		"ejercicio1.cpp"),
		[]byte("int main(){}"),
		0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++11"}
	a := fingerprintExercise(context.Background(), dir, 1, args, time.Second, "runner-a")
	b := fingerprintExercise(context.Background(), dir, 1, args, time.Second, "runner-b")
	missing := fingerprintExercise(context.Background(), dir, 1, args, time.Second, "")
	if a == "" || b == "" || a == b || missing != "" || fingerprintRunner() == "" {
		t.Fatal("runner identity not applied")
	}
}
