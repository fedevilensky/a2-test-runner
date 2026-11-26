package models

import "time"

type OutputLine struct {
	Text    string
	Changed bool
}
type TestCaseResult struct {
	ExecutedAt                 time.Time
	Fingerprint                string
	Cached                     bool
	BuildFlags                 []string
	InputFile                  string
	InputSize                  int
	Duration                   time.Duration
	Passed                     bool
	FailureType                string
	Message, Expected, Actual  string
	ExpectedLines, ActualLines []OutputLine
	HasDiffLines               bool
}
type Candidate struct {
	Name string
	RMSE float64
}
type Fit struct {
	Available       bool
	Best, Note      string
	Count, Distinct int
	Candidates      []Candidate
}
type ExerciseGroup struct {
	Exercise           int
	Results            []TestCaseResult
	Passed, Failed     int
	HasChartData       bool
	FitAll, FitPassing Fit
	ChartConfig        string
}
type StudentReportData struct {
	ID, StudentName, StudentPath                                   string
	StartedAt, FinishedAt                                          time.Time
	Total, Passed, Failed, CompileFails, RuntimeFails, DiffFails   int
	Groups                                                         []ExerciseGroup
	IndexPath, SummaryPath, PrevName, PrevPath, NextName, NextPath string
}
type (
	SummaryRow    struct{ Exercise, Total, AllPass, Partial, AllFail, CompileFail, NotSubmitted int }
	SummaryReport struct {
		GeneratedAt time.Time
		Rows        []SummaryRow
	}
)
type SPAData struct {
	Students []StudentReportData
	Summary  SummaryReport
}

// ResultSelection identifies exercises and optionally case input filenames to replace.
// An empty case list selects the entire exercise.
type ResultSelection struct {
	Exercises map[int][]string
}
