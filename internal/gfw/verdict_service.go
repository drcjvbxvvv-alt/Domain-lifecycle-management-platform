package gfw

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"domain-platform/pkg/probeprotocol"
	"domain-platform/store/postgres"
)

// VerdictService orchestrates the classification pipeline:
//
//  1. Fetch latest probe + control measurements for a domain.
//  2. Run the Analyzer decision tree to produce a Verdict.
//  3. Update confidence via ConfidenceTracker.
//  4. Persist the Verdict to gfw_verdicts.
//  5. Evaluate blocking state and fire/resolve alerts via BlockingAlertService.
//
// It is called:
//   - After each measurement batch is stored (by MeasurementService.StoreMeasurements).
//   - On demand from the REST API (GET /gfw/verdicts/:domainId/latest).
type VerdictService struct {
	msvc         *MeasurementService
	vstore       *postgres.GFWVerdictStore
	analyzer     *Analyzer
	confidence   ConfidenceTracker
	blockingAlert *BlockingAlertService // may be nil (optional dependency)
	logger       *zap.Logger
}

// NewVerdictService creates a VerdictService.
func NewVerdictService(
	msvc *MeasurementService,
	vstore *postgres.GFWVerdictStore,
	analyzer *Analyzer,
	confidence ConfidenceTracker,
	blockingAlert *BlockingAlertService,
	logger *zap.Logger,
) *VerdictService {
	return &VerdictService{
		msvc:         msvc,
		vstore:       vstore,
		analyzer:     analyzer,
		confidence:   confidence,
		blockingAlert: blockingAlert,
		logger:       logger,
	}
}

// AnalyzeAndStore fetches the latest measurements for domainID, runs the
// decision tree, updates confidence, and persists the verdict.
// Returns the produced Verdict.  If no probe measurement exists yet, returns
// nil without error.
func (s *VerdictService) AnalyzeAndStore(ctx context.Context, domainID int64) (*Verdict, error) {
	// 1. Fetch latest measurements.
	probeRow, controlRow, err := s.msvc.GetLatestMeasurements(ctx, domainID)
	if err != nil {
		return nil, fmt.Errorf("fetch measurements domain %d: %w", domainID, err)
	}
	if probeRow == nil {
		// No data yet — nothing to classify.
		return nil, nil
	}

	// 2. Deserialize probe measurement.
	probe, err := rowToMeasurement(probeRow)
	if err != nil {
		return nil, fmt.Errorf("decode probe measurement: %w", err)
	}

	// 3. Deserialize control measurement (may be nil).
	var control *probeprotocol.Measurement
	if controlRow != nil {
		m, err := rowToMeasurement(controlRow)
		if err != nil {
			s.logger.Warn("failed to decode control measurement — proceeding without",
				zap.Error(err), zap.Int64("domain_id", domainID))
		} else {
			control = m
		}
	}

	// 4. Update confidence.
	// We need to run the preliminary classification to know the blocking type
	// before we can update the tracker, so we classify at confidence=0 first.
	preliminary := s.analyzer.Classify(probe, control, 0)
	conf, err := s.confidence.Record(ctx, domainID, probe.NodeID, preliminary.Blocking)
	if err != nil {
		// Non-fatal — confidence tracking is best-effort.
		s.logger.Warn("confidence record failed", zap.Error(err), zap.Int64("domain_id", domainID))
		conf = 0
	}

	// 5. Re-classify with the real confidence score.
	verdict := s.analyzer.Classify(probe, control, conf)

	// 6. Persist verdict.
	if err := s.persistVerdict(ctx, verdict); err != nil {
		return &verdict, fmt.Errorf("persist verdict domain %d: %w", domainID, err)
	}

	s.logger.Info("verdict stored",
		zap.Int64("domain_id", domainID),
		zap.String("fqdn", verdict.FQDN),
		zap.String("blocking", verdict.Blocking),
		zap.Float64("confidence", verdict.Confidence),
		zap.Bool("accessible", verdict.Accessible),
	)

	// 7. Update blocking state + fire/resolve alert (best-effort).
	if s.blockingAlert != nil {
		if err := s.blockingAlert.EvaluateAndAlert(ctx, verdict); err != nil {
			s.logger.Warn("blocking alert evaluation failed",
				zap.Int64("domain_id", domainID),
				zap.Error(err),
			)
		}
	}

	return &verdict, nil
}

