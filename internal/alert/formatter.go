package alert

import (
	"fmt"
	"strings"

	"domain-platform/store/postgres"
)

// formatMessage produces the notification subject + body for an alert event.
// Output is plain text suitable for Telegram Markdown and webhook JSON bodies.
func formatMessage(ev *postgres.AlertEvent) (subject, body string) {
	severity := severityLabel(ev.Severity)
	subject = fmt.Sprintf("%s %s", severity, ev.Title)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*[%s]* %s\n", ev.Severity, ev.Title))
	sb.WriteString(fmt.Sprintf("Source: %s", ev.Source))
	if ev.TargetKind != "" {
		sb.WriteString(fmt.Sprintf(" | Target: %s", ev.TargetKind))
		if ev.TargetID != nil {
			sb.WriteString(fmt.Sprintf(" #%d", *ev.TargetID))
		}
	}
	sb.WriteString("\n")
	if len(ev.Detail) > 2 { // non-empty JSON (not just "null" or "{}")
		sb.WriteString(fmt.Sprintf("Detail: %s\n", string(ev.Detail)))
	}
	sb.WriteString(fmt.Sprintf("Alert ID: %d | UUID: %s", ev.ID, ev.UUID))

	body = sb.String()
	return subject, body
}

func severityLabel(s string) string {
	switch s {
	case "P1":
		return "🔴 CRITICAL"
	case "P2":
		return "🟠 ERROR"
	case "P3":
		return "🟡 WARN"
	default:
		return "🔵 INFO"
	}
}
