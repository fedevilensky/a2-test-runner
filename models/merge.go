package models

import (
	"errors"
	"fmt"
)

// MergeResults combines a partial run with the previous report.
func MergeResults(
	old, incoming StudentReportData, selection ResultSelection,
) (StudentReportData, error) {
	if err := validateSelection(incoming, selection); err != nil {
		return StudentReportData{}, err
	}
	if old.StudentName == "" {
		old = incoming
		old.Groups = nil
	} else {
		old.Groups = append([]ExerciseGroup(nil), old.Groups...)
		for i := range old.Groups {
			old.Groups[i].Results = append([]TestCaseResult(nil), old.Groups[i].Results...)
		}
	}
	for _, group := range incoming.Groups {
		selected, ok := selection.Exercises[group.Exercise]
		if !ok {
			continue
		}
		index := -1
		for i := range old.Groups {
			if old.Groups[i].Exercise == group.Exercise {
				index = i
				break
			}
		}
		if len(selected) == 0 {
			if index < 0 {
				old.Groups = append(old.Groups, group)
			} else {
				old.Groups[index] = group
			}
			continue
		}
		var previous ExerciseGroup
		if index >= 0 {
			previous = old.Groups[index]
		} else {
			previous = group
			previous.Results = nil
		}
		previous = mergeCases(previous, group, selected)
		if index < 0 {
			old.Groups = append(old.Groups, previous)
		} else {
			old.Groups[index] = previous
		}
	}
	old.StartedAt, old.FinishedAt = incoming.StartedAt, incoming.FinishedAt
	old.StudentPath = incoming.StudentPath
	recountReport(&old)
	return old, nil
}

func validateSelection(incoming StudentReportData, selection ResultSelection) error {
	if len(selection.Exercises) == 0 {
		return errors.New("partial rerun must select at least one exercise")
	}
	groups := make(map[int]ExerciseGroup, len(incoming.Groups))
	for _, group := range incoming.Groups {
		if _, exists := groups[group.Exercise]; exists {
			return fmt.Errorf("duplicate exercise %d", group.Exercise)
		}
		groups[group.Exercise] = group
	}
	for exercise, names := range selection.Exercises {
		group, exists := groups[exercise]
		if !exists {
			return fmt.Errorf("selected exercise %d has no result", exercise)
		}
		present := make(map[string]bool, len(group.Results))
		for _, c := range group.Results {
			if present[c.InputFile] {
				return fmt.Errorf("duplicate case %q in exercise %d", c.InputFile, exercise)
			}
			present[c.InputFile] = true
		}
		for _, name := range names {
			if !present[name] {
				return fmt.Errorf("selected case %q in exercise %d has no result", name, exercise)
			}
		}
	}
	return nil
}

func mergeCases(previous, incoming ExerciseGroup, selected []string) ExerciseGroup {
	allowed := make(map[string]bool, len(selected))
	for _, name := range selected {
		allowed[name] = true
	}
	for _, c := range incoming.Results {
		if !allowed[c.InputFile] {
			continue
		}
		found := false
		for i, old := range previous.Results {
			if old.InputFile == c.InputFile {
				previous.Results[i] = c
				found = true
				break
			}
		}
		if !found {
			previous.Results = append(previous.Results, c)
		}
	}
	previous.HasChartData = false
	previous.ChartConfig = ""
	previous.FitAll, previous.FitPassing = Fit{}, Fit{}
	previous.Passed, previous.Failed = 0, 0
	for _, c := range previous.Results {
		if c.Passed {
			previous.Passed++
		} else {
			previous.Failed++
		}
	}
	return previous
}

func recountReport(report *StudentReportData) {
	report.Total, report.Passed, report.Failed = 0, 0, 0
	report.CompileFails, report.RuntimeFails, report.DiffFails = 0, 0, 0
	for _, group := range report.Groups {
		for _, c := range group.Results {
			report.Total++
			if c.Passed {
				report.Passed++
				continue
			}
			report.Failed++
			switch c.FailureType {
			case "compilation_error":
				report.CompileFails++
			case "runtime_error":
				report.RuntimeFails++
			case "diff_error":
				report.DiffFails++
			}
		}
	}
}
