package templates

import "fmt"

func percent(a, b int) string {
	if b == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(a)*100/float64(b))
}
