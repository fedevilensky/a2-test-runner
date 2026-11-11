package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
)

type timingPoint struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Label string  `json:"label,omitempty"`
}

type complexityModel struct {
	Name string
	Kind int
}

var complexityModels = []complexityModel{{"O(1)", 0}, {"O(log N)", 1}, {"O(√N)", 2}, {"O(N)", 3}, {"O(N log N)", 4}, {"O(N²)", 5}, {"O(N³)", 6}, {"O(2ᴺ)", 7}, {"O(N!)", 8}}

type candidateFit struct {
	Name                string
	RMSE                float64
	model               complexityModel
	maxX, offset, scale float64
}

type complexityFit struct {
	Available       bool
	Best            string
	Note            string
	Count, Distinct int
	Candidates      []candidateFit
	Curve           []timingPoint
}

// shape evaluates each growth function relative to its value at maxX. Log-space
// normalization keeps exponential and factorial curves finite on large inputs.
func (m complexityModel) shape(x, maxX float64) float64 {
	if m.Kind == 0 {
		return 1
	}
	logShape := func(n float64) float64 {
		switch m.Kind {
		case 1:
			if n <= 1 {
				return math.Inf(-1)
			}
			return math.Log(math.Log(n))
		case 2:
			return .5 * math.Log(n)
		case 3:
			return math.Log(n)
		case 4:
			if n <= 1 {
				return math.Inf(-1)
			}
			return math.Log(n) + math.Log(math.Log(n))
		case 5:
			return 2 * math.Log(n)
		case 6:
			return 3 * math.Log(n)
		case 7:
			return n * math.Ln2
		case 8:
			v, _ := math.Lgamma(n + 1)
			return v
		}
		return math.Inf(-1)
	}
	if x <= 0 || maxX <= 0 {
		return 0
	}
	a, b := logShape(x), logShape(maxX)
	if math.IsInf(a, -1) || math.IsInf(b, -1) || math.IsNaN(a-b) {
		return 0
	}
	return math.Exp(a - b)
}

func fitComplexity(points []timingPoint) complexityFit {
	fit := complexityFit{Count: len(points)}
	if len(points) == 0 {
		fit.Note = "No timed cases with a positive input size."
		return fit
	}
	minX, maxX, sumY := points[0].X, points[0].X, 0.0
	unique := make(map[float64]bool)
	for _, p := range points {
		if p.X <= 0 || p.Y < 0 || math.IsNaN(p.X) || math.IsInf(p.X, 0) || math.IsNaN(p.Y) || math.IsInf(p.Y, 0) {
			fit.Note = "Invalid timing data."
			return fit
		}
		minX = math.Min(minX, p.X)
		maxX = math.Max(maxX, p.X)
		sumY += p.Y
		unique[p.X] = true
	}
	fit.Distinct = len(unique)
	meanY := sumY / float64(len(points))
	if math.IsInf(meanY, 0) {
		fit.Note = "Timing data is too large to fit."
		return fit
	}
	for _, model := range complexityModels {
		meanX := 0.0
		for _, p := range points {
			meanX += model.shape(p.X, maxX)
		}
		meanX /= float64(len(points))
		cov, variance := 0.0, 0.0
		for _, p := range points {
			dx := model.shape(p.X, maxX) - meanX
			cov += dx * (p.Y - meanY)
			variance += dx * dx
		}
		intercept, slope := meanY, 0.0
		if variance > 0 && cov > 0 {
			slope = cov / variance
			intercept = meanY - slope*meanX
			if intercept < 0 { // Non-negative fixed overhead; refit through the origin.
				intercept = 0
				xy, xx := 0.0, 0.0
				for _, p := range points {
					x := model.shape(p.X, maxX)
					xy += x * p.Y
					xx += x * x
				}
				if xx > 0 {
					slope = xy / xx
				} else {
					slope = 0
				}
			}
		}
		sumSq := 0.0
		for _, p := range points {
			diff := p.Y - (intercept + slope*model.shape(p.X, maxX))
			sumSq += diff * diff
		}
		rmse := math.Sqrt(sumSq / float64(len(points)))
		if !math.IsNaN(rmse) && !math.IsInf(rmse, 0) {
			fit.Candidates = append(fit.Candidates, candidateFit{Name: model.Name, RMSE: rmse, model: model, maxX: maxX, offset: intercept, scale: slope})
		}
	}
	if len(fit.Candidates) == 0 {
		fit.Note = "Timing data is too large to fit."
		return fit
	}
	sort.SliceStable(fit.Candidates, func(i, j int) bool { return fit.Candidates[i].RMSE < fit.Candidates[j].RMSE })
	best := fit.Candidates[0]
	fit.Available, fit.Best = true, best.Name
	warnings := []string{}
	if fit.Distinct < 5 {
		warnings = append(warnings, "fewer than 5 distinct input sizes")
	}
	if meanY > 0 && best.RMSE/meanY > .25 {
		warnings = append(warnings, "timings are noisy")
	}
	if len(fit.Candidates) > 1 && fit.Candidates[1].RMSE <= best.RMSE*1.1+math.Max(meanY, 1)*1e-9 {
		warnings = append(warnings, "multiple curves fit similarly")
	}
	if len(warnings) > 0 {
		fit.Note = "Low confidence: " + strings.Join(warnings, "; ") + "."
	} else {
		fit.Note = "Heuristic fit; not a proof of complexity."
	}
	// Sampling is restricted to the observed domain; never extrapolate.
	if minX == maxX {
		fit.Curve = append(fit.Curve, timingPoint{X: minX, Y: best.offset + best.scale*best.model.shape(minX, maxX)})
	} else {
		for i := 0; i <= 80; i++ {
			x := minX + (maxX-minX)*float64(i)/80
			fit.Curve = append(fit.Curve, timingPoint{X: x, Y: best.offset + best.scale*best.model.shape(x, maxX)})
		}
	}
	return fit
}

