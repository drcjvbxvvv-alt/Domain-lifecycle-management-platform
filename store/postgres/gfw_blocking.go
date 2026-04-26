package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ── Domain blocking status ─────────────────────────────────────────────────────

// BlockingStatus constants mirror the CHECK constraint in the migration.
const (
	BlockingStatusNone           = ""                 // cleared / never blocked
	BlockingStatusPossiblyBlocked = "possibly_blocked" // confidence 0.7–0.89
	BlockingStatusBlocked         = "blocked"          // confidence ≥ 0.90
)

// DomainBlockingState is the denormalized blocking summary stored on domains.
type DomainBlockingState struct {
	BlockingStatus     sql.NullString  `db:"blocking_status"`
	BlockingType       sql.NullString  `db:"blocking_type"`
	BlockingSince      *time.Time      `db:"blocking_since"`
	BlockingConfidence sql.NullFloat64 `db:"blocking_confidence"`
}

// BlockedDomainRow is the projection returned by ListBlockedDomains.
type BlockedDomainRow struct {
	ID                 int64          `db:"id"`
	FQDN               string         `db:"fqdn"`
	ProjectID          int64          `db:"project_id"`
	BlockingStatus     string         `db:"blocking_status"`
	BlockingType       sql.NullString `db:"blocking_type"`
	BlockingSince      *time.Time     `db:"blocking_since"`
	BlockingConfidence float64        `db:"blocking_confidence"`
}

// GFWStats is the aggregate summary for the GFW dashboard.
type GFWStats struct {
	TotalMonitored   int `db:"total_monitored"`
	TotalBlocked     int `db:"total_blocked"`
	TotalPossible    int `db:"total_possibly_blocked"`
	BlockedByDNS     int `db:"blocked_dns"`
	BlockedByTCP     int `db:"blocked_tcp_ip"`
	BlockedByTLS     int `db:"blocked_tls_sni"`
	BlockedByHTTP    int `db:"blocked_http"`
}

// GFWBlockingStore handles the denormalized blocking state on the domains table
// and the aggregated stats queries used by the GFW dashboard.
type GFWBlockingStore struct {
	db *sqlx.DB
}

// NewGFWBlockingStore creates a new blocking store.
func NewGFWBlockingStore(db *sqlx.DB) *GFWBlockingStore {
	return &GFWBlockingStore{db: db}
}

// UpdateDomainBlockingStatus writes the denormalized blocking state back onto
// the domains row.  Called by BlockingAlertService on every verdict.
//
// status == "" clears the blocking state (domain became accessible again).
func (s *GFWBlockingStore) UpdateDomainBlockingStatus(
	ctx context.Context,
	domainID int64,
	status string,
	blockingType string,
	since *time.Time,
	confidence float64,
) error {
	const q = `
		UPDATE domains
		SET blocking_status     = NULLIF($2, ''),
		    blocking_type       = NULLIF($3, ''),
		    blocking_since      = $4,
		    blocking_confidence = CASE WHEN $2 = '' THEN NULL ELSE $5 END,
		    updated_at          = NOW()
		WHERE id = $1`

	if _, err := s.db.ExecContext(ctx, q, domainID, status, blockingType, since, confidence); err != nil {
		return fmt.Errorf("update domain blocking status %d: %w", domainID, err)
	}
	return nil
}

// ListBlockedDomains returns all domains that currently have a non-null
// blocking_status, ordered by confidence descending.
func (s *GFWBlockingStore) ListBlockedDomains(ctx context.Context) ([]BlockedDomainRow, error) {
	const q = `
		SELECT id, fqdn, project_id,
		       blocking_status, blocking_type, blocking_since,
		       COALESCE(blocking_confidence, 0) AS blocking_confidence
		FROM   domains
		WHERE  blocking_status IS NOT NULL
		  AND  deleted_at IS NULL
		ORDER  BY blocking_confidence DESC, blocking_since ASC`

	var rows []BlockedDomainRow
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, fmt.Errorf("list blocked domains: %w", err)
	}
	return rows, nil
}

// GetGFWStats returns aggregate counts for the GFW dashboard summary cards.
func (s *GFWBlockingStore) GetGFWStats(ctx context.Context) (*GFWStats, error) {
	const q = `
		SELECT
		    (SELECT COUNT(*) FROM gfw_check_assignments WHERE enabled = true)  AS total_monitored,
		    COUNT(*) FILTER (WHERE blocking_status = 'blocked')                AS total_blocked,
		    COUNT(*) FILTER (WHERE blocking_status = 'possibly_blocked')       AS total_possibly_blocked,
		    COUNT(*) FILTER (WHERE blocking_type = 'dns')                      AS blocked_dns,
		    COUNT(*) FILTER (WHERE blocking_type = 'tcp_ip')                   AS blocked_tcp_ip,
		    COUNT(*) FILTER (WHERE blocking_type = 'tls_sni')                  AS blocked_tls_sni,
		    COUNT(*) FILTER (WHERE blocking_type IN ('http-failure','http-diff')) AS blocked_http
		FROM domains
		WHERE blocking_status IS NOT NULL AND deleted_at IS NULL`

	var stats GFWStats
	if err := s.db.GetContext(ctx, &stats, q); err != nil {
		return nil, fmt.Errorf("get gfw stats: %w", err)
	}
	return &stats, nil
}

// GetDomainBlockingState returns the current denormalized blocking state for
// a single domain (used by domain detail page GFW tab).
func (s *GFWBlockingStore) GetDomainBlockingState(ctx context.Context, domainID int64) (*DomainBlockingState, error) {
	const q = `
		SELECT blocking_status, blocking_type, blocking_since, blocking_confidence
		FROM   domains
		WHERE  id = $1 AND deleted_at IS NULL`

	var row DomainBlockingState
	if err := s.db.GetContext(ctx, &row, q, domainID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get domain blocking state %d: %w", domainID, err)
	}
	return &row, nil
}
