// Package agentprotocol defines the wire types for the Pull Agent ↔ Control Plane protocol.
// All request/response structs in this package are shared by cmd/agent and api/handler/agent.go.
// The protocol runs over HTTPS + mTLS on the agent_port (default :8443).
// See ARCHITECTURE.md §3 Pull Agent Detailed Design.
package agentprotocol
