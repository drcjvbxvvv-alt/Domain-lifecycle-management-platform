package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Release maps to the releases table row.
type Release struct {
	ID                int64      `db:"id"`
	UUID              string     `db:"uuid"`
	ReleaseID         string     `db:"release_id"`
	ProjectID         int64      `db:"project_id"`
	TemplateVersionID int64      `db:"template_version_id"`
	ArtifactID        *int64     `db:"artifact_id"`
	ReleaseType       string     `db:"release_type"`
	TriggerSource     string     `db:"trigger_source"`
	Status            string     `db:"status"`
	RequiresApproval  bool       `db:"requires_approval"`
	CanaryShardSize   int        `db:"canary_shard_size"`
	ShardSize         int        `db:"shard_size"`
	TotalDomains      *int       `db:"total_domains"`
	TotalShards       *int       `db:"total_shards"`
	SuccessCount      int        `db:"success_count"`
	FailureCount      int        `db:"failure_count"`
	Description       *string    `db:"description"`
	CreatedAt         time.Time  `db:"created_at"`
	CreatedBy         *int64     `db:"created_by"`
	StartedAt         *time.Time `db:"started_at"`
	EndedAt           *time.Time `db:"ended_at"`
}

// ReleaseScope maps to the release_scopes table row.
type ReleaseScope struct {
	ID          int64  `db:"id"`
	ReleaseID   int64  `db:"release_id"`
	DomainID    int64  `db:"domain_id"`
	HostGroupID *int64 `db:"host_group_id"`
}

// ReleaseShard maps to the release_shards table row.
type ReleaseShard struct {
	ID           int64      `db:"id"`
	ReleaseID    int64      `db:"release_id"`
	ShardIndex   int        `db:"shard_index"`
	IsCanary     bool       `db:"is_canary"`
	DomainCount  int        `db:"domain_count"`
	Status       string     `db:"status"`
	StartedAt    *time.Time `db:"started_at"`
	EndedAt      *time.Time `db:"ended_at"`
	SuccessCount int        `db:"success_count"`
	FailureCount int        `db:"failure_count"`
	PauseReason  *string    `db:"pause_reason"`
}

// ReleaseStateHistoryRow maps to the release_state_history table.
type ReleaseStateHistoryRow struct {
	ID          int64     `db:"id"`
	ReleaseID   int64     `db:"release_id"`
	FromState   *string   `db:"from_state"`
	ToState     string    `db:"to_state"`
	Reason      *string   `db:"reason"`
	TriggeredBy string    `db:"triggered_by"`
	CreatedAt   time.Time `db:"created_at"`
}

var (
	ErrReleaseNotFound      = errors.New("release not found")
	ErrReleaseRaceCondition = fmt.Errorf("release state race condition")
)

// ReleaseStore handles release persistence.
// CLAUDE.md Critical Rule #1: ALL `releases.status` mutations go through
// this store's updateReleaseStatusTx. No other code in the codebase may issue
// `UPDATE releases SET status`. The CI gate `make check-release-writes` enforces this.
type ReleaseStore struct {
	db *sqlx.DB
}

func NewReleaseStore(db *sqlx.DB) *ReleaseStore {
	return &ReleaseStore{db: db}
}

const releaseColumns = `id, uuid, release_id, project_id, template_version_id, artifact_id,
	release_type, trigger_source, status, requires_approval,
	canary_shard_size, shard_size, total_domains, total_shards,
	success_count, failure_count, description,
	created_at, created_by, started_at, ended_at`

// Create inserts a new release in the "pending" state.
func (s *ReleaseStore) Create(ctx context.Context, r *Release) (*Release, error) {
	var out Release
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO releases
		   (release_id, project_id, template_version_id, release_type,
		    trigger_source, description, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING `+releaseColumns,
		r.ReleaseID, r.ProjectID, r.TemplateVersionID, r.ReleaseType,
		r.TriggerSource, r.Description, r.CreatedBy,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create release: %w", err)
	}
	return &out, nil
}

// GetByID fetches a release by its database ID.
func (s *ReleaseStore) GetByID(ctx context.Context, id int64) (*Release, error) {
	var r Release
	err := s.db.GetContext(ctx, &r,
		`SELECT `+releaseColumns+` FROM releases WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReleaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get release by id: %w", err)
	}
	return &r, nil
}

