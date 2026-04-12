package lifecycle

// validLifecycleTransitions defines the complete domain lifecycle graph.
// Each key maps to the set of valid target states.
// This MUST match CLAUDE.md §"Domain Lifecycle State Machine" exactly.
//
//	requested ──→ approved ──→ provisioned ──→ active ──→ disabled
//	                                              │           │
//	                                              │           ▼
//	                                              │       active (re-enable)
//	                                              ▼
//	                                            retired (terminal)
var validLifecycleTransitions = map[string][]string{
	"requested":   {"approved", "retired"},
	"approved":    {"provisioned", "retired"},
	"provisioned": {"active", "disabled", "retired"},
	"active":      {"disabled", "retired"},
	"disabled":    {"active", "retired"},
	"retired":     {}, // terminal — no outgoing edges
}

// AllLifecycleStates is the authoritative list of valid lifecycle states.
var AllLifecycleStates = []string{
	"requested", "approved", "provisioned", "active", "disabled", "retired",
}

// CanLifecycleTransition returns true if the edge from→to exists in the
// lifecycle graph.
func CanLifecycleTransition(from, to string) bool {
	targets, ok := validLifecycleTransitions[from]
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

// ValidTargets returns the set of states reachable from the given state,
// or nil if the state is unknown or terminal.
func ValidTargets(from string) []string {
	return validLifecycleTransitions[from]
}
