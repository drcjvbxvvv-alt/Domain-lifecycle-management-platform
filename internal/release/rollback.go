package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/pkg/agentprotocol"
	"domain-platform/store/postgres"
)

// RollbackInput is the request to initiate a rollback for a release.
type RollbackInput struct {
	ReleaseDBID int64
	Reason      string
	TriggeredBy string // "user:123" or "system"
}

// Rollback initiates a rollback for the given release.
// It finds the previous succeeded release's artifact, records the intent in
// rollback_records, transitions the release to rolling_back, and enqueues the
// release:rollback task.
//
// Allowed from: failed, paused.
// (executing → rolling_back is also valid per the state machine via paused, but
// operators should pause first.)
func (s *Service) Rollback(ctx context.Context, in RollbackInput) error {
	rel, err := s.releases.GetByID(ctx, in.ReleaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}

	if !CanReleaseTransition(rel.Status, "rolling_back") {
		return fmt.Errorf("cannot rollback release in %q state: %w", rel.Status, ErrRollbackNotAllowed)
	}

	// Find the previous succeeded release in the same project
	prev, err := s.releases.GetLastSucceeded(ctx, rel.ProjectID, in.ReleaseDBID)
	if errors.Is(err, postgres.ErrReleaseNotFound) {
		return ErrNoPreviousRelease
	}
	if err != nil {
		return fmt.Errorf("find previous succeeded release: %w", err)
	}
	if prev.ArtifactID == nil {
		return ErrNoPreviousRelease
	}

	// Create rollback_records entry
	rec, err := s.rollbacks.Create(ctx, &postgres.RollbackRecord{
		ReleaseID:        in.ReleaseDBID,
		RollbackReleaseID: &prev.ID,
		TargetArtifactID: *prev.ArtifactID,
		Scope:            "release",
		Reason:           in.Reason,
	})
	if err != nil {
		s.logger.Error("create rollback record", zap.Error(err))
		// Non-fatal: proceed with rollback even if record creation fails
	}

	// Transition the release to rolling_back
	if err := s.TransitionRelease(ctx, in.ReleaseDBID, rel.Status, "rolling_back",
		fmt.Sprintf("rollback initiated: %s", in.Reason), in.TriggeredBy); err != nil {
		return fmt.Errorf("transition to rolling_back: %w", err)
	}

	// Enqueue the rollback execution task
	recordID := int64(0)
	if rec != nil {
		recordID = rec.ID
	}
	payload, _ := json.Marshal(tasks.ReleaseRollbackPayload{
		ReleaseID:        in.ReleaseDBID,
		TargetArtifactID: *prev.ArtifactID,
		TriggeredBy:      in.TriggeredBy,
		RollbackRecordID: recordID,
	})
	task := asynq.NewTask(tasks.TypeReleaseRollback, payload,
		asynq.MaxRetry(2),
		asynq.Timeout(300*time.Second),
		asynq.Queue("critical"),
	)
	if _, err := s.tasks.Enqueue(task); err != nil {
		s.logger.Error("enqueue rollback task", zap.Error(err),
			zap.Int64("release_id", in.ReleaseDBID))
	}

	s.logger.Info("rollback initiated",
		zap.Int64("release_id", in.ReleaseDBID),
		zap.Int64("target_artifact_id", *prev.ArtifactID),
		zap.String("triggered_by", in.TriggeredBy),
	)
	return nil
}