// GetByReleaseID fetches a release by its human-readable release_id.
func (s *ReleaseStore) GetByReleaseID(ctx context.Context, releaseID string) (*Release, error) {
	var r Release
	err := s.db.GetContext(ctx, &r,
		`SELECT `+releaseColumns+` FROM releases WHERE release_id = $1`, releaseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReleaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get release by release_id: %w", err)
	}
	return &r, nil
}

// ListByProject returns releases for a project with cursor pagination.
func (s *ReleaseStore) ListByProject(ctx context.Context, projectID int64, cursor int64, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var items []Release
	err := s.db.SelectContext(ctx, &items,
		`SELECT `+releaseColumns+` FROM releases
		 WHERE project_id = $1 AND id > $2
		 ORDER BY id ASC LIMIT $3`,
		projectID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list releases by project: %w", err)
	}
	return items, nil
}

// CountByProject returns the total releases for a project.
func (s *ReleaseStore) CountByProject(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM releases WHERE project_id = $1`, projectID)
	if err != nil {
		return 0, fmt.Errorf("count releases: %w", err)
	}
	return count, nil
}

// SetArtifactID links an artifact to a release.
func (s *ReleaseStore) SetArtifactID(ctx context.Context, releaseDBID int64, artifactDBID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET artifact_id = $1 WHERE id = $2`,
		artifactDBID, releaseDBID)
	if err != nil {
		return fmt.Errorf("set artifact_id on release %d: %w", releaseDBID, err)
	}
	return nil
}

// SetTotals sets the total_domains and total_shards after planning.
func (s *ReleaseStore) SetTotals(ctx context.Context, releaseDBID int64, totalDomains, totalShards int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET total_domains = $1, total_shards = $2 WHERE id = $3`,
		totalDomains, totalShards, releaseDBID)
	if err != nil {
		return fmt.Errorf("set totals on release %d: %w", releaseDBID, err)
	}
	return nil
}

// SetStartedAt marks when a release begins executing.
func (s *ReleaseStore) SetStartedAt(ctx context.Context, releaseDBID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET started_at = NOW() WHERE id = $1`, releaseDBID)
	if err != nil {
		return fmt.Errorf("set started_at on release %d: %w", releaseDBID, err)
	}
	return nil
}

// SetEndedAt marks when a release finishes (succeeded, failed, cancelled, rolled_back).
func (s *ReleaseStore) SetEndedAt(ctx context.Context, releaseDBID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET ended_at = NOW() WHERE id = $1`, releaseDBID)
	if err != nil {
		return fmt.Errorf("set ended_at on release %d: %w", releaseDBID, err)
	}
	return nil
}

// IncrementSuccess atomically increments success_count by 1.
func (s *ReleaseStore) IncrementSuccess(ctx context.Context, releaseDBID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET success_count = success_count + 1 WHERE id = $1`, releaseDBID)
	return err
}

// IncrementFailure atomically increments failure_count by 1.
func (s *ReleaseStore) IncrementFailure(ctx context.Context, releaseDBID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET failure_count = failure_count + 1 WHERE id = $1`, releaseDBID)
	return err
}

// IncrementSuccessBy atomically increments success_count by n.
func (s *ReleaseStore) IncrementSuccessBy(ctx context.Context, releaseDBID int64, n int) error {
	if n == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET success_count = success_count + $1 WHERE id = $2`, n, releaseDBID)
	if err != nil {
		return fmt.Errorf("increment success by %d on release %d: %w", n, releaseDBID, err)
	}
	return nil
}

