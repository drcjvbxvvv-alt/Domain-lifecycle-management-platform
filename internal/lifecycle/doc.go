// Package lifecycle implements the Domain Lifecycle state machine.
// Valid states: requested → approved → provisioned → active → disabled → retired.
// All status mutations MUST go through Service.Transition() — see CLAUDE.md Critical Rule #1.
package lifecycle
