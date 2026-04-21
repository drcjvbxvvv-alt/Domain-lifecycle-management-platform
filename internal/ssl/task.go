package ssl

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
)

// CheckExpiryPayload is the JSON payload for TypeSSLCheckExpiry tasks.
type CheckExpiryPayload struct {
	DomainID int64  `json:"domain_id"`
	FQDN     string `json:"fqdn"`
}

// NewCheckExpiryTask creates an asynq task for a single-domain TLS probe.
func NewCheckExpiryTask(domainID int64, fqdn string) (*asynq.Task, error) {
	payload, err := json.Marshal(CheckExpiryPayload{DomainID: domainID, FQDN: fqdn})
	if err != nil {
		return nil, fmt.Errorf("marshal ssl check payload: %w", err)
	}
	return asynq.NewTask(
		tasks.TypeSSLCheckExpiry,
		payload,
		asynq.MaxRetry(2),
		asynq.Queue("default"),
	), nil
}

// HandleCheckExpiry is the asynq handler for TypeSSLCheckExpiry.
// It performs a single-domain TLS probe and upserts the result.
type HandleCheckExpiry struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandleCheckExpiry(svc *Service, logger *zap.Logger) *HandleCheckExpiry {
	return &HandleCheckExpiry{svc: svc, logger: logger}
}

func (h *HandleCheckExpiry) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p CheckExpiryPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal ssl check payload: %w", err)
	}
	if p.DomainID == 0 || p.FQDN == "" {
		return fmt.Errorf("invalid payload: domain_id and fqdn are required")
	}

	_, err := h.svc.CheckExpiry(ctx, p.DomainID, p.FQDN)
	if err != nil {
		h.logger.Warn("ssl:check_expiry failed",
			zap.Int64("domain_id", p.DomainID),
			zap.String("fqdn", p.FQDN),
			zap.Error(err),
		)
		// Return nil — TLS failures are common (unreachable hosts, self-signed certs).
		// Returning an error would trigger retries that are unlikely to succeed.
		return nil
	}
	return nil
}

// HandleCheckAllActive is the asynq handler for TypeSSLCheckAllActive.
// It fetches all active domains and runs the TLS probe for each.
type HandleCheckAllActive struct {
	svc    *Service
	logger *zap.Logger
}

func NewHandleCheckAllActive(svc *Service, logger *zap.Logger) *HandleCheckAllActive {
	return &HandleCheckAllActive{svc: svc, logger: logger}
}

func (h *HandleCheckAllActive) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	checked, failed := h.svc.CheckAllActive(ctx)
	h.logger.Info("ssl:check_all_active complete",
		zap.Int("checked", checked),
		zap.Int("failed", failed),
	)
	return nil
}
