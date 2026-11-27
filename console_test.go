package main

import (
	"os"
	"strings"
	"testing"
)

func TestProgressCountsSkippedTests(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "progress")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.Close(); err != nil {
			t.Errorf("close progress output: %v", err)
		}
	})
	p := newRunProgress(output, 3, 2)
	p.start("student compiling")
	p.advance(3)
	p.message("skipping 2 tests\n")
	p.studentFinished("first")
	p.studentFinished("second")
	p.close()
	if p.done != 3 {
		t.Fatalf("progress=%d", p.done)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text,
		"[0/3]") || !strings.Contains(text,
		"skipping 2 tests") || !strings.Contains(text,
		"Students 1/2 (1 pending)") || !strings.Contains(text,
		"Students 2/2 (0 pending)") || strings.Contains(text,
		"\x1b") {
		t.Fatalf("unexpected redirected output: %q", text)
	}
}

func TestProgressConsoleText(t *testing.T) {
	if got := safeConsoleText("student\x1b[2J\nname"); got != "student [2J name" {
		t.Fatalf("unsafe text: %q", got)
	}
}
