package agent

// validAgentTransitions defines the complete agent state graph.
// Each key maps to the set of valid target states.
// This MUST match CLAUDE.md §"Agent State Machine" exactly.
//
//	registered ──→ online ──┬──→ busy ──→ online
//	                        ├──→ idle ──→ online
//	                        ├──→ draining ──→ disabled
//	                        ├──→ upgrading ──→ online / error
//	                        └──→ offline ──┬──→ online
//	                                       └──→ error
//	disabled ──→ online
//	error    ──→ online / disabled
var validAgentTransitions = map[string][]string{
	"registered": {"online"},
	"online":     {"busy", "idle", "offline", "draining", "disabled", "upgrading", "error"},
	"busy":       {"online", "offline", "error"},
	"idle":       {"online", "busy", "offline", "draining", "disabled", "error"},
	"offline":    {"online", "error"},
	"draining":   {"disabled", "offline", "error"},
	"disabled":   {"online"},
	"upgrading":  {"online", "error"},
	"error":      {"online", "disabled"},
}

// AllAgentStates is the authoritative list of valid agent states.
var AllAgentStates = []string{
	"registered", "online", "busy", "idle", "offline",
	"draining", "disabled", "upgrading", "error",
}

// CanAgentTransition returns true if the edge from→to exists in the
// agent state graph.
func CanAgentTransition(from, to string) bool {
	targets, ok := validAgentTransitions[from]
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

// ValidAgentTargets returns the set of states reachable from the given state,
// or nil if the state is unknown or terminal.
func ValidAgentTargets(from string) []string {
	return validAgentTransitions[from]
}

// IsTerminalAgentState returns true if the given state has no outgoing edges.
// In the current agent state machine, no state is truly terminal (even error
// and disabled can transition back to online). This function exists for
// consistency with lifecycle/release state machines.
func IsTerminalAgentState(state string) bool {
	targets, ok := validAgentTransitions[state]
	return ok && len(targets) == 0
}
