package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/service"
	"github.com/fedevilensky/a2-test-runner/templates"
	"golang.org/x/net/html"
)

func TestReportComponents(t *testing.T) {
	result := models.TestCaseResult{InputFile: "<case>", InputSize: 10, Duration: time.Second, Expected: "<expected>", Actual: "<actual>", HasDiffLines: true, ExpectedLines: []models.OutputLine{{Text: "<expected>", Changed: true}}, ActualLines: []models.OutputLine{{Text: "<actual>", Changed: true}}}
	group := models.ExerciseGroup{Exercise: 3, Results: []models.TestCaseResult{result}, Failed: 1, HasChartData: true}
	student := models.StudentReportData{StudentName: `A "<script>`, Total: 1, Failed: 1, Groups: []models.ExerciseGroup{group}, IndexPath: "../index.html", SummaryPath: "../summary/index.html"}
	views, _ := service.BuildReportViews([]models.StudentReportData{student})
	student = views[0]
	summary := models.SummaryReport{Rows: []models.SummaryRow{{Exercise: 3, Total: 1, AllFail: 1}}}
	for _, tc := range []struct {
		name   string
		render func(*bytes.Buffer) error
		chart  bool
	}{
		{"student", func(b *bytes.Buffer) error {
			return templates.StudentReportPage(student).Render(context.Background(), b)
		}, true},
		{"index", func(b *bytes.Buffer) error {
			return templates.IndexReport(models.SPAData{Students: []models.StudentReportData{student}, Summary: summary}).Render(context.Background(), b)
		}, true},
		{"summary", func(b *bytes.Buffer) error {
			return templates.SummaryReportPage(summary).Render(context.Background(), b)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := tc.render(&b); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(&b)
			if err != nil {
				t.Fatal(err)
			}
			var content strings.Builder
			foundChart, foundDiff := false, false
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data != "script" && n.Parent.Data != "style" {
					content.WriteString(n.Data)
				}
				if n.Type == html.ElementNode {
					if n.Data == "script" {
						for _, a := range n.Attr {
							if a.Key == "data-injected" {
								t.Error("injected script")
							}
						}
					}
					for _, a := range n.Attr {
						if a.Key == "class" && strings.Contains(a.Val, "diff-removed") {
							foundDiff = true
						}
						if n.Data == "canvas" && a.Key == "data-chart" {
							var cfg map[string]any
							if err := json.Unmarshal([]byte(a.Val), &cfg); err != nil {
								t.Errorf("invalid chart JSON: %v", err)
							}
							foundChart = true
						}
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(doc)
			if tc.chart && (!strings.Contains(content.String(), "<expected>") || !strings.Contains(content.String(), "<actual>") || !foundChart || !foundDiff) {
				t.Fatalf("missing escaped output, diff, or chart: chart=%v diff=%v text=%q", foundChart, foundDiff, content.String())
			}
			if !strings.Contains(content.String(), "Summary") {
				t.Error("missing summary navigation or heading")
			}
		})
	}
}