// IncrementFailureBy atomically increments failure_count by n.
func (s *ReleaseStore) IncrementFailureBy(ctx context.Context, releaseDBID int64, n int) error {
	if n == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET failure_count = failure_count + $1 WHERE id = $2`, n, releaseDBID)
	if err != nil {
		return fmt.Errorf("increment failure by %d on release %d: %w", n, releaseDBID, err)
	}
	return nil
}

// ── Transition (single write path) ──────────────────────────────────────────

// TransitionTx executes the release state transition within a transaction.
// It performs:
//  1. SELECT ... FOR UPDATE to lock the release row
//  2. Optimistic check: current status must equal expectedFrom
//  3. UPDATE releases SET status (the ONLY such UPDATE in the codebase)
//  4. INSERT into release_state_history
//
// Returns an error on failure. The caller MUST begin and commit/rollback the tx.
func (s *ReleaseStore) TransitionTx(ctx context.Context, tx *sqlx.Tx, releaseDBID int64, expectedFrom, to, reason, triggeredBy string) error {
	// Step 1: Lock the row and read current status
	var currentStatus string
	err := tx.GetContext(ctx, &currentStatus,
		`SELECT status FROM releases WHERE id = $1 FOR UPDATE`,
		releaseDBID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReleaseNotFound
	}
	if err != nil {
		return fmt.Errorf("lock release %d: %w", releaseDBID, err)
	}

	// Step 2: Optimistic check
	if currentStatus != expectedFrom {
		return fmt.Errorf("expected status %q but found %q: %w",
			expectedFrom, currentStatus, ErrReleaseRaceCondition)
	}

	// Step 3: The single write path — `UPDATE releases SET status`
	if err := updateReleaseStatusTx(ctx, tx, releaseDBID, to); err != nil {
		return err
	}

	// Step 4: Audit trail
	if err := insertReleaseStateHistoryTx(ctx, tx, releaseDBID, expectedFrom, to, reason, triggeredBy); err != nil {
		return err
	}

	return nil
}

// updateReleaseStatusTx is the ONLY function that issues
// `UPDATE releases SET status`. CI gate `make check-release-writes` enforces this.
func updateReleaseStatusTx(ctx context.Context, tx *sqlx.Tx, releaseDBID int64, newStatus string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE releases SET status = $1 WHERE id = $2`,
		newStatus, releaseDBID)
	if err != nil {
		return fmt.Errorf("update release status for release %d: %w", releaseDBID, err)
	}
	return nil
}

// insertReleaseStateHistoryTx appends a row to release_state_history.
func insertReleaseStateHistoryTx(ctx context.Context, tx *sqlx.Tx, releaseDBID int64, from, to, reason, triggeredBy string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO release_state_history (release_id, from_state, to_state, reason, triggered_by)
		 VALUES ($1, $2, $3, $4, $5)`,
		releaseDBID, from, to, reason, triggeredBy)
	if err != nil {
		return fmt.Errorf("insert release state history: %w", err)
	}
	return nil
}

// GetHistory returns the state history for a release, most recent first.
func (s *ReleaseStore) GetHistory(ctx context.Context, releaseDBID int64, limit int) ([]ReleaseStateHistoryRow, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []ReleaseStateHistoryRow
	err := s.db.SelectContext(ctx, &rows,
		`SELECT id, release_id, from_state, to_state, reason, triggered_by, created_at
		 FROM release_state_history
		 WHERE release_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, releaseDBID, limit)
	if err != nil {
		return nil, fmt.Errorf("get release state history: %w", err)
	}
	return rows, nil
}

// GetLastSucceeded returns the most recent succeeded release for a project
// with an ID strictly less than beforeID (used to find the rollback target).
func (s *ReleaseStore) GetLastSucceeded(ctx context.Context, projectID int64, beforeID int64) (*Release, error) {
	var r Release
	err := s.db.GetContext(ctx, &r,
		`SELECT `+releaseColumns+` FROM releases
		 WHERE project_id = $1 AND status = 'succeeded' AND artifact_id IS NOT NULL AND id < $2
		 ORDER BY id DESC LIMIT 1`,
		projectID, beforeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReleaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get last succeeded release: %w", err)
	}
	return &r, nil
}

// BeginTx starts a new transaction.
func (s *ReleaseStore) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	return s.db.BeginTxx(ctx, nil)
}

// ── Release Scope ───────────────────────────────────────────────────────────