// ExecuteRollback dispatches rollback agent_tasks to every agent that was
// involved in the original release execution. The agents will restore their
// local snapshot at .previous/{rel.ReleaseID}/.
//
// After dispatch, enqueues a finalize task to poll for completion and
// transition rolling_back → rolled_back (or failed).
func (s *Service) ExecuteRollback(ctx context.Context, releaseDBID int64, targetArtifactID int64, triggeredBy string, rollbackRecordID int64) error {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}
	if rel.Status != "rolling_back" {
		s.logger.Info("execute rollback skipped — release not in rolling_back state",
			zap.Int64("release_id", releaseDBID), zap.String("status", rel.Status))
		return nil
	}

	// Get existing agent_tasks for this release to find involved agents
	existingTasks, err := s.agents.ListAgentTasksByRelease(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("list existing agent tasks: %w", err)
	}

	// Get unique agent IDs from existing tasks
	seen := make(map[int64]bool)
	var agentIDs []int64
	for _, at := range existingTasks {
		if !seen[at.AgentDBID] {
			seen[at.AgentDBID] = true
			agentIDs = append(agentIDs, at.AgentDBID)
		}
	}

	if len(agentIDs) == 0 {
		s.logger.Warn("no agents found for rollback, using online agents",
			zap.Int64("release_id", releaseDBID))
		// Fall back to online agents
		online, err := s.agents.ListOnlineAgents(ctx)
		if err != nil || len(online) == 0 {
			// Transition directly to failed
			_ = s.TransitionRelease(ctx, releaseDBID, "rolling_back", "failed",
				"no agents available for rollback", triggeredBy)
			if rollbackRecordID > 0 {
				_ = s.rollbacks.Complete(ctx, rollbackRecordID, false)
			}
			return fmt.Errorf("no agents available for rollback on release %d", releaseDBID)
		}
		for _, a := range online {
			agentIDs = append(agentIDs, a.ID)
		}
	}

	// Get a representative domain_task ID for the FK constraint
	// (we reuse the first domain_task for this release)
	domainTaskRows, err := s.domainTasks.ListByRelease(ctx, releaseDBID)
	if err != nil || len(domainTaskRows) == 0 {
		return fmt.Errorf("no domain tasks for release %d: %w", releaseDBID, err)
	}
	representativeDomainTaskID := domainTaskRows[0].ID

	// Create one rollback agent_task per unique agent
	for _, agentDBID := range agentIDs {
		envelope := agentprotocol.TaskEnvelope{
			Type:            agentprotocol.TaskTypeRollback,
			ReleaseID:       rel.ReleaseID,
			TargetReleaseID: rel.ReleaseID, // snapshot key = current release's ID
		}
		envelopeJSON, _ := json.Marshal(envelope)

		taskID := fmt.Sprintf("rollback-%s-agent%d", rel.ReleaseID, agentDBID)

		agentTask := &postgres.AgentTask{
			TaskID:       taskID,
			DomainTaskID: representativeDomainTaskID,
			AgentDBID:    agentDBID,
			ArtifactID:   targetArtifactID,
			Payload:      string(envelopeJSON),
		}
		if _, err := s.agents.CreateAgentTask(ctx, agentTask); err != nil {
			s.logger.Error("create rollback agent task",
				zap.Error(err), zap.Int64("agent_id", agentDBID))
			continue
		}
		s.logger.Info("rollback agent task created",
			zap.Int64("agent_id", agentDBID),
			zap.String("task_id", taskID),
		)
	}

	if err := s.releases.SetStartedAt(ctx, releaseDBID); err != nil {
		s.logger.Error("set started_at for rollback", zap.Error(err))
	}

	// Enqueue finalize-rollback — polls agent task completion
	finalizePayload, _ := json.Marshal(ReleaseFinalizePayload{
		ReleaseID:        releaseDBID,
		RetryNum:         0,
		IsRollback:       true,
		RollbackRecordID: rollbackRecordID,
	})
	finalizeTask := asynq.NewTask(tasks.TypeReleaseFinalize, finalizePayload,
		asynq.MaxRetry(0),
		asynq.Timeout(60*time.Second),
		asynq.Queue("critical"),
		asynq.ProcessIn(10*time.Second),
	)
	if _, err := s.tasks.Enqueue(finalizeTask); err != nil {
		s.logger.Error("enqueue rollback finalize task", zap.Error(err))
	}

	s.logger.Info("rollback agent tasks dispatched",
		zap.Int64("release_id", releaseDBID),
		zap.Int("agent_count", len(agentIDs)),
	)
	return nil
}
