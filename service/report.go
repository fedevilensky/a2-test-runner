package service

import (
	"sort"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
)

// BuildReportViews derives summaries and chart data from current stored cases.
func BuildReportViews(stored []models.StudentReportData) ([]models.StudentReportData, models.SummaryReport) {
	views := append([]models.StudentReportData(nil), stored...)
	sort.Slice(views, func(i, j int) bool { return views[i].StudentName < views[j].StudentName })
	rows := map[int]*models.SummaryRow{}
	for i := range views {
		view := &views[i]
		view.Total = 0
		view.Passed = 0
		view.Failed = 0
		view.CompileFails = 0
		view.RuntimeFails = 0
		view.DiffFails = 0
		view.Groups = append([]models.ExerciseGroup(nil), view.Groups...)
		for j := range view.Groups {
			group := &view.Groups[j]
			group.Passed = 0
			group.Failed = 0
			group.HasChartData = false
			row := rows[group.Exercise]
			if row == nil {
				row = &models.SummaryRow{Exercise: group.Exercise}
				rows[group.Exercise] = row
			}
			row.Total++
			missing, compileFail := false, false
			for _, r := range group.Results {
				view.Total++
				if r.Passed {
					view.Passed++
					group.Passed++
				} else {
					view.Failed++
					group.Failed++
				}
				switch r.FailureType {
				case "source_missing":
					missing = true
				case "compilation_error":
					compileFail = true
					view.CompileFails++
				case "runtime_error":
					view.RuntimeFails++
				case "diff_error":
					view.DiffFails++
				}
				if r.InputSize > 0 && r.Duration > 0 {
					group.HasChartData = true
				}
			}
			switch {
			case missing:
				row.NotSubmitted++
			case compileFail:
				row.CompileFail++
			case group.Passed == len(group.Results):
				row.AllPass++
			case group.Passed == 0:
				row.AllFail++
			default:
				row.Partial++
			}
			all := fitComplexity(timingPoints(group.Results, false))
			passing := fitComplexity(timingPoints(group.Results, true))
			group.FitAll = modelFit(all)
			group.FitPassing = modelFit(passing)
			group.ChartConfig = ""
			if group.HasChartData {
				group.ChartConfig = complexityChartConfig(*group, all, passing)
			}
		}
	}
	summary := models.SummaryReport{GeneratedAt: time.Now()}
	for _, row := range rows {
		summary.Rows = append(summary.Rows, *row)
	}
	sort.Slice(summary.Rows, func(i, j int) bool { return summary.Rows[i].Exercise < summary.Rows[j].Exercise })
	return views, summary
}
func modelFit(f complexityFit) models.Fit {
	result := models.Fit{Available: f.Available, Best: f.Best, Note: f.Note, Count: f.Count, Distinct: f.Distinct}
	for _, candidate := range f.Candidates {
		result.Candidates = append(result.Candidates, models.Candidate{Name: candidate.Name, RMSE: candidate.RMSE})
	}
	return result
}
