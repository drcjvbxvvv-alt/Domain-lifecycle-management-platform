package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// AgentStore handles agent persistence.
// CLAUDE.md Critical Rule #1: ALL `agents.status` mutations go through
// this store's updateAgentStatusTx. No other code in the codebase may issue
// `UPDATE agents SET status`. The CI gate `make check-agent-writes` enforces this.
type AgentStore struct {
	db *sqlx.DB
}

func NewAgentStore(db *sqlx.DB) *AgentStore {
	return &AgentStore{db: db}
}

// Agent maps to the agents table.
type Agent struct {
	ID            int64      `db:"id"`
	UUID          string     `db:"uuid"`
	AgentID       string     `db:"agent_id"`
	Hostname      string     `db:"hostname"`
	IP            *string    `db:"ip"`
	Region        *string    `db:"region"`
	Datacenter    *string    `db:"datacenter"`
	HostGroupID   *int64     `db:"host_group_id"`
	AgentVersion  *string    `db:"agent_version"`
	Capabilities  string     `db:"capabilities"` // JSONB
	Tags          string     `db:"tags"`          // JSONB
	CertSerial    *string    `db:"cert_serial"`
	CertExpiresAt *time.Time `db:"cert_expires_at"`
	Status        string     `db:"status"`
	LastSeenAt    *time.Time `db:"last_seen_at"`
	LastError     *string    `db:"last_error"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	DeletedAt     *time.Time `db:"deleted_at"`
}

// AgentStateHistoryRow maps to agent_state_history.
type AgentStateHistoryRow struct {
	ID          int64     `db:"id"`
	AgentID     int64     `db:"agent_id"`
	FromState   *string   `db:"from_state"`
	ToState     string    `db:"to_state"`
	Reason      *string   `db:"reason"`
	TriggeredBy string    `db:"triggered_by"`
	CreatedAt   time.Time `db:"created_at"`
}

// AgentHeartbeatRow maps to agent_heartbeats.
type AgentHeartbeatRow struct {
	ID            int64      `db:"id"`
	AgentDBID     int64      `db:"agent_id"`
	Status        string     `db:"status"`
	CurrentTaskID *string    `db:"current_task_id"`
	AgentVersion  *string    `db:"agent_version"`
	LoadAvg1      *float64   `db:"load_avg_1"`
	DiskFreePct   *float64   `db:"disk_free_pct"`
	LastError     *string    `db:"last_error"`
	ReceivedAt    time.Time  `db:"received_at"`
}

// AgentTask maps to the agent_tasks table.
type AgentTask struct {
	ID           int64      `db:"id"`
	UUID         string     `db:"uuid"`
	TaskID       string     `db:"task_id"`
	DomainTaskID int64      `db:"domain_task_id"`
	AgentDBID    int64      `db:"agent_id"`
	ArtifactID   int64      `db:"artifact_id"`
	ArtifactURL  *string    `db:"artifact_url"`
	Payload      string     `db:"payload"` // JSONB
	Status       string     `db:"status"`
	ClaimedAt    *time.Time `db:"claimed_at"`
	StartedAt    *time.Time `db:"started_at"`
	EndedAt      *time.Time `db:"ended_at"`
	DurationMs   *int64     `db:"duration_ms"`
	LastError    *string    `db:"last_error"`
	RetryCount   int        `db:"retry_count"`
	CreatedAt    time.Time  `db:"created_at"`
}

// ErrAgentRaceCondition is the sentinel for concurrent agent state change.
// Defined here (store layer) because TransitionTx detects it.
var ErrAgentRaceCondition = fmt.Errorf("agent state race condition")

// ── Create / Read ───────────────────────────────────────────────────────

// Create inserts a new agent row and returns the DB ID.
func (s *AgentStore) Create(ctx context.Context, a *Agent) (int64, error) {
	const q = `
		INSERT INTO agents (agent_id, hostname, ip, region, datacenter,
			host_group_id, agent_version, capabilities, tags, cert_serial,
			cert_expires_at, status, last_seen_at)
		VALUES (:agent_id, :hostname, :ip, :region, :datacenter,
			:host_group_id, :agent_version, :capabilities, :tags, :cert_serial,
			:cert_expires_at, :status, :last_seen_at)
		RETURNING id`
	rows, err := sqlx.NamedQueryContext(ctx, s.db, q, a)
	if err != nil {
		return 0, fmt.Errorf("insert agent: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&a.ID); err != nil {
			return 0, fmt.Errorf("scan agent id: %w", err)
		}
	}
	return a.ID, nil
}

// GetByID returns an agent by its DB primary key.
func (s *AgentStore) GetByID(ctx context.Context, id int64) (*Agent, error) {
	var a Agent
	err := s.db.GetContext(ctx, &a,
		`SELECT * FROM agents WHERE id = $1 AND deleted_at IS NULL`, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent %d: %w", id, err)
	}
	return &a, nil
}

// GetByAgentID returns an agent by its public agent_id string.
func (s *AgentStore) GetByAgentID(ctx context.Context, agentID string) (*Agent, error) {
	var a Agent
	err := s.db.GetContext(ctx, &a,
		`SELECT * FROM agents WHERE agent_id = $1 AND deleted_at IS NULL`, agentID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent by agent_id %s: %w", agentID, err)
	}
	return &a, nil
}

// ListByStatus returns agents matching the given status.
func (s *AgentStore) ListByStatus(ctx context.Context, status string, limit, offset int) ([]Agent, int, error) {
	var total int
	err := s.db.GetContext(ctx, &total,
		`SELECT COUNT(*) FROM agents WHERE status = $1 AND deleted_at IS NULL`, status)
	if err != nil {
		return nil, 0, fmt.Errorf("count agents by status: %w", err)
	}

	var agents []Agent
	err = s.db.SelectContext(ctx, &agents,
		`SELECT * FROM agents WHERE status = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list agents by status: %w", err)
	}
	return agents, total, nil
}

