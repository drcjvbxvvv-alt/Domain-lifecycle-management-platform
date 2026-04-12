// Package agent implements the Agent management control plane.
// It handles agent registration, heartbeat tracking, and the Agent state machine.
// Valid states: registered → online ↔ busy/idle/offline/draining/disabled/upgrading/error.
// All status mutations MUST go through Service.Transition() — see CLAUDE.md Critical Rule #1.
package agent
