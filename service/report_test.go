package service

import (
	"testing"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
)

func TestBuildReportViewsRecalculatesSummaries(t *testing.T) {
	input := []models.StudentReportData{
		{StudentName: "B", Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{FailureType: "source_missing"}}}}},
		{StudentName: "A", Total: 99, Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{Passed: true, InputSize: 2, Duration: time.Millisecond}, {FailureType: "diff_error"}}}}},
	}
	views, summary := BuildReportViews(input)
	if len(views) != 2 || views[0].StudentName != "A" || views[0].Total != 2 || views[0].Passed != 1 || views[0].DiffFails != 1 || !views[0].Groups[0].HasChartData {
		t.Fatalf("unexpected view: %+v", views)
	}
	if len(summary.Rows) != 1 || summary.Rows[0].Total != 2 || summary.Rows[0].Partial != 1 || summary.Rows[0].NotSubmitted != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if input[1].Total != 99 || input[1].Groups[0].HasChartData {
		t.Fatal("modified stored input")
	}
}
