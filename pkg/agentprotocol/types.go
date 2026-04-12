package agentprotocol

import "time"

// ── Task types ──────────────────────────────────────────────────────────────

// TaskType constants for agent task envelopes.
const (
	TaskTypeDeployHTML  = "deploy_html"
	TaskTypeDeployNginx = "deploy_nginx"
	TaskTypeDeployFull  = "deploy_full"
	TaskTypeRollback    = "rollback"
	TaskTypeVerify      = "verify"
)

// ── Task status ─────────────────────────────────────────────────────────────

const (
	TaskStatusPending   = "pending"
	TaskStatusClaimed   = "claimed"
	TaskStatusRunning   = "running"
	TaskStatusSucceeded = "succeeded"
	TaskStatusFailed    = "failed"
	TaskStatusTimeout   = "timeout"
	TaskStatusCancelled = "cancelled"
)

// ── Registration ────────────────────────────────────────────────────────────

// RegisterRequest is sent by the agent on first contact.
type RegisterRequest struct {
	Hostname     string `json:"hostname"`
	IP           string `json:"ip,omitempty"`
	Region       string `json:"region,omitempty"`
	Datacenter   string `json:"datacenter,omitempty"`
	HostGroupID  *int64 `json:"host_group_id,omitempty"`
	AgentVersion string `json:"agent_version"`
	CertSerial   string `json:"cert_serial,omitempty"`
}

// RegisterResponse is returned by the control plane after registration.
type RegisterResponse struct {
	AgentID       string `json:"agent_id"`
	Status        string `json:"status"`
	HeartbeatSecs int    `json:"heartbeat_secs"`
}

// ── Heartbeat ───────────────────────────────────────────────────────────────

// HeartbeatRequest is sent periodically by the agent.
type HeartbeatRequest struct {
	AgentID       string  `json:"agent_id"`
	Status        string  `json:"status"`
	CurrentTaskID string  `json:"current_task_id,omitempty"`
	AgentVersion  string  `json:"agent_version"`
	LoadAvg1      float64 `json:"load_avg_1"`
	DiskFreePct   float64 `json:"disk_free_pct"`
	LastError     string  `json:"last_error,omitempty"`
}

// HeartbeatResponse is returned by the control plane.
type HeartbeatResponse struct {
	Ack        bool   `json:"ack"`
	Status     string `json:"status"`
	HasNewTask bool   `json:"has_new_task"`
}

// ── Task Envelope ───────────────────────────────────────────────────────────

// TaskEnvelope is the task assignment sent from control plane to agent.
// The agent receives this via GET /agent/v1/tasks (long-poll).
type TaskEnvelope struct {
	TaskID      string       `json:"task_id"`
	Type        string       `json:"type"` // TaskTypeDeployHTML, etc.
	ReleaseID   string       `json:"release_id"`
	ArtifactURL string       `json:"artifact_url"` // pre-signed S3 URL to download
	Manifest    Manifest     `json:"manifest"`
	Domains     []string     `json:"domains"`
	DeployPath  string       `json:"deploy_path"`  // e.g., /var/www
	NginxPath   string       `json:"nginx_path"`    // e.g., /etc/nginx/conf.d
	AllowReload bool         `json:"allow_reload"`  // whether nginx reload is permitted
	Verify      VerifyConfig `json:"verify"`
}

// VerifyConfig describes local verification after deployment.
type VerifyConfig struct {
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url,omitempty"`        // e.g., http://localhost:80
	StatusCode int    `json:"status_code,omitempty"` // expected status (default 200)
	TimeoutMs  int    `json:"timeout_ms,omitempty"`  // per-check timeout (default 5000)
}

// ── Task Report ─────────────────────────────────────────────────────────────

// TaskReport is sent by the agent after completing (or failing) a task.
type TaskReport struct {
	TaskID     string        `json:"task_id"`
	Status     string        `json:"status"` // succeeded | failed
	Phases     []PhaseReport `json:"phases"`
	DurationMs int64         `json:"duration_ms"`
	Error      string        `json:"error,omitempty"`
}

// PhaseReport describes one phase of the deployment pipeline.
type PhaseReport struct {
	Phase      string `json:"phase"` // download, verify_checksum, verify_signature, write, nginx_test, snapshot, swap, reload, local_verify
	Status     string `json:"status"` // succeeded | failed | skipped
	DurationMs int64  `json:"duration_ms"`
	Detail     string `json:"detail,omitempty"`
}

// ── Log upload ──────────────────────────────────────────────────────────────

// LogEntry is a single log line uploaded by the agent.
type LogEntry struct {
	TaskID    string    `json:"task_id"`
	Level     string    `json:"level"` // info | warn | error
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// LogUploadRequest is the bulk log upload payload.
type LogUploadRequest struct {
	Entries []LogEntry `json:"entries"`
}

// ── Upgrade ─────────────────────────────────────────────────────────────────

// UpgradeResponse is returned by GET /agent/v1/upgrade.
type UpgradeResponse struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	URL       string `json:"url,omitempty"`
	Checksum  string `json:"checksum,omitempty"`
}
