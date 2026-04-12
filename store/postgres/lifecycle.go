package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// LifecycleStore handles domain lifecycle state persistence.
// CLAUDE.md Critical Rule #1: ALL `domains.lifecycle_state` mutations go
// through this store's updateLifecycleStateTx. No other code in the codebase
// may issue `UPDATE domains SET lifecycle_state`. The CI gate
// `make check-lifecycle-writes` enforces this.
type LifecycleStore struct {
	db *sqlx.DB
}

func NewLifecycleStore(db *sqlx.DB) *LifecycleStore {
	return &LifecycleStore{db: db}
}

// LifecycleHistoryEntry represents a row in domain_lifecycle_history.
type LifecycleHistoryEntry struct {
	DomainID    int64  `db:"domain_id"`
	FromState   string `db:"from_state"`
	ToState     string `db:"to_state"`
	Reason      string `db:"reason"`
	TriggeredBy string `db:"triggered_by"`
}

// TransitionTx executes the lifecycle state transition within a transaction.
// It performs:
//  1. SELECT ... FOR UPDATE to lock the domain row
//  2. Optimistic check: current state must equal expectedFrom
//  3. UPDATE domains SET lifecycle_state (the ONLY such UPDATE in the codebase)
//  4. INSERT into domain_lifecycle_history
//
// Returns the new state on success, or an error. The caller MUST begin and
// commit/rollback the transaction.
func (s *LifecycleStore) TransitionTx(ctx context.Context, tx *sqlx.Tx, domainID int64, expectedFrom, to, reason, triggeredBy string) error {
	// Step 1: Lock the row and read current state
	var currentState string
	err := tx.GetContext(ctx, &currentState,
		`SELECT lifecycle_state FROM domains WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		domainID)
	if err != nil {
		return fmt.Errorf("lock domain %d: %w", domainID, err)
	}

	// Step 2: Optimistic check — the caller believes state is expectedFrom
	if currentState != expectedFrom {
		return fmt.Errorf("expected state %q but found %q: %w",
			expectedFrom, currentState, ErrLifecycleRaceCondition)
	}

	// Step 3: The single write path — `UPDATE domains SET lifecycle_state`
	if err := updateLifecycleStateTx(ctx, tx, domainID, to); err != nil {
		return err
	}

	// Step 4: Audit trail
	if err := insertLifecycleHistoryTx(ctx, tx, LifecycleHistoryEntry{
		DomainID:    domainID,
		FromState:   expectedFrom,
		ToState:     to,
		Reason:      reason,
		TriggeredBy: triggeredBy,
	}); err != nil {
		return err
	}

	return nil
}

// ErrLifecycleRaceCondition is the sentinel for concurrent state change.
// Re-exported here so callers importing only the store package can check it.
var ErrLifecycleRaceCondition = fmt.Errorf("lifecycle race condition")

// updateLifecycleStateTx is the ONLY function that issues
// `UPDATE domains SET lifecycle_state`. CI gate enforces this.
func updateLifecycleStateTx(ctx context.Context, tx *sqlx.Tx, domainID int64, newState string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE domains SET lifecycle_state = $1, updated_at = NOW() WHERE id = $2`,
		newState, domainID)
	if err != nil {
		return fmt.Errorf("update lifecycle_state for domain %d: %w", domainID, err)
	}
	return nil
}

// insertLifecycleHistoryTx appends a row to domain_lifecycle_history.
func insertLifecycleHistoryTx(ctx context.Context, tx *sqlx.Tx, entry LifecycleHistoryEntry) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO domain_lifecycle_history (domain_id, from_state, to_state, reason, triggered_by)
		 VALUES ($1, $2, $3, $4, $5)`,
		entry.DomainID, entry.FromState, entry.ToState, entry.Reason, entry.TriggeredBy)
	if err != nil {
		return fmt.Errorf("insert lifecycle history: %w", err)
	}
	return nil
}

// GetCurrentState reads the lifecycle_state for a domain (non-locking).
func (s *LifecycleStore) GetCurrentState(ctx context.Context, domainID int64) (string, error) {
	var state string
	err := s.db.GetContext(ctx, &state,
		`SELECT lifecycle_state FROM domains WHERE id = $1 AND deleted_at IS NULL`, domainID)
	if err != nil {
		return "", fmt.Errorf("get lifecycle state for domain %d: %w", domainID, err)
	}
	return state, nil
}

// LifecycleHistory returns the state history for a domain, most recent first.
type LifecycleHistoryRow struct {
	ID          int64     `db:"id"`
	DomainID    int64     `db:"domain_id"`
	FromState   *string   `db:"from_state"`
	ToState     string    `db:"to_state"`
	Reason      *string   `db:"reason"`
	TriggeredBy string    `db:"triggered_by"`
	CreatedAt   time.Time `db:"created_at"`
}

func (s *LifecycleStore) GetHistory(ctx context.Context, domainID int64, limit int) ([]LifecycleHistoryRow, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []LifecycleHistoryRow
	err := s.db.SelectContext(ctx, &rows,
		`SELECT id, domain_id, from_state, to_state, reason, triggered_by, created_at
		 FROM domain_lifecycle_history
		 WHERE domain_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, domainID, limit)
	if err != nil {
		return nil, fmt.Errorf("get lifecycle history: %w", err)
	}
	return rows, nil
}

// BeginTx starts a new transaction. Callers must defer tx.Rollback() and
// call tx.Commit() on success.
func (s *LifecycleStore) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	return s.db.BeginTxx(ctx, nil)
}
