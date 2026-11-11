package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestProgressCountsSkippedTests(t *testing.T) {
	student := t.TempDir()
	output, err := os.CreateTemp(t.TempDir(), "progress")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	p := newRunProgress(output, 3, 2)
	cases := []testCase{{name: "one", inputPath: "one.in.txt"}, {name: "two", inputPath: "two.in.txt"}}
	report, err := runForStudent(student, map[int][]testCase{1: cases}, []int{1}, time.Second, p)
	p.studentFinished("first")
	p.studentFinished("second")
	p.close()
	if err != nil {
		t.Fatal(err)
	}
	if p.done != 3 || report.MissingFails != 2 {
		t.Fatalf("progress=%d, missing failures=%d", p.done, report.MissingFails)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[0/3]") || !strings.Contains(text, "skipping 2 tests") || !strings.Contains(text, "Students 1/2 (1 pending)") || !strings.Contains(text, "Students 2/2 (0 pending)") || strings.Contains(text, "\x1b") {
		t.Fatalf("unexpected redirected output: %q", text)
	}
}

func TestProgressConsoleText(t *testing.T) {
	if got := safeConsoleText("student\x1b[2J\nname"); got != "student [2J name" {
		t.Fatalf("unsafe text: %q", got)
	}
}
