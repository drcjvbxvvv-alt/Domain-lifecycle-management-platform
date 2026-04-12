package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// RollbackRecord maps to the rollback_records table row.
type RollbackRecord struct {
	ID                int64      `db:"id"`
	UUID              string     `db:"uuid"`
	ReleaseID         int64      `db:"release_id"`
	RollbackReleaseID *int64     `db:"rollback_release_id"`
	TargetArtifactID  int64      `db:"target_artifact_id"`
	Scope             string     `db:"scope"` // "release" | "shard" | "domain"
	ScopeTargetID     *int64     `db:"scope_target_id"`
	Reason            string     `db:"reason"`
	TriggeredBy       *int64     `db:"triggered_by"`
	TriggeredAt       time.Time  `db:"triggered_at"`
	CompletedAt       *time.Time `db:"completed_at"`
	Success           *bool      `db:"success"`
}

// RollbackStore handles rollback_records persistence.
type RollbackStore struct {
	db *sqlx.DB
}

func NewRollbackStore(db *sqlx.DB) *RollbackStore {
	return &RollbackStore{db: db}
}

// Create inserts a new rollback_record row and returns it with the generated ID and UUID.
func (s *RollbackStore) Create(ctx context.Context, r *RollbackRecord) (*RollbackRecord, error) {
	var out RollbackRecord
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO rollback_records
		   (release_id, rollback_release_id, target_artifact_id, scope, scope_target_id, reason, triggered_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, uuid, release_id, rollback_release_id, target_artifact_id, scope,
		           scope_target_id, reason, triggered_by, triggered_at, completed_at, success`,
		r.ReleaseID, r.RollbackReleaseID, r.TargetArtifactID, r.Scope, r.ScopeTargetID,
		r.Reason, r.TriggeredBy,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create rollback record: %w", err)
	}
	return &out, nil
}

// Complete marks a rollback_record as finished with a success flag.
func (s *RollbackStore) Complete(ctx context.Context, id int64, success bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rollback_records SET completed_at = NOW(), success = $1 WHERE id = $2`,
		success, id)
	if err != nil {
		return fmt.Errorf("complete rollback record %d: %w", id, err)
	}
	return nil
}

// GetByRelease returns all rollback records for a release, most recent first.
func (s *RollbackStore) GetByRelease(ctx context.Context, releaseID int64) ([]RollbackRecord, error) {
	var rows []RollbackRecord
	err := s.db.SelectContext(ctx, &rows,
		`SELECT id, uuid, release_id, rollback_release_id, target_artifact_id, scope,
		        scope_target_id, reason, triggered_by, triggered_at, completed_at, success
		 FROM rollback_records WHERE release_id = $1 ORDER BY triggered_at DESC`,
		releaseID)
	if err != nil {
		return nil, fmt.Errorf("get rollback records for release %d: %w", releaseID, err)
	}
	return rows, nil
}
