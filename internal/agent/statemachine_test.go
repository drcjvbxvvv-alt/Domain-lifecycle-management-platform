package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanAgentTransition_ValidEdges(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// registered
		{"registered→online", "registered", "online", true},
		{"registered→offline (invalid)", "registered", "offline", false},

		// online
		{"online→busy", "online", "busy", true},
		{"online→idle", "online", "idle", true},
		{"online→offline", "online", "offline", true},
		{"online→draining", "online", "draining", true},
		{"online→disabled", "online", "disabled", true},
		{"online→upgrading", "online", "upgrading", true},
		{"online→error", "online", "error", true},
		{"online→registered (invalid)", "online", "registered", false},

		// busy
		{"busy→online", "busy", "online", true},
		{"busy→offline", "busy", "offline", true},
		{"busy→error", "busy", "error", true},
		{"busy→idle (invalid)", "busy", "idle", false},

		// idle
		{"idle→online", "idle", "online", true},
		{"idle→busy", "idle", "busy", true},
		{"idle→offline", "idle", "offline", true},
		{"idle→draining", "idle", "draining", true},
		{"idle→disabled", "idle", "disabled", true},
		{"idle→error", "idle", "error", true},

		// offline
		{"offline→online", "offline", "online", true},
		{"offline→error", "offline", "error", true},
		{"offline→busy (invalid)", "offline", "busy", false},

		// draining
		{"draining→disabled", "draining", "disabled", true},
		{"draining→offline", "draining", "offline", true},
		{"draining→error", "draining", "error", true},
		{"draining→online (invalid)", "draining", "online", false},

		// disabled
		{"disabled→online", "disabled", "online", true},
		{"disabled→busy (invalid)", "disabled", "busy", false},

		// upgrading
		{"upgrading→online", "upgrading", "online", true},
		{"upgrading→error", "upgrading", "error", true},
		{"upgrading→busy (invalid)", "upgrading", "busy", false},

		// error
		{"error→online", "error", "online", true},
		{"error→disabled", "error", "disabled", true},
		{"error→busy (invalid)", "error", "busy", false},

		// unknown
		{"unknown→online", "unknown", "online", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanAgentTransition(tt.from, tt.to)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidAgentTargets(t *testing.T) {
	targets := ValidAgentTargets("online")
	assert.Contains(t, targets, "busy")
	assert.Contains(t, targets, "idle")
	assert.Contains(t, targets, "offline")
	assert.Contains(t, targets, "draining")
	assert.Contains(t, targets, "disabled")
	assert.Contains(t, targets, "upgrading")
	assert.Contains(t, targets, "error")
	assert.Len(t, targets, 7)

	targets = ValidAgentTargets("registered")
	assert.Equal(t, []string{"online"}, targets)

	targets = ValidAgentTargets("unknown_state")
	assert.Nil(t, targets)
}

func TestIsTerminalAgentState(t *testing.T) {
	// No agent state is terminal in the current design
	for _, state := range AllAgentStates {
		assert.False(t, IsTerminalAgentState(state), "state %q should not be terminal", state)
	}
}

func TestAllAgentStates_CoverGraph(t *testing.T) {
	// Every state in AllAgentStates must be a key in validAgentTransitions
	for _, state := range AllAgentStates {
		_, ok := validAgentTransitions[state]
		assert.True(t, ok, "state %q is in AllAgentStates but not in validAgentTransitions", state)
	}

	// Every key in validAgentTransitions must be in AllAgentStates
	for state := range validAgentTransitions {
		found := false
		for _, s := range AllAgentStates {
			if s == state {
				found = true
				break
			}
		}
		assert.True(t, found, "state %q is in validAgentTransitions but not in AllAgentStates", state)
	}
}

func TestAllAgentStates_TargetsCoverGraph(t *testing.T) {
	// Every target state in every edge must exist as a key in the graph
	for from, targets := range validAgentTransitions {
		for _, to := range targets {
			_, ok := validAgentTransitions[to]
			assert.True(t, ok, "edge %s→%s: target %q is not a known state", from, to, to)
		}
	}
}
