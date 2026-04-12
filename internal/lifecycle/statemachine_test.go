package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanLifecycleTransition_ValidEdges(t *testing.T) {
	// Every edge defined in CLAUDE.md §"Domain Lifecycle State Machine"
	validEdges := []struct {
		from, to string
	}{
		{"requested", "approved"},
		{"requested", "retired"},
		{"approved", "provisioned"},
		{"approved", "retired"},
		{"provisioned", "active"},
		{"provisioned", "disabled"},
		{"provisioned", "retired"},
		{"active", "disabled"},
		{"active", "retired"},
		{"disabled", "active"},
		{"disabled", "retired"},
	}
	for _, e := range validEdges {
		assert.True(t, CanLifecycleTransition(e.from, e.to),
			"expected valid: %s → %s", e.from, e.to)
	}
}

func TestCanLifecycleTransition_InvalidEdges(t *testing.T) {
	invalidEdges := []struct {
		from, to string
	}{
		// Skip states
		{"requested", "active"},
		{"requested", "provisioned"},
		{"requested", "disabled"},
		{"approved", "active"},
		{"approved", "disabled"},
		// Backward edges
		{"active", "provisioned"},
		{"active", "approved"},
		{"active", "requested"},
		{"provisioned", "approved"},
		{"provisioned", "requested"},
		// Terminal state has no outgoing edges
		{"retired", "active"},
		{"retired", "requested"},
		{"retired", "approved"},
		{"retired", "provisioned"},
		{"retired", "disabled"},
		// Unknown states
		{"nonexistent", "active"},
		{"active", "nonexistent"},
	}
	for _, e := range invalidEdges {
		assert.False(t, CanLifecycleTransition(e.from, e.to),
			"expected invalid: %s → %s", e.from, e.to)
	}
}

func TestValidTargets(t *testing.T) {
	assert.ElementsMatch(t, []string{"approved", "retired"}, ValidTargets("requested"))
	assert.ElementsMatch(t, []string{"provisioned", "retired"}, ValidTargets("approved"))
	assert.ElementsMatch(t, []string{"active", "disabled", "retired"}, ValidTargets("provisioned"))
	assert.ElementsMatch(t, []string{"disabled", "retired"}, ValidTargets("active"))
	assert.ElementsMatch(t, []string{"active", "retired"}, ValidTargets("disabled"))
	assert.Empty(t, ValidTargets("retired"))
	assert.Nil(t, ValidTargets("nonexistent"))
}

func TestAllLifecycleStates_Complete(t *testing.T) {
	expected := []string{"requested", "approved", "provisioned", "active", "disabled", "retired"}
	assert.Equal(t, expected, AllLifecycleStates)
}
