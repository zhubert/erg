package workflow

import (
	"strings"
	"unicode"
)

// PhaseLabel returns a human-readable label for a workflow phase.
func PhaseLabel(phase string) string {
	switch phase {
	case "async_pending":
		return "In Progress"
	case "addressing_feedback":
		return "Addressing Feedback"
	case "docker_pending":
		return "Starting Container"
	case "pushing":
		return "Pushing"
	case "retry_pending":
		return "Retrying"
	case "idle", "":
		return "Idle"
	default:
		return toTitleCase(strings.ReplaceAll(phase, "_", " "))
	}
}

// StepLabel returns a human-readable fallback label for a step name when no
// explicit DisplayName is available. It strips template namespace prefixes
// (e.g. "_t_plan_") and humanizes underscores.
func StepLabel(step string) string {
	if step == "" {
		return "—"
	}
	// Strip "_t_<prefix>_" namespace from template-expanded states.
	// Format: _t_ + sanitized-template-name + _ + original-state-name
	if strings.HasPrefix(step, "_t_") {
		rest := step[3:] // drop "_t_"
		if i := strings.Index(rest, "_"); i >= 0 {
			step = rest[i+1:]
		}
	}
	return toTitleCase(strings.ReplaceAll(step, "_", " "))
}

// toTitleCase capitalizes the first letter of each word.
func toTitleCase(s string) string {
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
