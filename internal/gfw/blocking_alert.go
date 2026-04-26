package gfw

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"domain-platform/internal/alert"
	"domain-platform/store/postgres"
)

// blockingAlertDetail is serialized into alert_events.detail.
type blockingAlertDetail struct {
	FQDN           string  `json:"fqdn"`
	BlockingType   string  `json:"blocking_type,omitempty"`
	Confidence     float64 `json:"confidence"`
	ProbeNodeID    string  `json:"probe_node_id,omitempty"`
	ControlNodeID  string  `json:"control_node_id,omitempty"`
}

// BlockingAlertService bridges the GFW verdict pipeline with the alert engine
// and the denormalized blocking state on the domains table.
//
// On every verdict:
//   - Updates domains.blocking_status / blocking_type / blocking_since / blocking_confidence.
//   - Fires or resolves an alert via alert.Engine with dedup key "gfw:blocked:{domainID}".
//
// Severity mapping (matches Critical Rule #8 dedup contract):
//
//	confidence ≥ 0.90 → P1 ("blocked")
//	confidence ≥ 0.70 → P2 ("possibly_blocked")
//	confidence ≥ 0.30 → P3 (borderline — state cleared, informational fire)
//	confidence < 0.30 → resolve any existing alert; clear blocking state
type BlockingAlertService struct {
	blockStore *postgres.GFWBlockingStore
	engine     *alert.Engine
	logger     *zap.Logger
}

// NewBlockingAlertService creates a BlockingAlertService.
func NewBlockingAlertService(
	blockStore *postgres.GFWBlockingStore,
	engine *alert.Engine,
	logger *zap.Logger,
) *BlockingAlertService {
	return &BlockingAlertService{
		blockStore: blockStore,
		engine:     engine,
		logger:     logger,
	}
}

// EvaluateAndAlert is called after every verdict is produced by VerdictService.
// It updates the denormalized blocking state and fires or resolves alerts.
func (s *BlockingAlertService) EvaluateAndAlert(ctx context.Context, v Verdict) error {
	dedupKey := fmt.Sprintf("gfw:blocked:%d", v.DomainID)

	// ── 1. Determine blocking status from confidence ──────────────────────────
	status, severity := classifyBlocking(v.Confidence, v.Blocking)

	// ── 2. Update denormalized blocking state on domains row ─────────────────
	var since *time.Time
	if status != postgres.BlockingStatusNone {
		now := v.MeasuredAt
		since = &now
	}

	if err := s.blockStore.UpdateDomainBlockingStatus(
		ctx,
		v.DomainID,
		status,
		v.Blocking, // raw blocking type (dns / tcp_ip / tls_sni / http-* / "")
		since,
		v.Confidence,
	); err != nil {
		// Non-fatal: log and continue — alert should still be attempted.
		s.logger.Warn("blocking alert: update domain blocking status failed",
			zap.Int64("domain_id", v.DomainID),
			zap.String("fqdn", v.FQDN),
			zap.Error(err),
		)
	}

	// ── 3. Resolve alert if domain is now accessible ──────────────────────────
	if v.Accessible || status == postgres.BlockingStatusNone {
		if err := s.engine.Resolve(ctx, dedupKey); err != nil {
			s.logger.Warn("blocking alert: resolve failed",
				zap.Int64("domain_id", v.DomainID),
				zap.String("dedup_key", dedupKey),
				zap.Error(err),
			)
		} else if v.Accessible {
			s.logger.Info("blocking alert: domain recovered",
				zap.Int64("domain_id", v.DomainID),
				zap.String("fqdn", v.FQDN),
				zap.Float64("confidence", v.Confidence),
			)
		}
		return nil
	}

	// ── 4. Fire alert for active blocking ─────────────────────────────────────
	detail, err := json.Marshal(blockingAlertDetail{
		FQDN:          v.FQDN,
		BlockingType:  v.Blocking,
		Confidence:    v.Confidence,
		ProbeNodeID:   v.ProbeNodeID,
		ControlNodeID: v.ControlNodeID,
	})
	if err != nil {
		return fmt.Errorf("blocking alert: marshal detail: %w", err)
	}

	title := buildTitle(v.FQDN, v.Blocking, v.Confidence)
	domainID := v.DomainID
	ev := &postgres.AlertEvent{
		Severity:   severity,
		Source:     "gfw",
		TargetKind: "domain",
		TargetID:   &domainID,
		Title:      title,
		Detail:     detail,
		DedupKey:   &dedupKey,
	}

	if err := s.engine.Fire(ctx, ev); err != nil {
		return fmt.Errorf("blocking alert: fire: %w", err)
	}

	s.logger.Info("blocking alert fired",
		zap.Int64("domain_id", v.DomainID),
		zap.String("fqdn", v.FQDN),
		zap.String("severity", severity),
		zap.String("blocking", v.Blocking),
		zap.Float64("confidence", v.Confidence),
	)

	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// classifyBlocking maps a confidence score to a blocking status constant and
// an alert severity string.
//
// Returns ("", "") when confidence < 0.30 — caller should resolve the alert.
func classifyBlocking(confidence float64, blockingType string) (status, severity string) {
	// Only classify if there is an actual blocking signal.
	if blockingType == BlockingNone || blockingType == "" {
		return postgres.BlockingStatusNone, ""
	}

	switch {
	case confidence >= 0.90:
		return postgres.BlockingStatusBlocked, "P1"
	case confidence >= 0.70:
		return postgres.BlockingStatusPossiblyBlocked, "P2"
	case confidence >= 0.30:
		// Below the "possibly blocked" threshold — borderline signal; do not
		// persist blocking state but do fire a low-severity alert so operators
		// can monitor emerging patterns.
		return postgres.BlockingStatusNone, "P3"
	default:
		return postgres.BlockingStatusNone, ""
	}
}

// buildTitle produces a human-readable alert title.
func buildTitle(fqdn, blockingType string, confidence float64) string {
	pct := int(confidence * 100)
	switch blockingType {
	case BlockingDNS:
		return fmt.Sprintf("GFW: DNS blocking detected on %s (%d%% confidence)", fqdn, pct)
	case BlockingTCPIP:
		return fmt.Sprintf("GFW: TCP/IP blocking detected on %s (%d%% confidence)", fqdn, pct)
	case BlockingTLSSNI:
		return fmt.Sprintf("GFW: TLS SNI blocking detected on %s (%d%% confidence)", fqdn, pct)
	case BlockingHTTPFailure:
		return fmt.Sprintf("GFW: HTTP failure detected on %s (%d%% confidence)", fqdn, pct)
	case BlockingHTTPDiff:
		return fmt.Sprintf("GFW: HTTP content difference detected on %s (%d%% confidence)", fqdn, pct)
	default:
		return fmt.Sprintf("GFW: blocking detected on %s (%d%% confidence)", fqdn, pct)
	}
}
