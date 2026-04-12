package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
	"domain-platform/store/postgres"
)

// Service implements the agent management business logic.
// All status mutations go through TransitionAgent() — see CLAUDE.md Critical Rule #1.
type Service struct {
	store  *postgres.AgentStore
	logger *zap.Logger
}

func NewService(store *postgres.AgentStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// TransitionAgent atomically moves an agent from one state to another.
//
// The method:
//  1. Validates the edge (from → to) against the state machine
//  2. Opens a transaction
//  3. SELECT ... FOR UPDATE on the agent row
//  4. Optimistic check: current state == from
//  5. UPDATE agents SET status (single write path)
//  6. INSERT into agent_state_history
//  7. Commits
func (s *Service) TransitionAgent(ctx context.Context, agentDBID int64, from, to, reason, triggeredBy string) error {
	if !CanAgentTransition(from, to) {
		return fmt.Errorf("transition %q → %q: %w", from, to, ErrInvalidAgentState)
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	err = s.store.TransitionTx(ctx, tx, agentDBID, from, to, reason, triggeredBy)
	if err != nil {
		if errors.Is(err, postgres.ErrAgentRaceCondition) {
			return ErrAgentRaceCondition
		}
		return fmt.Errorf("transition agent %d: %w", agentDBID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}

	s.logger.Info("agent state transitioned",
		zap.Int64("agent_db_id", agentDBID),
		zap.String("from", from),
		zap.String("to", to),
		zap.String("reason", reason),
	)
	return nil
}

// Register handles an agent's first contact with the control plane.
// If the agent_id (by hostname) already exists and is not deleted, it re-registers.
// Otherwise a new agent row is created.
// Returns a RegisterResponse with the assigned agent_id.
func (s *Service) Register(ctx context.Context, req agentprotocol.RegisterRequest) (*agentprotocol.RegisterResponse, error) {
	agentID := fmt.Sprintf("agent-%s", uuid.New().String()[:8])

	now := time.Now()
	a := &postgres.Agent{
		AgentID:      agentID,
		Hostname:     req.Hostname,
		Status:       "registered",
		Capabilities: "[]",
		Tags:         "{}",
		LastSeenAt:   &now,
	}
	if req.IP != "" {
		a.IP = &req.IP
	}
	if req.Region != "" {
		a.Region = &req.Region
	}
	if req.Datacenter != "" {
		a.Datacenter = &req.Datacenter
	}
	if req.HostGroupID != nil {
		a.HostGroupID = req.HostGroupID
	}
	if req.AgentVersion != "" {
		a.AgentVersion = &req.AgentVersion
	}
	if req.CertSerial != "" {
		a.CertSerial = &req.CertSerial
	}

	dbID, err := s.store.Create(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	// Transition registered → online
	if err := s.TransitionAgent(ctx, dbID, "registered", "online", "initial registration", "system"); err != nil {
		s.logger.Warn("auto-transition registered→online failed", zap.Error(err))
		// Not fatal — agent is in 'registered' state; next heartbeat will promote
	}

	s.logger.Info("agent registered",
		zap.String("agent_id", agentID),
		zap.String("hostname", req.Hostname),
	)

	return &agentprotocol.RegisterResponse{
		AgentID:       agentID,
		Status:        "online",
		HeartbeatSecs: 30,
	}, nil
}

// Heartbeat processes an agent's periodic heartbeat.
// Updates last_seen_at, persists heartbeat data, and checks for pending tasks.
func (s *Service) Heartbeat(ctx context.Context, req agentprotocol.HeartbeatRequest) (*agentprotocol.HeartbeatResponse, error) {
	agent, err := s.store.GetByAgentID(ctx, req.AgentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return nil, ErrAgentNotFound
	}

	now := time.Now()
	if err := s.store.UpdateLastSeen(ctx, agent.ID, now); err != nil {
		return nil, fmt.Errorf("update last_seen: %w", err)
	}

	// Update agent version if changed
	if req.AgentVersion != "" && (agent.AgentVersion == nil || *agent.AgentVersion != req.AgentVersion) {
		if err := s.store.UpdateAgentVersion(ctx, agent.ID, req.AgentVersion); err != nil {
			s.logger.Warn("update agent version failed", zap.Error(err))
		}
	}

	// Persist heartbeat record
	var currentTaskPtr *string
	if req.CurrentTaskID != "" {
		currentTaskPtr = &req.CurrentTaskID
	}
	var versionPtr *string
	if req.AgentVersion != "" {
		versionPtr = &req.AgentVersion
	}
	var lastErrPtr *string
	if req.LastError != "" {
		lastErrPtr = &req.LastError
	}
	hb := &postgres.AgentHeartbeatRow{
		AgentDBID:     agent.ID,
		Status:        req.Status,
		CurrentTaskID: currentTaskPtr,
		AgentVersion:  versionPtr,
		LoadAvg1:      &req.LoadAvg1,
		DiskFreePct:   &req.DiskFreePct,
		LastError:     lastErrPtr,
	}
	if err := s.store.InsertHeartbeat(ctx, hb); err != nil {
		s.logger.Warn("insert heartbeat failed", zap.Error(err))
	}

	// If agent was offline and is now heartbeating, try to bring it back online
	if agent.Status == "offline" {
		if err := s.TransitionAgent(ctx, agent.ID, "offline", "online", "heartbeat resumed", "system"); err != nil {
			s.logger.Warn("offline→online transition failed", zap.Error(err))
		}
	}

	// Check for pending tasks
	pendingTask, err := s.store.NextPendingTask(ctx, agent.ID)
	if err != nil {
		s.logger.Warn("check pending tasks failed", zap.Error(err))
	}

	return &agentprotocol.HeartbeatResponse{
		Ack:        true,
		Status:     agent.Status,
		HasNewTask: pendingTask != nil,
	}, nil
}

// PullNextTask returns the next pending task for the agent, or nil if none.
func (s *Service) PullNextTask(ctx context.Context, agentID string) (*agentprotocol.TaskEnvelope, error) {
	agent, err := s.store.GetByAgentID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return nil, ErrAgentNotFound
	}

	task, err := s.store.NextPendingTask(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("next pending task: %w", err)
	}
	if task == nil {
		return nil, nil // no task available
	}

	// Decode the payload JSONB into a TaskEnvelope
	var envelope agentprotocol.TaskEnvelope
	if err := json.Unmarshal([]byte(task.Payload), &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal task payload: %w", err)
	}
	envelope.TaskID = task.TaskID

	// Set artifact URL if stored
	if task.ArtifactURL != nil {
		envelope.ArtifactURL = *task.ArtifactURL
	}

	return &envelope, nil
}

// ClaimTask marks a task as claimed by the agent.
func (s *Service) ClaimTask(ctx context.Context, taskID string) error {
	return s.store.ClaimTask(ctx, taskID)
}

// ReportTask processes the agent's task completion report.
// Updates agent_tasks and records deployment logs for each phase.
func (s *Service) ReportTask(ctx context.Context, report agentprotocol.TaskReport) error {
	task, err := s.store.GetTaskByTaskID(ctx, report.TaskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if task == nil {
		return fmt.Errorf("task %s not found", report.TaskID)
	}

	// Update task status
	if err := s.store.CompleteTask(ctx, report.TaskID, report.Status, report.DurationMs, report.Error); err != nil {
		return fmt.Errorf("complete task: %w", err)
	}

	// Record phase logs
	for _, phase := range report.Phases {
		if err := s.store.InsertDeploymentLog(ctx, task.ID, phase.Phase, phase.Status, phase.DurationMs, phase.Detail); err != nil {
			s.logger.Warn("insert deployment log failed",
				zap.String("task_id", report.TaskID),
				zap.String("phase", phase.Phase),
				zap.Error(err),
			)
		}
	}

	s.logger.Info("task reported",
		zap.String("task_id", report.TaskID),
		zap.String("status", report.Status),
		zap.Int64("duration_ms", report.DurationMs),
	)
	return nil
}

// GetAgent returns an agent by its DB ID.
func (s *Service) GetAgent(ctx context.Context, id int64) (*postgres.Agent, error) {
	return s.store.GetByID(ctx, id)
}

// GetAgentByAgentID returns an agent by its public agent_id string.
func (s *Service) GetAgentByAgentID(ctx context.Context, agentID string) (*postgres.Agent, error) {
	return s.store.GetByAgentID(ctx, agentID)
}

// ListAgents returns all agents with pagination.
func (s *Service) ListAgents(ctx context.Context, limit, offset int) ([]postgres.Agent, int, error) {
	return s.store.ListAll(ctx, limit, offset)
}

// ListStateHistory returns the state transition history for an agent.
func (s *Service) ListStateHistory(ctx context.Context, agentDBID int64, limit int) ([]postgres.AgentStateHistoryRow, error) {
	return s.store.ListStateHistory(ctx, agentDBID, limit)
}
