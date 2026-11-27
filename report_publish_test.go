package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fedevilensky/a2-test-runner/models"
)

func TestPublishUpdatedReportsOnlyRewritesChangedStudentAndNeighbors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	reports := []models.StudentReportData{
		{StudentName: "alice"},
		{StudentName: "bob"},
		{StudentName: "charlie"},
		{StudentName: "dana"},
	}
	if err := publishReports(ctx, dir, reports); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "alice", "report.html")
	if err := os.WriteFile(path, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishUpdatedReports(ctx, dir, reports, "dana"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "untouched" {
		t.Fatalf("unrelated report rewritten: %q, %v", content, err)
	}
	neighbor, err := os.ReadFile(filepath.Join(dir, "charlie", "report.html"))
	if err != nil || !strings.Contains(string(neighbor), "dana") {
		t.Fatalf("missing neighbor navigation: %v", err)
	}
}
