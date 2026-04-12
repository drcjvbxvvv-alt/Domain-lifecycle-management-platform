package agent

import (
	"context"
	"time"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// HealthChecker scans for stale online agents and transitions them to offline.
// Used by the asynq worker as a periodic task handler.
type HealthChecker struct {
	store  *postgres.AgentStore
	svc    *Service
	logger *zap.Logger
}

func NewHealthChecker(store *postgres.AgentStore, svc *Service, logger *zap.Logger) *HealthChecker {
	return &HealthChecker{store: store, svc: svc, logger: logger}
}

// CheckStaleAgents finds agents that are online/busy/idle but have not
// heartbeated within the given threshold, and transitions them to offline.
// Default threshold: 90 seconds.
func (h *HealthChecker) CheckStaleAgents(ctx context.Context) error {
	threshold := 90 * time.Second

	stale, err := h.store.ListStaleOnlineAgents(ctx, threshold)
	if err != nil {
		return err
	}

	if len(stale) == 0 {
		return nil
	}

	h.logger.Info("found stale agents", zap.Int("count", len(stale)))

	for _, a := range stale {
		err := h.svc.TransitionAgent(ctx, a.ID, a.Status, "offline",
			"heartbeat missed (>90s)", "health_checker")
		if err != nil {
			h.logger.Warn("failed to transition stale agent to offline",
				zap.String("agent_id", a.AgentID),
				zap.String("current_status", a.Status),
				zap.Error(err),
			)
			continue
		}
		h.logger.Info("agent transitioned to offline",
			zap.String("agent_id", a.AgentID),
			zap.String("from", a.Status),
		)
	}

	return nil
}
