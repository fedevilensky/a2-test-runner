package components

import (
	"fmt"
	"time"
)

func formatDuration(d time.Duration) string { return fmt.Sprintf("%.3fs", d.Seconds()) }
func diffLineClass(changed bool, kind string) string {
	if changed {
		return "diff-line diff-" + kind
	}
	return "diff-line"
}

func summaryTableClass(standalone bool) string {
	if standalone {
		return "summary-table"
	}
	return ""
}
