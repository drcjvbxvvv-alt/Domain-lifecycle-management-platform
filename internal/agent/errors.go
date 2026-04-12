package agent

import "errors"

var (
	// ErrAgentNotFound is returned when a query finds no matching agent.
	ErrAgentNotFound = errors.New("agent not found")

	// ErrInvalidAgentState is returned when a requested state transition
	// is not permitted by the agent state machine.
	ErrInvalidAgentState = errors.New("invalid agent state transition")

	// ErrAgentRaceCondition is returned when the agent's current state
	// does not match the expected state (optimistic concurrency failure).
	ErrAgentRaceCondition = errors.New("agent state race condition")

	// ErrAgentOffline is returned when an operation requires an online
	// agent but the agent is offline.
	ErrAgentOffline = errors.New("agent offline")

	// ErrAgentDisabled is returned when an operation targets a disabled agent.
	ErrAgentDisabled = errors.New("agent disabled")

	// ErrNoTaskAvailable is returned when there are no pending tasks for the agent.
	ErrNoTaskAvailable = errors.New("no task available")
)
