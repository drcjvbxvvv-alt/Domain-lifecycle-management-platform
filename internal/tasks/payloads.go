package tasks

// This file declares the JSON payload struct for every task type constant in types.go.
// Workers unmarshal these from the asynq task body.
// Keep in sync with the task type constants — one payload per task type.

// ── Lifecycle ────────────────────────────────────────────────────────────────

// LifecycleProvisionPayload is the payload for TypeLifecycleProvision.
// Dispatched when a domain transitions approved → provisioned.
type LifecycleProvisionPayload struct {
	DomainID   int64  `json:"domain_id"`
	DomainUUID string `json:"domain_uuid"`
	FQDN       string `json:"fqdn"`
	ProjectID  int64  `json:"project_id"`
}

// LifecycleDeprovisionPayload is the payload for TypeLifecycleDeprovision.
// Dispatched when a domain transitions to retired (DNS cleanup).
type LifecycleDeprovisionPayload struct {
	DomainID   int64  `json:"domain_id"`
	DomainUUID string `json:"domain_uuid"`
	FQDN       string `json:"fqdn"`
}

// ── Artifact ─────────────────────────────────────────────────────────────────

// ArtifactBuildPayload is the JSON payload for TypeArtifactBuild tasks.
type ArtifactBuildPayload struct {
	ProjectID         int64   `json:"project_id"`
	ProjectSlug       string  `json:"project_slug"`
	TemplateVersionID int64   `json:"template_version_id"`
	ReleaseID         *int64  `json:"release_id,omitempty"`
	BuiltBy           *int64  `json:"built_by,omitempty"`
	DomainIDs         []int64 `json:"domain_ids"`
}

// ArtifactSignPayload is the payload for TypeArtifactSign.
type ArtifactSignPayload struct {
	ArtifactID   int64  `json:"artifact_id"`
	ArtifactUUID string `json:"artifact_uuid"`
}

// ── Release ───────────────────────────────────────────────────────────────────

// ReleasePlanPayload is the payload for TypeReleasePlan.
type ReleasePlanPayload struct {
	ReleaseID   int64  `json:"release_id"`
	ReleaseUUID string `json:"release_uuid"`
}

// ReleaseDispatchShardPayload is the payload for TypeReleaseDispatchShard.
type ReleaseDispatchShardPayload struct {
	ReleaseID   int64   `json:"release_id"`
	ShardID     int64   `json:"shard_id"`
	AgentIDs    []int64 `json:"agent_ids"`
}

// ReleaseProbeVerifyPayload is the payload for TypeReleaseProbeVerify.
type ReleaseProbeVerifyPayload struct {
	ReleaseID int64 `json:"release_id"`
	ShardID   int64 `json:"shard_id"`
}

// ReleaseFinalizePayload is the payload for TypeReleaseFinalize.
type ReleaseFinalizePayload struct {
	ReleaseID   int64  `json:"release_id"`
	ReleaseUUID string `json:"release_uuid"`
}

// ReleaseRollbackPayload is the payload for TypeReleaseRollback.
type ReleaseRollbackPayload struct {
	ReleaseID          int64  `json:"release_id"`
	TargetArtifactID   int64  `json:"target_artifact_id"`
	TriggeredBy        string `json:"triggered_by"`
}

// ── Agent ─────────────────────────────────────────────────────────────────────

// AgentHealthCheckPayload is the payload for TypeAgentHealthCheck.
// Empty — health check scans the entire agent table.
type AgentHealthCheckPayload struct{}

// AgentUpgradeDispatchPayload is the payload for TypeAgentUpgradeDispatch.
type AgentUpgradeDispatchPayload struct {
	TargetVersion string  `json:"target_version"`
	AgentIDs      []int64 `json:"agent_ids,omitempty"` // empty = all eligible agents
}

// ── Probe ─────────────────────────────────────────────────────────────────────

// ProbeRunPayload is shared by TypeProbeRunL1, TypeProbeRunL2, TypeProbeRunL3.
type ProbeRunPayload struct {
	ReleaseID int64    `json:"release_id"`
	DomainIDs []int64  `json:"domain_ids,omitempty"`
	Level     int      `json:"level"` // 1, 2, or 3
}

// ── Notify ────────────────────────────────────────────────────────────────────

// NotifySendPayload is the payload for TypeNotifySend.
type NotifySendPayload struct {
	Channel  string `json:"channel"`           // "telegram" | "slack" | "webhook"
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Severity string `json:"severity,omitempty"` // "info" | "warn" | "error"
}
