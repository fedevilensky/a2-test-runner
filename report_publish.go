package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/a-h/templ"
	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/service"
	"github.com/fedevilensky/a2-test-runner/templates"
)

func reportViews(
	stored []models.StudentReportData,
) ([]models.StudentReportData, models.SummaryReport) {
	views, summary := service.BuildReportViews(stored)
	for i := range views {
		view := &views[i]
		view.ID = sanitizeName(view.StudentName)
		view.IndexPath = "../index.html"
		view.SummaryPath = "../summary/index.html"
		if i > 0 {
			view.PrevName = views[i-1].StudentName
			view.PrevPath = fmt.Sprintf("../%s/report.html", sanitizeName(view.PrevName))
		}
		if i+1 < len(views) {
			view.NextName = views[i+1].StudentName
			view.NextPath = fmt.Sprintf("../%s/report.html", sanitizeName(view.NextName))
		}
	}
	return views, summary
}

func reportComponent(
	stored []models.StudentReportData, studentName string,
) (templ.Component, error) {
	views, summary := reportViews(stored)
	if studentName == "" {
		return templates.IndexReport(models.SPAData{Students: views, Summary: summary}), nil
	}
	for _, view := range views {
		if view.StudentName == studentName {
			view.IndexPath = "/reports"
			view.SummaryPath = "/reports#summary"
			if view.PrevName != "" {
				view.PrevPath = "/reports/" + url.PathEscape(view.PrevName)
			}
			if view.NextName != "" {
				view.NextPath = "/reports/" + url.PathEscape(view.NextName)
			}
			return templates.StudentReportPage(view), nil
		}
	}
	return nil, fmt.Errorf("student %q: %w", studentName, os.ErrNotExist)
}

func publishReports(
	ctx context.Context, staticDir string, stored []models.StudentReportData,
) error {
	return publishUpdatedReports(ctx, staticDir, stored, "")
}

// publishUpdatedReports updates the student's report and its navigation neighbors.
// Empty updatedName rebuilds every report, including on server startup.
func publishUpdatedReports(
	ctx context.Context,
	staticDir string,
	stored []models.StudentReportData,
	updatedName string,
) error {
	views, summary := reportViews(stored)
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		return err
	}
	seen := make(map[string]string)
	for i, view := range views {
		id := sanitizeName(view.StudentName)
		if other, ok := seen[id]; ok && other != view.StudentName {
			return fmt.Errorf(
				"student names %q and %q produce the same report path", other, view.StudentName,
			)
		}
		seen[id] = view.StudentName
		path := filepath.Join(staticDir, id, "report.html")
		needsUpdate := updatedName == "" || view.StudentName == updatedName ||
			view.PrevName == updatedName || view.NextName == updatedName
		if !needsUpdate {
			_, err := os.Stat(path)
			needsUpdate = errors.Is(err, os.ErrNotExist)
			if err != nil && !needsUpdate {
				return err
			}
		}
		if !needsUpdate {
			continue
		}
		if err := writeReportAtomic(ctx, path, templates.StudentReportPage(views[i])); err != nil {
			return err
		}
	}
	if err := writeReportAtomic(
		ctx, filepath.Join(staticDir, "summary", "index.html"), templates.SummaryReportPage(summary),
	); err != nil {
		return err
	}
	index := templates.IndexReport(models.SPAData{Students: views, Summary: summary})
	return writeReportAtomic(ctx, filepath.Join(staticDir, "index.html"), index)
}

func writeReportAtomic(ctx context.Context, path string, component templ.Component) (err error) {
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".report-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = component.Render(ctx, f); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
