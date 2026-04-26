package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// GFWVerdict maps to one row in gfw_verdicts.
type GFWVerdict struct {
	ID             int64           `db:"id"`
	DomainID       int64           `db:"domain_id"`
	Blocking       string          `db:"blocking"`
	Accessible     bool            `db:"accessible"`
	DNSConsistency sql.NullString  `db:"dns_consistency"`
	Confidence     float64         `db:"confidence"`
	ProbeNodeID    string          `db:"probe_node_id"`
	ControlNodeID  string          `db:"control_node_id"`
	Detail         json.RawMessage `db:"detail"`
	MeasuredAt     time.Time       `db:"measured_at"`
	CreatedAt      time.Time       `db:"created_at"`
}

// VerdictSummary is a lightweight projection used by dashboard / list endpoints.
type VerdictSummary struct {
	DomainID       int64     `db:"domain_id"`
	Blocking       string    `db:"blocking"`
	Accessible     bool      `db:"accessible"`
	Confidence     float64   `db:"confidence"`
	ProbeNodeID    string    `db:"probe_node_id"`
	MeasuredAt     time.Time `db:"measured_at"`
}

// GFWVerdictStore handles persistence of GFW verdicts.
type GFWVerdictStore struct {
	db *sqlx.DB
}

// NewGFWVerdictStore creates a new verdict store.
func NewGFWVerdictStore(db *sqlx.DB) *GFWVerdictStore {
	return &GFWVerdictStore{db: db}
}

// InsertVerdict persists a verdict and returns the assigned row ID.
func (s *GFWVerdictStore) InsertVerdict(ctx context.Context, v GFWVerdict) (int64, error) {
	const q = `
		INSERT INTO gfw_verdicts
			(domain_id, blocking, accessible, dns_consistency, confidence,
			 probe_node_id, control_node_id, detail, measured_at)
		VALUES
			(:domain_id, :blocking, :accessible, :dns_consistency, :confidence,
			 :probe_node_id, :control_node_id, :detail, :measured_at)
		RETURNING id`

	rows, err := s.db.NamedQueryContext(ctx, q, v)
	if err != nil {
		return 0, fmt.Errorf("insert gfw verdict domain %d: %w", v.DomainID, err)
	}
	defer rows.Close()
	if rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan verdict id: %w", err)
		}
		return id, nil
	}
	return 0, fmt.Errorf("insert gfw verdict: no rows returned")
}

// LatestVerdict returns the most recent verdict for the given domain.
func (s *GFWVerdictStore) LatestVerdict(ctx context.Context, domainID int64) (*GFWVerdict, error) {
	const q = `
		SELECT id, domain_id, blocking, accessible, dns_consistency, confidence,
		       probe_node_id, control_node_id, detail, measured_at, created_at
		FROM gfw_verdicts
		WHERE domain_id = $1
		ORDER BY measured_at DESC
		LIMIT 1`

	var row GFWVerdict
	if err := s.db.GetContext(ctx, &row, q, domainID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("latest gfw verdict domain %d: %w", domainID, err)
	}
	return &row, nil
}

// ListVerdicts returns verdicts for a domain in descending order of measured_at.
func (s *GFWVerdictStore) ListVerdicts(ctx context.Context, domainID int64, limit int) ([]GFWVerdict, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT id, domain_id, blocking, accessible, dns_consistency, confidence,
		       probe_node_id, control_node_id, detail, measured_at, created_at
		FROM gfw_verdicts
		WHERE domain_id = $1
		ORDER BY measured_at DESC
		LIMIT $2`

	var rows []GFWVerdict
	if err := s.db.SelectContext(ctx, &rows, q, domainID, limit); err != nil {
		return nil, fmt.Errorf("list gfw verdicts domain %d: %w", domainID, err)
	}
	return rows, nil
}

// ActivelyBlockedDomains returns a summary of all domains that have a latest
// verdict with blocking != '' and blocking != 'indeterminate'.
func (s *GFWVerdictStore) ActivelyBlockedDomains(ctx context.Context) ([]VerdictSummary, error) {
	const q = `
		SELECT DISTINCT ON (domain_id)
		       domain_id, blocking, accessible, confidence, probe_node_id, measured_at
		FROM gfw_verdicts
		WHERE blocking != '' AND blocking != 'indeterminate'
		ORDER BY domain_id, measured_at DESC`

	var rows []VerdictSummary
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, fmt.Errorf("actively blocked domains: %w", err)
	}
	return rows, nil
}
