package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanReleaseTransition_ValidEdges(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// Happy path
		{"pending → planning", "pending", "planning", true},
		{"planning → ready", "planning", "ready", true},
		{"ready → executing", "ready", "executing", true},
		{"executing → succeeded", "executing", "succeeded", true},
		{"executing → paused", "executing", "paused", true},
		{"executing → failed", "executing", "failed", true},
		{"paused → executing", "paused", "executing", true},
		{"paused → rolling_back", "paused", "rolling_back", true},
		{"paused → cancelled", "paused", "cancelled", true},
		{"failed → rolling_back", "failed", "rolling_back", true},
		{"failed → cancelled", "failed", "cancelled", true},
		{"rolling_back → rolled_back", "rolling_back", "rolled_back", true},
		{"rolling_back → failed", "rolling_back", "failed", true},

		// Cancel from early states
		{"pending → cancelled", "pending", "cancelled", true},
		{"planning → cancelled", "planning", "cancelled", true},
		{"ready → cancelled", "ready", "cancelled", true},

		// Planning can fail
		{"planning → failed", "planning", "failed", true},

		// Invalid edges
		{"pending → executing (skip)", "pending", "executing", false},
		{"pending → succeeded", "pending", "succeeded", false},
		{"ready → succeeded (skip)", "ready", "succeeded", false},
		{"succeeded → pending (terminal)", "succeeded", "pending", false},
		{"rolled_back → pending (terminal)", "rolled_back", "pending", false},
		{"cancelled → pending (terminal)", "cancelled", "pending", false},
		{"executing → ready (backward)", "executing", "ready", false},
		{"unknown → planning", "unknown", "planning", false},

		// Cannot go backwards
		{"planning → pending", "planning", "pending", false},
		{"ready → planning", "ready", "planning", false},
		{"succeeded → executing", "succeeded", "executing", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanReleaseTransition(tt.from, tt.to)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidReleaseTargets(t *testing.T) {
	// Terminal states have no targets
	assert.Empty(t, ValidReleaseTargets("succeeded"))
	assert.Empty(t, ValidReleaseTargets("rolled_back"))
	assert.Empty(t, ValidReleaseTargets("cancelled"))

	// Non-terminal states have targets
	assert.NotEmpty(t, ValidReleaseTargets("pending"))
	assert.NotEmpty(t, ValidReleaseTargets("executing"))

	// Unknown state returns nil
	assert.Nil(t, ValidReleaseTargets("bogus"))
}

func TestIsTerminalReleaseState(t *testing.T) {
	assert.True(t, IsTerminalReleaseState("succeeded"))
	assert.True(t, IsTerminalReleaseState("rolled_back"))
	assert.True(t, IsTerminalReleaseState("cancelled"))

	assert.False(t, IsTerminalReleaseState("pending"))
	assert.False(t, IsTerminalReleaseState("executing"))
	assert.False(t, IsTerminalReleaseState("failed"))
	assert.False(t, IsTerminalReleaseState("paused"))
}

func TestAllReleaseStates_Complete(t *testing.T) {
	// Every state in AllReleaseStates must be a key in validReleaseTransitions
	for _, state := range AllReleaseStates {
		_, ok := validReleaseTransitions[state]
		assert.True(t, ok, "state %q missing from validReleaseTransitions", state)
	}
	// And vice versa
	assert.Equal(t, len(AllReleaseStates), len(validReleaseTransitions),
		"AllReleaseStates and validReleaseTransitions must have the same count")
}

func TestReleaseStateGraph_Completeness(t *testing.T) {
	// Every target must also be a valid state (no dangling edges)
	for from, targets := range validReleaseTransitions {
		for _, to := range targets {
			_, ok := validReleaseTransitions[to]
			assert.True(t, ok, "state %q transitions to %q which is not a valid state", from, to)
		}
	}
}