// LatestVerdict returns the last persisted verdict for a domain without
// triggering a re-analysis.  Returns nil when no verdict exists yet.
func (s *VerdictService) LatestVerdict(ctx context.Context, domainID int64) (*postgres.GFWVerdict, error) {
	row, err := s.vstore.LatestVerdict(ctx, domainID)
	if err != nil {
		return nil, fmt.Errorf("latest verdict domain %d: %w", domainID, err)
	}
	return row, nil
}

// ListVerdicts returns the verdict history for a domain.
func (s *VerdictService) ListVerdicts(ctx context.Context, domainID int64, limit int) ([]postgres.GFWVerdict, error) {
	rows, err := s.vstore.ListVerdicts(ctx, domainID, limit)
	if err != nil {
		return nil, fmt.Errorf("list verdicts domain %d: %w", domainID, err)
	}
	return rows, nil
}

// ActivelyBlockedDomains returns a summary list of all currently-blocked domains.
func (s *VerdictService) ActivelyBlockedDomains(ctx context.Context) ([]postgres.VerdictSummary, error) {
	rows, err := s.vstore.ActivelyBlockedDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("actively blocked domains: %w", err)
	}
	return rows, nil
}

// ── private helpers ──────────────────────────────────────────────────────────

// persistVerdict serializes a Verdict and inserts it into gfw_verdicts.
func (s *VerdictService) persistVerdict(ctx context.Context, v Verdict) error {
	detailJSON, err := json.Marshal(v.Detail)
	if err != nil {
		return fmt.Errorf("marshal verdict detail: %w", err)
	}

	row := postgres.GFWVerdict{
		DomainID:      v.DomainID,
		Blocking:      v.Blocking,
		Accessible:    v.Accessible,
		Confidence:    v.Confidence,
		ProbeNodeID:   v.ProbeNodeID,
		ControlNodeID: v.ControlNodeID,
		Detail:        detailJSON,
		MeasuredAt:    v.MeasuredAt,
	}
	if v.DNSConsistency != "" {
		row.DNSConsistency.Valid = true
		row.DNSConsistency.String = v.DNSConsistency
	}

	_, err = s.vstore.InsertVerdict(ctx, row)
	return err
}

// rowToMeasurement decodes a GFWMeasurement DB row back into a
// probeprotocol.Measurement for analysis.
func rowToMeasurement(row *postgres.GFWMeasurement) (*probeprotocol.Measurement, error) {
	m := &probeprotocol.Measurement{
		DomainID:   row.DomainID,
		FQDN:       row.FQDN,
		NodeID:     row.NodeID,
		NodeRole:   row.NodeRole,
		MeasuredAt: row.MeasuredAt,
	}
	if row.TotalMS != nil {
		m.TotalMS = int64(*row.TotalMS)
	}

	if len(row.DNSResult) > 0 && string(row.DNSResult) != "null" {
		var dns probeprotocol.DNSResult
		if err := json.Unmarshal(row.DNSResult, &dns); err != nil {
			return nil, fmt.Errorf("decode dns_result: %w", err)
		}
		m.DNS = &dns
	}

	if len(row.TCPResults) > 0 && string(row.TCPResults) != "null" {
		if err := json.Unmarshal(row.TCPResults, &m.TCP); err != nil {
			return nil, fmt.Errorf("decode tcp_results: %w", err)
		}
	}

	if len(row.TLSResults) > 0 && string(row.TLSResults) != "null" {
		if err := json.Unmarshal(row.TLSResults, &m.TLS); err != nil {
			return nil, fmt.Errorf("decode tls_results: %w", err)
		}
	}

	if len(row.HTTPResult) > 0 && string(row.HTTPResult) != "null" {
		var http probeprotocol.HTTPResult
		if err := json.Unmarshal(row.HTTPResult, &http); err != nil {
			return nil, fmt.Errorf("decode http_result: %w", err)
		}
		m.HTTP = &http
	}

	return m, nil
}

