// Package release implements the Release state machine and release orchestration.
// Valid states: pending → planning → ready → executing → succeeded/paused/failed/rolling_back/rolled_back/cancelled.
// All status mutations MUST go through Service.Transition() — see CLAUDE.md Critical Rule #1.
package release