// ListByHostGroup returns agents belonging to a host group.
func (s *AgentStore) ListByHostGroup(ctx context.Context, hostGroupID int64, limit, offset int) ([]Agent, int, error) {
	var total int
	err := s.db.GetContext(ctx, &total,
		`SELECT COUNT(*) FROM agents WHERE host_group_id = $1 AND deleted_at IS NULL`, hostGroupID)
	if err != nil {
		return nil, 0, fmt.Errorf("count agents by host_group: %w", err)
	}

	var agents []Agent
	err = s.db.SelectContext(ctx, &agents,
		`SELECT * FROM agents WHERE host_group_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, hostGroupID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list agents by host_group: %w", err)
	}
	return agents, total, nil
}

// ListAll returns all non-deleted agents.
func (s *AgentStore) ListAll(ctx context.Context, limit, offset int) ([]Agent, int, error) {
	var total int
	err := s.db.GetContext(ctx, &total,
		`SELECT COUNT(*) FROM agents WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, 0, fmt.Errorf("count all agents: %w", err)
	}

	var agents []Agent
	err = s.db.SelectContext(ctx, &agents,
		`SELECT * FROM agents WHERE deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list all agents: %w", err)
	}
	return agents, total, nil
}

// ── State transition (single write path) ────────────────────────────────

// TransitionTx executes the agent state transition within a transaction.
// It performs:
//  1. SELECT ... FOR UPDATE to lock the agent row
//  2. Optimistic check: current state must equal expectedFrom
//  3. UPDATE agents SET status (the ONLY such UPDATE in the codebase)
//  4. INSERT into agent_state_history
func (s *AgentStore) TransitionTx(ctx context.Context, tx *sqlx.Tx, agentDBID int64, expectedFrom, to, reason, triggeredBy string) error {
	// Step 1: Lock the row and read current state
	var currentState string
	err := tx.GetContext(ctx, &currentState,
		`SELECT status FROM agents WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		agentDBID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("agent %d not found", agentDBID)
	}
	if err != nil {
		return fmt.Errorf("lock agent %d: %w", agentDBID, err)
	}

	// Step 2: Optimistic check
	if currentState != expectedFrom {
		return fmt.Errorf("expected state %q but found %q: %w",
			expectedFrom, currentState, ErrAgentRaceCondition)
	}

	// Step 3: The single write path — `UPDATE agents SET status`
	if err := updateAgentStatusTx(ctx, tx, agentDBID, to); err != nil {
		return err
	}

	// Step 4: Record history
	return insertAgentStateHistoryTx(ctx, tx, agentDBID, expectedFrom, to, reason, triggeredBy)
}

// updateAgentStatusTx is the ONLY function in the entire codebase that issues
// `UPDATE agents SET status`. CI gate `make check-agent-writes` enforces this.
func updateAgentStatusTx(ctx context.Context, tx *sqlx.Tx, agentDBID int64, status string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE agents SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, agentDBID)
	if err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}
	return nil
}

func insertAgentStateHistoryTx(ctx context.Context, tx *sqlx.Tx, agentDBID int64, from, to, reason, triggeredBy string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO agent_state_history (agent_id, from_state, to_state, reason, triggered_by)
		 VALUES ($1, $2, $3, $4, $5)`,
		agentDBID, from, to, reason, triggeredBy)
	if err != nil {
		return fmt.Errorf("insert agent state history: %w", err)
	}
	return nil
}

// ── Heartbeat ───────────────────────────────────────────────────────────

// UpdateLastSeen updates the agent's last_seen_at timestamp.
func (s *AgentStore) UpdateLastSeen(ctx context.Context, agentDBID int64, ts time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE agents SET last_seen_at = $1, updated_at = NOW() WHERE id = $2`,
		ts, agentDBID)
	if err != nil {
		return fmt.Errorf("update last_seen: %w", err)
	}
	return nil
}