// CreateScope inserts a release scope row.
func (s *ReleaseStore) CreateScope(ctx context.Context, scope *ReleaseScope) (*ReleaseScope, error) {
	var out ReleaseScope
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO release_scopes (release_id, domain_id, host_group_id)
		 VALUES ($1, $2, $3) RETURNING id, release_id, domain_id, host_group_id`,
		scope.ReleaseID, scope.DomainID, scope.HostGroupID).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create release scope: %w", err)
	}
	return &out, nil
}

// ListScopes returns all scopes for a release.
func (s *ReleaseStore) ListScopes(ctx context.Context, releaseDBID int64) ([]ReleaseScope, error) {
	var items []ReleaseScope
	err := s.db.SelectContext(ctx, &items,
		`SELECT id, release_id, domain_id, host_group_id
		 FROM release_scopes WHERE release_id = $1`, releaseDBID)
	if err != nil {
		return nil, fmt.Errorf("list release scopes: %w", err)
	}
	return items, nil
}

// ── Release Shard ───────────────────────────────────────────────────────────

// CreateShard inserts a release shard row.
func (s *ReleaseStore) CreateShard(ctx context.Context, shard *ReleaseShard) (*ReleaseShard, error) {
	var out ReleaseShard
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO release_shards (release_id, shard_index, is_canary, domain_count)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, release_id, shard_index, is_canary, domain_count, status,
		           started_at, ended_at, success_count, failure_count, pause_reason`,
		shard.ReleaseID, shard.ShardIndex, shard.IsCanary, shard.DomainCount).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create release shard: %w", err)
	}
	return &out, nil
}

// ListShards returns all shards for a release, ordered by shard_index.
func (s *ReleaseStore) ListShards(ctx context.Context, releaseDBID int64) ([]ReleaseShard, error) {
	var items []ReleaseShard
	err := s.db.SelectContext(ctx, &items,
		`SELECT id, release_id, shard_index, is_canary, domain_count, status,
		        started_at, ended_at, success_count, failure_count, pause_reason
		 FROM release_shards WHERE release_id = $1 ORDER BY shard_index`, releaseDBID)
	if err != nil {
		return nil, fmt.Errorf("list release shards: %w", err)
	}
	return items, nil
}

// GetShardByID returns a shard by its DB ID.
func (s *ReleaseStore) GetShardByID(ctx context.Context, shardID int64) (*ReleaseShard, error) {
	var shard ReleaseShard
	err := s.db.GetContext(ctx, &shard,
		`SELECT id, release_id, shard_index, is_canary, domain_count, status,
		        started_at, ended_at, success_count, failure_count, pause_reason
		 FROM release_shards WHERE id = $1`, shardID)
	if err != nil {
		return nil, fmt.Errorf("get shard %d: %w", shardID, err)
	}
	return &shard, nil
}

// UpdateShardStatus updates a shard's status and optionally sets started_at or ended_at.
func (s *ReleaseStore) UpdateShardStatus(ctx context.Context, shardID int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE release_shards SET status = $1,
		 started_at = CASE WHEN $1 IN ('dispatching', 'running') AND started_at IS NULL THEN NOW() ELSE started_at END,
		 ended_at = CASE WHEN $1 IN ('succeeded', 'failed', 'cancelled') THEN NOW() ELSE ended_at END
		 WHERE id = $2`,
		status, shardID)
	if err != nil {
		return fmt.Errorf("update shard %d status: %w", shardID, err)
	}
	return nil
}

// UpdateShardCounts updates a shard's success and failure counts.
func (s *ReleaseStore) UpdateShardCounts(ctx context.Context, shardID int64, successCount, failureCount int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE release_shards SET success_count = $1, failure_count = $2 WHERE id = $3`,
		successCount, failureCount, shardID)
	if err != nil {
		return fmt.Errorf("update shard %d counts: %w", shardID, err)
	}
	return nil
}

// GetNextPendingShard returns the next shard with shard_index > afterIndex
// that is still in "pending" status, ordered by shard_index ascending.
// Returns nil (with no error) if no such shard exists (all shards done).
func (s *ReleaseStore) GetNextPendingShard(ctx context.Context, releaseID int64, afterIndex int) (*ReleaseShard, error) {
	var shard ReleaseShard
	err := s.db.GetContext(ctx, &shard,
		`SELECT id, release_id, shard_index, is_canary, domain_count, status,
		        started_at, ended_at, success_count, failure_count, pause_reason
		 FROM release_shards
		 WHERE release_id = $1 AND shard_index > $2 AND status = 'pending'
		 ORDER BY shard_index
		 LIMIT 1`,
		releaseID, afterIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get next pending shard for release %d after index %d: %w", releaseID, afterIndex, err)
	}
	return &shard, nil
}
