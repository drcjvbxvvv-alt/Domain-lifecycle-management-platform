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

var ErrAlertNotFound        = errors.New("alert not found")
var ErrNotificationRuleNotFound = errors.New("notification rule not found")

// ── Models ────────────────────────────────────────────────────────────────────

// AlertEvent maps to the alert_events table.
type AlertEvent struct {
	ID              int64           `db:"id"`
	UUID            string          `db:"uuid"`
	Severity        string          `db:"severity"`        // P1 | P2 | P3 | INFO
	Source          string          `db:"source"`          // probe | drift | expiry | agent | manual | system
	TargetKind      string          `db:"target_kind"`
	TargetID        *int64          `db:"target_id"`
	Title           string          `db:"title"`
	Detail          json.RawMessage `db:"detail"`
	DedupKey        *string         `db:"dedup_key"`
	NotifiedAt      *time.Time      `db:"notified_at"`
	ResolvedAt      *time.Time      `db:"resolved_at"`
	AcknowledgedAt  *time.Time      `db:"acknowledged_at"`
	AcknowledgedBy  *int64          `db:"acknowledged_by"`
	CreatedAt       time.Time       `db:"created_at"`
}

// NotificationRule maps to the notification_rules table.
type NotificationRule struct {
	ID             int64           `db:"id"`
	UUID           string          `db:"uuid"`
	Name           string          `db:"name"`
	ProjectID      *int64          `db:"project_id"`
	SeverityFilter *string         `db:"severity_filter"` // nil = match all severities
	TargetKind     *string         `db:"target_kind"`     // nil = match all target kinds
	Channel        string          `db:"channel"`         // telegram | webhook | slack
	Config         json.RawMessage `db:"config"`
	Enabled        bool            `db:"enabled"`
	CreatedAt      time.Time       `db:"created_at"`
}

// ── Alert Store ───────────────────────────────────────────────────────────────

type AlertStore struct {
	db *sqlx.DB
}

func NewAlertStore(db *sqlx.DB) *AlertStore {
	return &AlertStore{db: db}
}

// ── AlertEvent writes ─────────────────────────────────────────────────────────

// Insert saves a new alert_events row.
func (s *AlertStore) Insert(ctx context.Context, ev *AlertEvent) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO alert_events
		   (severity, source, target_kind, target_id, title, detail, dedup_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id, uuid, created_at`,
		ev.Severity, ev.Source, ev.TargetKind, ev.TargetID,
		ev.Title, nullJSON(ev.Detail), ev.DedupKey,
	).Scan(&ev.ID, &ev.UUID, &ev.CreatedAt)
}

// ExistsActiveDedupKey returns true if there is an unresolved alert with the
// given dedup_key created within the last dedupWindow.
func (s *AlertStore) ExistsActiveDedupKey(ctx context.Context, dedupKey string, window time.Duration) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM alert_events
		 WHERE dedup_key = $1
		   AND resolved_at IS NULL
		   AND created_at > NOW() - $2::INTERVAL`,
		dedupKey, fmt.Sprintf("%d seconds", int(window.Seconds())),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("alert dedup check: %w", err)
	}
	return count > 0, nil
}

// MarkNotified sets notified_at = NOW() for the alert.
func (s *AlertStore) MarkNotified(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE alert_events SET notified_at = NOW() WHERE id = $1`, id)
	return err
}

// Resolve marks the alert as resolved.
func (s *AlertStore) Resolve(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE alert_events SET resolved_at = NOW()
		 WHERE id = $1 AND resolved_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("resolve alert %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlertNotFound
	}
	return nil
}

// ResolveByDedupKey resolves all active alerts matching a dedup key.
// Used when a probe recovers to auto-clear related alerts.
func (s *AlertStore) ResolveByDedupKey(ctx context.Context, dedupKey string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE alert_events SET resolved_at = NOW()
		 WHERE dedup_key = $1 AND resolved_at IS NULL`, dedupKey)
	if err != nil {
		return fmt.Errorf("resolve alerts by dedup_key %q: %w", dedupKey, err)
	}
	return nil
}

// Acknowledge marks the alert as acknowledged by the given user.
func (s *AlertStore) Acknowledge(ctx context.Context, id, userID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE alert_events
		 SET acknowledged_at = NOW(), acknowledged_by = $2
		 WHERE id = $1 AND acknowledged_at IS NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("acknowledge alert %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlertNotFound
	}
	return nil
}

// ── AlertEvent reads ──────────────────────────────────────────────────────────

