package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ─── Models ──────────────────────────────────────────────────────────────────

// MaintenanceWindow represents a planned downtime window.
type MaintenanceWindow struct {
	ID          int64           `db:"id"`
	UUID        string          `db:"uuid"`
	Title       string          `db:"title"`
	Description *string         `db:"description"`
	Strategy    string          `db:"strategy"`
	StartAt     *time.Time      `db:"start_at"`
	EndAt       *time.Time      `db:"end_at"`
	Recurrence  json.RawMessage `db:"recurrence"`
	Active      bool            `db:"active"`
	CreatedBy   *int64          `db:"created_by"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// MaintenanceTarget links a window to a domain, host_group, or project.
type MaintenanceTarget struct {
	ID            int64  `db:"id"`
	MaintenanceID int64  `db:"maintenance_id"`
	TargetType    string `db:"target_type"` // "domain", "host_group", "project"
	TargetID      int64  `db:"target_id"`
}

// ─── Store ────────────────────────────────────────────────────────────────────

// ErrMaintenanceWindowNotFound is returned when a maintenance window is not found.
var ErrMaintenanceWindowNotFound = errors.New("maintenance window not found")

// MaintenanceStore handles all maintenance window persistence.
type MaintenanceStore struct {
	db *sqlx.DB
}

// NewMaintenanceStore constructs a MaintenanceStore.
func NewMaintenanceStore(db *sqlx.DB) *MaintenanceStore {
	return &MaintenanceStore{db: db}
}

// ── Window CRUD ───────────────────────────────────────────────────────────────

const insertMaintenanceWindow = `
INSERT INTO maintenance_windows
    (title, description, strategy, start_at, end_at, recurrence, active, created_by)
VALUES
    (:title, :description, :strategy, :start_at, :end_at, :recurrence, :active, :created_by)
RETURNING id, uuid, created_at, updated_at`

// Create inserts a new maintenance window and returns the populated record.
func (s *MaintenanceStore) Create(ctx context.Context, w *MaintenanceWindow) (*MaintenanceWindow, error) {
	rows, err := s.db.NamedQueryContext(ctx, insertMaintenanceWindow, w)
	if err != nil {
		return nil, fmt.Errorf("create maintenance window: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&w.ID, &w.UUID, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan maintenance window: %w", err)
		}
	}
	return w, nil
}

const updateMaintenanceWindow = `
UPDATE maintenance_windows
SET title=$2, description=$3, strategy=$4, start_at=$5, end_at=$6,
    recurrence=$7, active=$8, updated_at=NOW()
WHERE id=$1
RETURNING updated_at`

// Update saves edits to an existing window.
func (s *MaintenanceStore) Update(ctx context.Context, w *MaintenanceWindow) error {
	rec := w.Recurrence
	if len(rec) == 0 {
		rec = json.RawMessage("null")
	}
	return s.db.QueryRowContext(ctx, updateMaintenanceWindow,
		w.ID, w.Title, w.Description, w.Strategy,
		w.StartAt, w.EndAt, []byte(rec), w.Active,
	).Scan(&w.UpdatedAt)
}

const deleteMaintenanceWindow = `DELETE FROM maintenance_windows WHERE id=$1`

// Delete hard-deletes a window (cascade removes targets).
func (s *MaintenanceStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, deleteMaintenanceWindow, id)
	return err
}

const getMaintenanceWindow = `
SELECT id, uuid, title, description, strategy, start_at, end_at,
       recurrence, active, created_by, created_at, updated_at
FROM maintenance_windows WHERE id=$1`

// Get returns a single window by ID.
func (s *MaintenanceStore) Get(ctx context.Context, id int64) (*MaintenanceWindow, error) {
	var w MaintenanceWindow
	if err := s.db.GetContext(ctx, &w, getMaintenanceWindow, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMaintenanceWindowNotFound
		}
		return nil, fmt.Errorf("get maintenance window %d: %w", id, err)
	}
	return &w, nil
}

const listMaintenanceWindows = `
SELECT id, uuid, title, description, strategy, start_at, end_at,
       recurrence, active, created_by, created_at, updated_at
FROM maintenance_windows
ORDER BY created_at DESC`

// List returns all maintenance windows.
func (s *MaintenanceStore) List(ctx context.Context) ([]MaintenanceWindow, error) {
	var ws []MaintenanceWindow
	if err := s.db.SelectContext(ctx, &ws, listMaintenanceWindows); err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}
	return ws, nil
}

// GetActive returns windows that are currently active (single: now within range;
// recurring: active=true). The service layer handles recurring schedule evaluation.
const listActiveMaintenanceWindows = `
SELECT id, uuid, title, description, strategy, start_at, end_at,
       recurrence, active, created_by, created_at, updated_at
FROM maintenance_windows
WHERE active = true`

// ListActive returns all active windows (both single and recurring).
func (s *MaintenanceStore) ListActive(ctx context.Context) ([]MaintenanceWindow, error) {
	var ws []MaintenanceWindow
	if err := s.db.SelectContext(ctx, &ws, listActiveMaintenanceWindows); err != nil {
		return nil, fmt.Errorf("list active maintenance windows: %w", err)
	}
	return ws, nil
}

// ── Target CRUD ───────────────────────────────────────────────────────────────

const insertTarget = `
INSERT INTO maintenance_window_targets (maintenance_id, target_type, target_id)
VALUES ($1, $2, $3)
ON CONFLICT (maintenance_id, target_type, target_id) DO NOTHING
RETURNING id`

// AddTarget links a target to a maintenance window.
func (s *MaintenanceStore) AddTarget(ctx context.Context, maintenanceID int64, targetType string, targetID int64) (*MaintenanceTarget, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, insertTarget, maintenanceID, targetType, targetID).Scan(&id)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row — treat as success
		if errors.Is(err, sql.ErrNoRows) {
			return &MaintenanceTarget{MaintenanceID: maintenanceID, TargetType: targetType, TargetID: targetID}, nil
		}
		return nil, fmt.Errorf("add maintenance target: %w", err)
	}
	return &MaintenanceTarget{ID: id, MaintenanceID: maintenanceID, TargetType: targetType, TargetID: targetID}, nil
}

const deleteTarget = `
DELETE FROM maintenance_window_targets WHERE id=$1 AND maintenance_id=$2`

// RemoveTarget unlinks a target.
func (s *MaintenanceStore) RemoveTarget(ctx context.Context, maintenanceID, targetID int64) error {
	_, err := s.db.ExecContext(ctx, deleteTarget, targetID, maintenanceID)
	return err
}

const listTargets = `
SELECT id, maintenance_id, target_type, target_id
FROM maintenance_window_targets WHERE maintenance_id=$1
ORDER BY id`

// ListTargets returns all targets for a window.
func (s *MaintenanceStore) ListTargets(ctx context.Context, maintenanceID int64) ([]MaintenanceTarget, error) {
	var ts []MaintenanceTarget
	if err := s.db.SelectContext(ctx, &ts, listTargets, maintenanceID); err != nil {
		return nil, fmt.Errorf("list maintenance targets: %w", err)
	}
	return ts, nil
}

// ── Maintenance check helpers ─────────────────────────────────────────────────

// WindowsForDomain returns all active windows that cover the given domain
// directly (target_type='domain') OR via its project.
// host_group-level targeting is resolved in the service layer via the agent
// registry (domains don't directly reference host_groups in the schema).
const windowsForDomain = `
SELECT DISTINCT mw.id, mw.uuid, mw.title, mw.description, mw.strategy,
       mw.start_at, mw.end_at, mw.recurrence, mw.active,
       mw.created_by, mw.created_at, mw.updated_at
FROM maintenance_windows mw
JOIN maintenance_window_targets mt ON mt.maintenance_id = mw.id
WHERE mw.active = true
  AND (
      (mt.target_type = 'domain'  AND mt.target_id = $1)
   OR (mt.target_type = 'project' AND mt.target_id = (
          SELECT project_id FROM domains WHERE id = $1
      ))
  )`

// WindowsForDomain fetches active maintenance windows that directly cover the
// given domain ID or its parent project.
func (s *MaintenanceStore) WindowsForDomain(ctx context.Context, domainID int64) ([]MaintenanceWindow, error) {
	var ws []MaintenanceWindow
	if err := s.db.SelectContext(ctx, &ws, windowsForDomain, domainID); err != nil {
		return nil, fmt.Errorf("windows for domain %d: %w", domainID, err)
	}
	return ws, nil
}
