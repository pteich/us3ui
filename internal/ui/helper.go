package ui

import (
	"fmt"
	"strings"
)

func byteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

func normalizePrefix(p string) string { return strings.Trim(strings.TrimSpace(p), "/") }

func transferSummaryMessage(action string, completed, total int, failures []string) string {
	if len(failures) == 0 {
		if completed == 1 {
			return fmt.Sprintf("%s 1 file.", action)
		}
		return fmt.Sprintf("%s %d files.", action, completed)
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "%s %d of %d files.", action, completed, total)
	msg.WriteString("\n\nFailed:")

	limit := min(len(failures), 5)
	for _, failure := range failures[:limit] {
		msg.WriteString("\n- ")
		msg.WriteString(failure)
	}
	if len(failures) > limit {
		fmt.Fprintf(&msg, "\n- ... and %d more", len(failures)-limit)
	}

	return msg.String()
}