// GetByID returns a single alert event.
func (s *AlertStore) GetByID(ctx context.Context, id int64) (*AlertEvent, error) {
	var ev AlertEvent
	err := s.db.GetContext(ctx, &ev,
		`SELECT id, uuid, severity, source, target_kind, target_id, title, detail,
		        dedup_key, notified_at, resolved_at, acknowledged_at, acknowledged_by, created_at
		 FROM alert_events WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAlertNotFound
		}
		return nil, fmt.Errorf("get alert %d: %w", id, err)
	}
	return &ev, nil
}

// ListFilter configures alert listing.
type AlertListFilter struct {
	Severity   string // "" = all
	Source     string // "" = all
	TargetKind string // "" = all
	TargetID   *int64
	Unresolved bool   // only unresolved
	Limit      int    // default 100
	Offset     int
}

// List returns alert events matching the filter, ordered by created_at DESC.
func (s *AlertStore) List(ctx context.Context, f AlertListFilter) ([]AlertEvent, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	var out []AlertEvent
	err := s.db.SelectContext(ctx, &out,
		`SELECT id, uuid, severity, source, target_kind, target_id, title, detail,
		        dedup_key, notified_at, resolved_at, acknowledged_at, acknowledged_by, created_at
		 FROM alert_events
		 WHERE ($1 = '' OR severity = $1)
		   AND ($2 = '' OR source = $2)
		   AND ($3 = '' OR target_kind = $3)
		   AND ($4::BIGINT IS NULL OR target_id = $4)
		   AND ($5 = false OR resolved_at IS NULL)
		 ORDER BY created_at DESC
		 LIMIT $6 OFFSET $7`,
		f.Severity, f.Source, f.TargetKind, f.TargetID, f.Unresolved, f.Limit, f.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	return out, nil
}

// CountUnresolved returns the number of unresolved alerts per severity.
func (s *AlertStore) CountUnresolved(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT severity, COUNT(*) FROM alert_events
		 WHERE resolved_at IS NULL
		 GROUP BY severity`)
	if err != nil {
		return nil, fmt.Errorf("count unresolved alerts: %w", err)
	}
	defer rows.Close()
	out := map[string]int{"P1": 0, "P2": 0, "P3": 0, "INFO": 0}
	for rows.Next() {
		var sev string
		var cnt int
		if err := rows.Scan(&sev, &cnt); err != nil {
			return nil, err
		}
		out[sev] = cnt
	}
	return out, rows.Err()
}

// ── NotificationRule ──────────────────────────────────────────────────────────

// ListMatchingRules returns enabled notification_rules that match the given
// severity and target_kind. Rules with nil filter fields match everything.
func (s *AlertStore) ListMatchingRules(ctx context.Context, severity, targetKind string) ([]NotificationRule, error) {
	var out []NotificationRule
	err := s.db.SelectContext(ctx, &out,
		`SELECT id, uuid, name, project_id, severity_filter, target_kind, channel, config, enabled, created_at
		 FROM notification_rules
		 WHERE enabled = true
		   AND (severity_filter IS NULL OR severity_filter = $1)
		   AND (target_kind IS NULL OR target_kind = $2)`,
		severity, targetKind)
	if err != nil {
		return nil, fmt.Errorf("list matching notification rules: %w", err)
	}
	return out, nil
}

// ListAllRules returns all notification rules (for admin management).
func (s *AlertStore) ListAllRules(ctx context.Context) ([]NotificationRule, error) {
	var out []NotificationRule
	err := s.db.SelectContext(ctx, &out,
		`SELECT id, uuid, name, project_id, severity_filter, target_kind, channel, config, enabled, created_at
		 FROM notification_rules ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list notification rules: %w", err)
	}
	return out, nil
}

// CreateRule inserts a new notification rule.
func (s *AlertStore) CreateRule(ctx context.Context, r *NotificationRule) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO notification_rules
		   (name, project_id, severity_filter, target_kind, channel, config, enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id, uuid, created_at`,
		r.Name, r.ProjectID, r.SeverityFilter, r.TargetKind,
		r.Channel, nullJSON(r.Config), r.Enabled,
	).Scan(&r.ID, &r.UUID, &r.CreatedAt)
}

// UpdateRule updates mutable fields of a notification rule.
func (s *AlertStore) UpdateRule(ctx context.Context, r *NotificationRule) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notification_rules SET
		   name=$1, severity_filter=$2, target_kind=$3,
		   channel=$4, config=$5, enabled=$6
		 WHERE id=$7`,
		r.Name, r.SeverityFilter, r.TargetKind,
		r.Channel, nullJSON(r.Config), r.Enabled, r.ID)
	if err != nil {
		return fmt.Errorf("update notification rule %d: %w", r.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotificationRuleNotFound
	}
	return nil
}

// DeleteRule removes a notification rule.
func (s *AlertStore) DeleteRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_rules WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete notification rule %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotificationRuleNotFound
	}
	return nil
}

// GetRule returns a single notification rule.
func (s *AlertStore) GetRule(ctx context.Context, id int64) (*NotificationRule, error) {
	var r NotificationRule
	err := s.db.GetContext(ctx, &r,
		`SELECT id, uuid, name, project_id, severity_filter, target_kind, channel, config, enabled, created_at
		 FROM notification_rules WHERE id=$1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotificationRuleNotFound
		}
		return nil, fmt.Errorf("get notification rule %d: %w", id, err)
	}
	return &r, nil
}
