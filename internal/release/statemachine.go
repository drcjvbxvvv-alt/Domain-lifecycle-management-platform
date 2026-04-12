package release

// validReleaseTransitions defines the complete release state graph.
// Each key maps to the set of valid target states.
// This MUST match CLAUDE.md §"Release State Machine" exactly.
//
//	pending ──→ planning ──→ ready ──→ executing ──→ succeeded
//	   │           │           │          │
//	   ▼           ▼           ▼          ├──→ paused ──→ executing (resume)
//	cancelled  cancelled   cancelled      │              ↓
//	                                      │           rolling_back ──→ rolled_back
//	                                      └──→ failed ──→ rolling_back ──→ rolled_back
var validReleaseTransitions = map[string][]string{
	"pending":      {"planning", "cancelled"},
	"planning":     {"ready", "cancelled", "failed"},
	"ready":        {"executing", "cancelled"},
	"executing":    {"paused", "succeeded", "failed"},
	"paused":       {"executing", "rolling_back", "cancelled"},
	"succeeded":    {},                                         // terminal
	"failed":       {"rolling_back", "cancelled"},
	"rolling_back": {"rolled_back", "failed"},
	"rolled_back":  {},                                         // terminal
	"cancelled":    {},                                         // terminal
}

// AllReleaseStates is the authoritative list of valid release states.
var AllReleaseStates = []string{
	"pending", "planning", "ready", "executing",
	"paused", "succeeded", "failed",
	"rolling_back", "rolled_back", "cancelled",
}

// TerminalReleaseStates are states from which no further transition is possible.
var TerminalReleaseStates = []string{
	"succeeded", "rolled_back", "cancelled",
}

// CanReleaseTransition returns true if the edge from→to exists in the
// release state graph.
func CanReleaseTransition(from, to string) bool {
	targets, ok := validReleaseTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// ValidReleaseTargets returns the set of states reachable from the given state,
// or nil if the state is unknown or terminal.
func ValidReleaseTargets(from string) []string {
	return validReleaseTransitions[from]
}

// IsTerminalReleaseState returns true if the state has no outgoing edges.
func IsTerminalReleaseState(state string) bool {
	targets, ok := validReleaseTransitions[state]
	return ok && len(targets) == 0
}
