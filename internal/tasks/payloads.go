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
	ReleaseID int64   `json:"release_id"`
	DomainIDs []int64 `json:"domain_ids"`
}

// ReleaseDispatchShardPayload is the payload for TypeReleaseDispatchShard.
type ReleaseDispatchShardPayload struct {
	ReleaseID int64 `json:"release_id"`
	ShardID   int64 `json:"shard_id"`
}

// ReleaseProbeVerifyPayload is the payload for TypeReleaseProbeVerify.
type ReleaseProbeVerifyPayload struct {
	ReleaseID int64 `json:"release_id"`
	ShardID   int64 `json:"shard_id"`
}

// ReleaseFinalizePayload is the payload for TypeReleaseFinalize.
type ReleaseFinalizePayload struct {
	ReleaseID int64 `json:"release_id"`
	RetryNum  int   `json:"retry_num,omitempty"`
}

// ReleaseRollbackPayload is the payload for TypeReleaseRollback.
type ReleaseRollbackPayload struct {
	ReleaseID        int64  `json:"release_id"`
	TargetArtifactID int64  `json:"target_artifact_id"`
	TriggeredBy      string `json:"triggered_by"`
	RollbackRecordID int64  `json:"rollback_record_id,omitempty"`
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

// ── Domain Import ─────────────────────────────────────────────────────────────

// DomainImportPayload is the payload for TypeDomainImport.
// The raw CSV content is stored in the job row; this payload just carries the job ID.
type DomainImportPayload struct {
	JobID int64 `json:"job_id"`
}

// ── DNS Drift ─────────────────────────────────────────────────────────────────

// DNSDriftCheckAllPayload is the payload for TypeDNSDriftCheckAll.
// Empty — the handler scans all active domains that have a dns_provider_id set.
type DNSDriftCheckAllPayload struct{}

// DNSDriftCheckPayload is the payload for TypeDNSDriftCheck.
// Carries the domain + provider identifiers needed to run one drift check.
type DNSDriftCheckPayload struct {
	DomainID      int64  `json:"domain_id"`
	DomainUUID    string `json:"domain_uuid"`
	FQDN          string `json:"fqdn"`
	DNSProviderID int64  `json:"dns_provider_id"`
}

// ── NS Delegation Check (B.2) ─────────────────────────────────────────────────

// NSCheckAllPayload is the payload for TypeNSCheckAll.
// Empty — the handler scans all domains with ns_delegation_status IN ('pending', 'mismatch').
type NSCheckAllPayload struct{}

// NSCheckPayload is the payload for TypeNSCheck.
type NSCheckPayload struct {
	DomainID      int64    `json:"domain_id"`
	FQDN          string   `json:"fqdn"`
	DNSProviderID int64    `json:"dns_provider_id"`
	ExpectedNS    []string `json:"expected_ns"`
}

// ── Alert ─────────────────────────────────────────────────────────────────────

// AlertFirePayload is the payload for TypeAlertFire.
// The engine picks it up, deduplicates, persists, and fans out TypeNotifySend tasks.
type AlertFirePayload struct {
	Severity   string `json:"severity"`              // P1 | P2 | P3 | INFO
	Source     string `json:"source"`                // probe | drift | expiry | agent | manual | system
	TargetKind string `json:"target_kind"`
	TargetID   *int64 `json:"target_id,omitempty"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"` // JSON string
	DedupKey   string `json:"dedup_key,omitempty"`
}

// ── Notify ────────────────────────────────────────────────────────────────────

// NotifySendPayload is the payload for TypeNotifySend.
// Carries the channel_id so the worker can look up the channel config
// from the notification_channels table and dispatch via the Dispatcher.
type NotifySendPayload struct {
	ChannelID    int64  `json:"channel_id"`
	AlertEventID int64  `json:"alert_event_id,omitempty"`
	Severity     string `json:"severity,omitempty"` // "info" | "warning" | "critical"
}
