package probe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/alert"
	"domain-platform/internal/tasks"
)

// ── Schedule-all batch handler ─────────────────────────────────────────────────

// HandleScheduleAll is the asynq handler for TypeProbeScheduleAll.
// It enqueues one probe task per (policy, active-domain) combination.
type HandleScheduleAll struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandleScheduleAll(svc *Service, logger *zap.Logger) *HandleScheduleAll {
	return &HandleScheduleAll{svc: svc, logger: logger}
}

func (h *HandleScheduleAll) ProcessTask(ctx context.Context, t *asynq.Task) error {
	h.logger.Info("probe:schedule_all started")
	if err := h.svc.ScheduleAll(ctx); err != nil {
		return fmt.Errorf("probe schedule_all: %w", err)
	}
	return nil
}

// ── L1 handler ────────────────────────────────────────────────────────────────

// HandleL1 is the asynq handler for TypeProbeRunL1.
type HandleL1 struct {
	checker *L1Checker
	svc     *Service
	logger  *zap.Logger
}

func NewHandleL1(svc *Service, logger *zap.Logger) *HandleL1 {
	return &HandleL1{checker: NewL1Checker(), svc: svc, logger: logger}
}

func (h *HandleL1) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p RunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal l1 payload: %w", err)
	}
	return h.run(ctx, p)
}

func (h *HandleL1) run(ctx context.Context, p RunPayload) error {
	if err := h.svc.probeStore.ClaimTask(ctx, p.ProbeTaskID); err != nil {
		// Already claimed by another worker — skip silently.
		h.logger.Warn("probe l1: claim task failed — already running?",
			zap.Int64("probe_task_id", p.ProbeTaskID), zap.Error(err))
		return nil
	}

	result := h.checker.Check(ctx, CheckRequest{
		FQDN:           p.FQDN,
		ExpectedStatus: p.ExpectedStatus,
		TimeoutSeconds: p.TimeoutSeconds,
	})

	h.logger.Info("probe l1 result",
		zap.Int64("domain_id", p.DomainID),
		zap.String("fqdn", p.FQDN),
		zap.String("status", result.Status),
	)

	saveErr := h.svc.SaveResult(ctx, p.ProbeTaskID, p.DomainID, p.PolicyID, 1, result)
	if saveErr != nil {
		h.logger.Error("probe l1: save result failed", zap.Error(saveErr))
	}

	if result.Status == StatusFail || result.Status == StatusTimeout || result.Status == StatusError {
		fireProbeAlert(ctx, h.svc, h.logger, 1, "P1", p)
	}

	return h.svc.probeStore.CompleteTask(ctx, p.ProbeTaskID, result.ErrorMessage)
}

// ── L2 handler ────────────────────────────────────────────────────────────────

// HandleL2 is the asynq handler for TypeProbeRunL2.
type HandleL2 struct {
	checker *L2Checker
	svc     *Service
	logger  *zap.Logger
}

func NewHandleL2(svc *Service, logger *zap.Logger) *HandleL2 {
	return &HandleL2{checker: NewL2Checker(), svc: svc, logger: logger}
}

func (h *HandleL2) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p RunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal l2 payload: %w", err)
	}
	return h.run(ctx, p)
}

func (h *HandleL2) run(ctx context.Context, p RunPayload) error {
	if err := h.svc.probeStore.ClaimTask(ctx, p.ProbeTaskID); err != nil {
		h.logger.Warn("probe l2: claim task failed — already running?",
			zap.Int64("probe_task_id", p.ProbeTaskID), zap.Error(err))
		return nil
	}

	result := h.checker.Check(ctx, CheckRequest{
		FQDN:            p.FQDN,
		ExpectedStatus:  p.ExpectedStatus,
		ExpectedKeyword: p.ExpectedKeyword,
		ExpectedMetaTag: p.ExpectedMetaTag,
		TimeoutSeconds:  p.TimeoutSeconds,
	})

	h.logger.Info("probe l2 result",
		zap.Int64("domain_id", p.DomainID),
		zap.String("fqdn", p.FQDN),
		zap.String("status", result.Status),
	)

	saveErr := h.svc.SaveResult(ctx, p.ProbeTaskID, p.DomainID, p.PolicyID, 2, result)
	if saveErr != nil {
		h.logger.Error("probe l2: save result failed", zap.Error(saveErr))
	}

	if result.Status == StatusFail || result.Status == StatusTimeout || result.Status == StatusError {
		fireProbeAlert(ctx, h.svc, h.logger, 2, "P2", p)
	}

	return h.svc.probeStore.CompleteTask(ctx, p.ProbeTaskID, result.ErrorMessage)
}

// ── L3 handler ────────────────────────────────────────────────────────────────

// HandleL3 is the asynq handler for TypeProbeRunL3.
type HandleL3 struct {
	checker *L3Checker
	svc     *Service
	logger  *zap.Logger
}

func NewHandleL3(svc *Service, logger *zap.Logger) *HandleL3 {
	return &HandleL3{checker: NewL3Checker(), svc: svc, logger: logger}
}

func (h *HandleL3) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p RunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal l3 payload: %w", err)
	}
	return h.run(ctx, p)
}

func (h *HandleL3) run(ctx context.Context, p RunPayload) error {
	if err := h.svc.probeStore.ClaimTask(ctx, p.ProbeTaskID); err != nil {
		h.logger.Warn("probe l3: claim task failed — already running?",
			zap.Int64("probe_task_id", p.ProbeTaskID), zap.Error(err))
		return nil
	}

	result := h.checker.Check(ctx, CheckRequest{
		FQDN:            p.FQDN,
		ExpectedKeyword: p.ExpectedKeyword,
		TimeoutSeconds:  p.TimeoutSeconds,
		HealthPath:      p.HealthPath,
	})

	h.logger.Info("probe l3 result",
		zap.Int64("domain_id", p.DomainID),
		zap.String("fqdn", p.FQDN),
		zap.String("status", result.Status),
	)

	saveErr := h.svc.SaveResult(ctx, p.ProbeTaskID, p.DomainID, p.PolicyID, 3, result)
	if saveErr != nil {
		h.logger.Error("probe l3: save result failed", zap.Error(saveErr))
	}

	if result.Status == StatusFail || result.Status == StatusTimeout || result.Status == StatusError {
		fireProbeAlert(ctx, h.svc, h.logger, 3, "P3", p)
	}

	return h.svc.probeStore.CompleteTask(ctx, p.ProbeTaskID, result.ErrorMessage)
}

// ── helpers ───────────────────────────────────────────────────────────────────

// fireProbeAlert enqueues a TypeAlertFire task when a probe check fails.
// Severity: L1=P1 (infrastructure), L2=P2 (release mismatch), L3=P3 (health).
// Dedup key: probe:lN:domain:<id> — suppresses duplicate alerts within 1 hour.
func fireProbeAlert(ctx context.Context, svc *Service, logger *zap.Logger, tier int, severity string, p RunPayload) {
	domainID := p.DomainID
	dedupKey := fmt.Sprintf("probe:l%d:domain:%d", tier, domainID)
	title := fmt.Sprintf("L%d probe failed: %s", tier, p.FQDN)

	payload := tasks.AlertFirePayload{
		Severity:   severity,
		Source:     "probe",
		TargetKind: "domain",
		TargetID:   &domainID,
		Title:      title,
		DedupKey:   dedupKey,
	}
	if err := alert.EnqueueFire(ctx, svc.asynqClient, payload); err != nil {
		logger.Warn("probe: enqueue alert fire failed",
			zap.String("dedup_key", dedupKey),
			zap.Error(err),
		)
	}
}