func timingPoints(results []testCaseResult, passingOnly bool) []timingPoint {
	points := make([]timingPoint, 0, len(results))
	for _, r := range results {
		if r.InputSize <= 0 || r.Duration <= 0 || (passingOnly && !r.Passed) {
			continue
		}
		points = append(points, timingPoint{X: float64(r.InputSize), Y: float64(r.Duration) / 1e6, Label: r.CaseName})
	}
	return points
}

func complexityChartConfig(group exerciseGroup) template.JS {
	passing, failing := []timingPoint{}, []timingPoint{}
	for _, r := range group.Results {
		p := timingPoints([]testCaseResult{r}, false)
		if len(p) == 0 {
			continue
		}
		if r.Passed {
			passing = append(passing, p[0])
		} else {
			failing = append(failing, p[0])
		}
	}
	datasets := []any{
		map[string]any{"label": "Passing cases", "data": passing, "backgroundColor": "#238a55", "pointRadius": 5},
		map[string]any{"label": "Failed cases (includes timeouts)", "data": failing, "backgroundColor": "#c04545", "pointRadius": 5},
	}
	for _, item := range []struct {
		label, color string
		fit          complexityFit
		dashed       bool
	}{{"All cases", "#396ac6", group.FitAll, false}, {"Passing only", "#b07716", group.FitPassing, true}} {
		if !item.fit.Available {
			continue
		}
		line := map[string]any{"label": fmt.Sprintf("%s fit: %s", item.label, item.fit.Best), "data": item.fit.Curve, "borderColor": item.color, "backgroundColor": item.color, "showLine": true, "pointRadius": 0, "borderWidth": 2, "fill": false}
		if item.dashed {
			line["borderDash"] = []int{6, 4}
		}
		datasets = append(datasets, line)
	}
	cfg := map[string]any{"type": "scatter", "data": map[string]any{"datasets": datasets}, "options": map[string]any{"plugins": map[string]any{"legend": map[string]any{"display": true}, "title": map[string]any{"display": true, "text": "Input size vs time (ms)"}}, "scales": map[string]any{"x": map[string]any{"title": map[string]any{"display": true, "text": "Input size (from filename)"}, "type": "linear"}, "y": map[string]any{"title": map[string]any{"display": true, "text": "Time (ms)"}, "beginAtZero": true}}}}
	b, err := json.Marshal(cfg)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(b)
}