// InsertHeartbeat persists a heartbeat record.
func (s *AgentStore) InsertHeartbeat(ctx context.Context, hb *AgentHeartbeatRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_heartbeats (agent_id, status, current_task_id, agent_version, load_avg_1, disk_free_pct, last_error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		hb.AgentDBID, hb.Status, hb.CurrentTaskID, hb.AgentVersion, hb.LoadAvg1, hb.DiskFreePct, hb.LastError)
	if err != nil {
		return fmt.Errorf("insert heartbeat: %w", err)
	}
	return nil
}

// UpdateAgentVersion updates the agent's software version.
func (s *AgentStore) UpdateAgentVersion(ctx context.Context, agentDBID int64, version string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE agents SET agent_version = $1, updated_at = NOW() WHERE id = $2`,
		version, agentDBID)
	if err != nil {
		return fmt.Errorf("update agent version: %w", err)
	}
	return nil
}

// ── Task queries ────────────────────────────────────────────────────────

// NextPendingTask returns the oldest pending agent_task for the given agent, or nil.
func (s *AgentStore) NextPendingTask(ctx context.Context, agentDBID int64) (*AgentTask, error) {
	var t AgentTask
	err := s.db.GetContext(ctx, &t,
		`SELECT * FROM agent_tasks WHERE agent_id = $1 AND status = 'pending'
		 ORDER BY created_at ASC LIMIT 1`, agentDBID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("next pending task: %w", err)
	}
	return &t, nil
}

// ClaimTask marks an agent_task as claimed.
func (s *AgentStore) ClaimTask(ctx context.Context, taskID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_tasks SET status = 'claimed', claimed_at = NOW()
		 WHERE task_id = $1 AND status = 'pending'`, taskID)
	if err != nil {
		return fmt.Errorf("claim task %s: %w", taskID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %s not pending or not found", taskID)
	}
	return nil
}

// CompleteTask updates an agent_task with the final status and duration.
func (s *AgentStore) CompleteTask(ctx context.Context, taskID, status string, durationMs int64, lastError string) error {
	var errPtr *string
	if lastError != "" {
		errPtr = &lastError
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_tasks SET status = $1, ended_at = NOW(), duration_ms = $2, last_error = $3
		 WHERE task_id = $4`,
		status, durationMs, errPtr, taskID)
	if err != nil {
		return fmt.Errorf("complete task %s: %w", taskID, err)
	}
	return nil
}

// GetTaskByTaskID returns an agent_task by its public task_id.
func (s *AgentStore) GetTaskByTaskID(ctx context.Context, taskID string) (*AgentTask, error) {
	var t AgentTask
	err := s.db.GetContext(ctx, &t,
		`SELECT * FROM agent_tasks WHERE task_id = $1`, taskID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task %s: %w", taskID, err)
	}
	return &t, nil
}

// InsertDeploymentLog records a phase execution log.
func (s *AgentStore) InsertDeploymentLog(ctx context.Context, agentTaskID int64, phase, status string, durationMs int64, detail string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deployment_logs (agent_task_id, phase, status, duration_ms, detail)
		 VALUES ($1, $2, $3, $4, $5)`,
		agentTaskID, phase, status, durationMs, detail)
	if err != nil {
		return fmt.Errorf("insert deployment log: %w", err)
	}
	return nil
}

// ── Offline detection ───────────────────────────────────────────────────

// ListStaleOnlineAgents returns agents that are in online/busy/idle status
// but have not sent a heartbeat within the given threshold.
func (s *AgentStore) ListStaleOnlineAgents(ctx context.Context, threshold time.Duration) ([]Agent, error) {
	cutoff := time.Now().Add(-threshold)
	var agents []Agent
	err := s.db.SelectContext(ctx, &agents,
		`SELECT * FROM agents
		 WHERE status IN ('online', 'busy', 'idle')
		   AND deleted_at IS NULL
		   AND (last_seen_at IS NULL OR last_seen_at < $1)`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale agents: %w", err)
	}
	return agents, nil
}

// ── History ─────────────────────────────────────────────────────────────

// ListStateHistory returns the state history for an agent, newest first.
func (s *AgentStore) ListStateHistory(ctx context.Context, agentDBID int64, limit int) ([]AgentStateHistoryRow, error) {
	var rows []AgentStateHistoryRow
	err := s.db.SelectContext(ctx, &rows,
		`SELECT * FROM agent_state_history WHERE agent_id = $1
		 ORDER BY created_at DESC LIMIT $2`, agentDBID, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent state history: %w", err)
	}
	return rows, nil
}

// BeginTx starts a new transaction.
func (s *AgentStore) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	return s.db.BeginTxx(ctx, nil)
}
