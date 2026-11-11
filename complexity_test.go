package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestFitKnownCurves(t *testing.T) {
	for _, model := range complexityModels {
		t.Run(model.Name, func(t *testing.T) {
			max := 200.0
			if model.Kind >= 7 {
				max = 20
			}
			var points []timingPoint
			for i := 1; i <= 10; i++ {
				x := 2 + (max-2)*float64(i-1)/9
				points = append(points, timingPoint{X: x, Y: 3 + 100*model.shape(x, max)})
			}
			fit := fitComplexity(points)
			if !fit.Available || fit.Best != model.Name {
				t.Fatalf("want %s, got %s (%s)", model.Name, fit.Best, fit.Note)
			}
			if fit.Candidates[0].RMSE > 1e-8 {
				t.Fatalf("unexpected error: %g", fit.Candidates[0].RMSE)
			}
		})
	}
}

func TestFitSparseRepeatedAndFailingCases(t *testing.T) {
	if fit := fitComplexity(nil); fit.Available {
		t.Fatal("empty input should have no fit")
	}
	fit := fitComplexity([]timingPoint{{X: 10, Y: 1}, {X: 10, Y: 2}})
	if !fit.Available || fit.Distinct != 1 || !strings.Contains(fit.Note, "Low confidence") {
		t.Fatalf("bad sparse fit: %+v", fit)
	}
	results := []testCaseResult{{InputSize: 10, Duration: 1500 * time.Microsecond, Passed: true}, {InputSize: 20, Duration: 3 * time.Millisecond, Passed: false}, {InputSize: 30, Duration: 0}, {InputSize: 0, Duration: time.Second}}
	all, passing := timingPoints(results, false), timingPoints(results, true)
	if len(all) != 2 || len(passing) != 1 || passing[0].Y != 1.5 {
		t.Fatalf("wrong filtered points: %+v %+v", all, passing)
	}
	g := exerciseGroup{Results: results, FitAll: fitComplexity(all), FitPassing: fitComplexity(passing)}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(complexityChartConfig(g)), &cfg); err != nil {
		t.Fatal(err)
	}
	sets := cfg["data"].(map[string]any)["datasets"].([]any)
	if len(sets) != 4 {
		t.Fatalf("expected pass, fail, and both fits: %#v", sets)
	}
}

func TestLargeSizesStayFinite(t *testing.T) {
	points := []timingPoint{{X: 10, Y: 1}, {X: 1000000000, Y: 2}, {X: 1e15, Y: 4}}
	fit := fitComplexity(points)
	if !fit.Available || len(fit.Candidates) != len(complexityModels) {
		t.Fatalf("unexpected fit: %+v", fit)
	}
	for _, p := range fit.Curve {
		if math.IsNaN(p.Y) || math.IsInf(p.Y, 0) {
			t.Fatalf("nonfinite curve: %+v", p)
		}
	}
}

func FuzzFitComplexity(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add([]byte{0, 0, 0, 0, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 100 {
			data = data[:100]
		}
		var points []timingPoint
		for i := 0; i+1 < len(data); i += 2 {
			points = append(points, timingPoint{X: float64(data[i]) + 1, Y: float64(data[i+1]) / 10})
		}
		fit := fitComplexity(points)
		if len(points) == 0 {
			if fit.Available {
				t.Fatal("empty input has fit")
			}
			return
		}
		if !fit.Available || len(fit.Candidates) != len(complexityModels) {
			t.Fatalf("missing fit: %+v", fit)
		}
		last := -1.0
		for _, c := range fit.Candidates {
			if math.IsNaN(c.RMSE) || math.IsInf(c.RMSE, 0) || c.RMSE < last {
				t.Fatalf("invalid ranking: %+v", fit.Candidates)
			}
			last = c.RMSE
		}
		for _, p := range fit.Curve {
			if math.IsNaN(p.Y) || math.IsInf(p.Y, 0) || p.Y < 0 {
				t.Fatalf("invalid curve point: %+v", p)
			}
		}
	})
}

func FuzzComplexityLargeInputs(f *testing.F) {
	f.Add(uint64(10), uint64(1<<50), uint64(1<<63))
	f.Add(uint64(1), uint64(1), uint64(1))
	f.Fuzz(func(t *testing.T, a, b, c uint64) {
		points := []timingPoint{
			{X: float64(a>>1) + 1, Y: 1},
			{X: float64(b>>1) + 1, Y: 2},
			{X: float64(c>>1) + 1, Y: 3},
		}
		fit := fitComplexity(points)
		if !fit.Available || len(fit.Candidates) != len(complexityModels) {
			t.Fatalf("missing candidate for large inputs: %+v", fit)
		}
		for _, p := range fit.Curve {
			if math.IsNaN(p.Y) || math.IsInf(p.Y, 0) || p.Y < 0 {
				t.Fatalf("invalid curve point: %+v", p)
			}
		}
	})
}
