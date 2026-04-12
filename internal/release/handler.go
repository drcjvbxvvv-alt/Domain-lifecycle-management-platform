package release

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
)

// ── HandlePlan ──────────────────────────────────────────────────────────────

// HandlePlan is the asynq handler for TypeReleasePlan.
// It delegates to Service.Plan which transitions pending → planning,
// creates domain_tasks, and enqueues the artifact build.
type HandlePlan struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandlePlan(svc *Service, logger *zap.Logger) *HandlePlan {
	return &HandlePlan{svc: svc, logger: logger}
}

func (h *HandlePlan) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload ReleasePlanPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal release plan payload: %w", err)
	}

	h.logger.Info("release plan task started",
		zap.Int64("release_id", payload.ReleaseID),
	)

	if err := h.svc.Plan(ctx, payload.ReleaseID, payload.ShardStrategy); err != nil {
		h.logger.Error("release plan failed",
			zap.Int64("release_id", payload.ReleaseID),
			zap.Error(err),
		)
		return fmt.Errorf("plan release %d: %w", payload.ReleaseID, err)
	}

	return nil
}

// ── HandleDispatchShard ─────────────────────────────────────────────────────

// HandleDispatchShard is the asynq handler for TypeReleaseDispatchShard.
// It delegates to Service.DispatchShard which creates agent_tasks and
// transitions the release to executing.
type HandleDispatchShard struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandleDispatchShard(svc *Service, logger *zap.Logger) *HandleDispatchShard {
	return &HandleDispatchShard{svc: svc, logger: logger}
}

func (h *HandleDispatchShard) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload ReleaseDispatchPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal dispatch shard payload: %w", err)
	}

	h.logger.Info("dispatch shard task started",
		zap.Int64("release_id", payload.ReleaseID),
		zap.Int64("shard_id", payload.ShardID),
	)

	if err := h.svc.DispatchShard(ctx, payload.ReleaseID, payload.ShardID); err != nil {
		h.logger.Error("dispatch shard failed",
			zap.Int64("release_id", payload.ReleaseID),
			zap.Int64("shard_id", payload.ShardID),
			zap.Error(err),
		)
		return fmt.Errorf("dispatch shard %d for release %d: %w", payload.ShardID, payload.ReleaseID, err)
	}

	return nil
}

// ── HandleFinalize ──────────────────────────────────────────────────────────

// HandleFinalize is the asynq handler for TypeReleaseFinalize.
// It delegates to Service.Finalize which polls agent_task stats and
// transitions executing → succeeded or failed (or re-enqueues itself).
type HandleFinalize struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandleFinalize(svc *Service, logger *zap.Logger) *HandleFinalize {
	return &HandleFinalize{svc: svc, logger: logger}
}

func (h *HandleFinalize) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload ReleaseFinalizePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal finalize payload: %w", err)
	}

	if err := h.svc.Finalize(ctx, payload.ReleaseID, payload.ShardID, payload.RetryNum, payload.IsRollback, payload.RollbackRecordID); err != nil {
		h.logger.Error("finalize failed",
			zap.Int64("release_id", payload.ReleaseID),
			zap.Int64("shard_id", payload.ShardID),
			zap.Int("retry_num", payload.RetryNum),
			zap.Bool("is_rollback", payload.IsRollback),
			zap.Error(err),
		)
		return fmt.Errorf("finalize release %d: %w", payload.ReleaseID, err)
	}

	return nil
}

// ── HandleRollback ──────────────────────────────────────────────────────────

// HandleRollback is the asynq handler for TypeReleaseRollback.
// It delegates to Service.ExecuteRollback which dispatches rollback agent_tasks
// and enqueues a finalize-rollback polling task.
type HandleRollback struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandleRollback(svc *Service, logger *zap.Logger) *HandleRollback {
	return &HandleRollback{svc: svc, logger: logger}
}

func (h *HandleRollback) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ReleaseRollbackPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal rollback payload: %w", err)
	}

	h.logger.Info("rollback task started",
		zap.Int64("release_id", payload.ReleaseID),
		zap.Int64("target_artifact_id", payload.TargetArtifactID),
	)

	if err := h.svc.ExecuteRollback(ctx, payload.ReleaseID, payload.TargetArtifactID, payload.TriggeredBy, payload.RollbackRecordID); err != nil {
		h.logger.Error("execute rollback failed",
			zap.Int64("release_id", payload.ReleaseID),
			zap.Error(err),
		)
		return fmt.Errorf("execute rollback for release %d: %w", payload.ReleaseID, err)
	}

	return nil
}

// Verify interface compliance at compile time.
var (
	_ asynq.Handler = (*HandlePlan)(nil)
	_ asynq.Handler = (*HandleDispatchShard)(nil)
	_ asynq.Handler = (*HandleFinalize)(nil)
	_ asynq.Handler = (*HandleRollback)(nil)
)

// ── Unused import guard ─────────────────────────────────────────────────────
var _ = tasks.TypeReleasePlan
