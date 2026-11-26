package models

// BuildSettings configures the C++ compiler. Empty fields inherit global values.
// Extra is nil to inherit or a non-nil slice to replace global extra arguments.
type BuildSettings struct {
	Standard     string
	Optimization string
	Extra        []string
}

type StudentConfig struct {
	Path  string
	Build BuildSettings
}

// AvailableExercise describes an exercise folder and its input cases.
type AvailableExercise struct {
	Number int      `json:"number"`
	Folder string   `json:"folder"`
	Cases  []string `json:"cases"`
}

// RunProgress reports work completed for the current execution.
type RunProgress struct {
	TotalUnits        int    `json:"total_units"`
	CompletedUnits    int    `json:"completed_units"`
	TotalStudents     int    `json:"total_students"`
	CompletedStudents int    `json:"completed_students"`
	CachedCases       int    `json:"cached_cases"`
	ExecutedCases     int    `json:"executed_cases"`
	Activity          string `json:"activity"`
}
