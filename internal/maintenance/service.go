// Package maintenance manages planned downtime windows.
//
// A maintenance window suppresses probe alerts and causes probes to record
// status="maintenance" instead of "down". The service exposes IsInMaintenance
// which the probe engine and alert engine call before processing results.
package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// ─── Recurrence payloads ──────────────────────────────────────────────────────

// WeeklyRecurrence is stored in maintenance_windows.recurrence for
// strategy="recurring_weekly".
//
//	{"weekdays":[1,5],"start_time":"02:00","duration_minutes":120,"timezone":"UTC"}
type WeeklyRecurrence struct {
	Weekdays        []time.Weekday `json:"weekdays"`         // 0=Sun … 6=Sat
	StartTime       string         `json:"start_time"`       // "HH:MM" 24-hour
	DurationMinutes int            `json:"duration_minutes"` // window length
	Timezone        string         `json:"timezone"`         // IANA tz name
}

// CronRecurrence is stored for strategy="cron".
//
//	{"expression":"0 2 * * 1","duration_minutes":120,"timezone":"UTC"}
type CronRecurrence struct {
	Expression      string `json:"expression"`       // standard 5-field cron
	DurationMinutes int    `json:"duration_minutes"`
	Timezone        string `json:"timezone"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

// Service provides maintenance window business logic.
type Service struct {
	store  *postgres.MaintenanceStore
	logger *zap.Logger
}

// NewService constructs a maintenance Service.
func NewService(store *postgres.MaintenanceStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// ── Window CRUD ───────────────────────────────────────────────────────────────

// CreateWindowInput carries the fields needed to create a new window.
type CreateWindowInput struct {
	Title       string
	Description *string
	Strategy    string          // "single" | "recurring_weekly" | "recurring_monthly" | "cron"
	StartAt     *time.Time      // for single
	EndAt       *time.Time      // for single
	Recurrence  json.RawMessage // for recurring/cron
	Active      bool
	CreatedBy   *int64
}

// Create validates and persists a new maintenance window.
func (s *Service) Create(ctx context.Context, in CreateWindowInput) (*postgres.MaintenanceWindow, error) {
	if err := validateStrategy(in.Strategy, in.StartAt, in.EndAt, in.Recurrence); err != nil {
		return nil, err
	}
	w := &postgres.MaintenanceWindow{
		Title:       in.Title,
		Description: in.Description,
		Strategy:    in.Strategy,
		StartAt:     in.StartAt,
		EndAt:       in.EndAt,
		Recurrence:  in.Recurrence,
		Active:      in.Active,
		CreatedBy:   in.CreatedBy,
	}
	return s.store.Create(ctx, w)
}

// UpdateWindowInput carries updatable fields.
type UpdateWindowInput struct {
	Title       string
	Description *string
	Strategy    string
	StartAt     *time.Time
	EndAt       *time.Time
	Recurrence  json.RawMessage
	Active      bool
}

// Update validates and saves changes to an existing window.
func (s *Service) Update(ctx context.Context, id int64, in UpdateWindowInput) (*postgres.MaintenanceWindow, error) {
	w, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validateStrategy(in.Strategy, in.StartAt, in.EndAt, in.Recurrence); err != nil {
		return nil, err
	}
	w.Title = in.Title
	w.Description = in.Description
	w.Strategy = in.Strategy
	w.StartAt = in.StartAt
	w.EndAt = in.EndAt
	w.Recurrence = in.Recurrence
	w.Active = in.Active
	if err := s.store.Update(ctx, w); err != nil {
		return nil, fmt.Errorf("update maintenance window %d: %w", id, err)
	}
	return w, nil
}

// Delete removes a maintenance window.
func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id)
}

// Get returns a single window with its targets.
func (s *Service) Get(ctx context.Context, id int64) (*postgres.MaintenanceWindow, []postgres.MaintenanceTarget, error) {
	w, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	ts, err := s.store.ListTargets(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return w, ts, nil
}

// List returns all windows (no pagination — ops workload is small).
func (s *Service) List(ctx context.Context) ([]postgres.MaintenanceWindow, error) {
	return s.store.List(ctx)
}

// AddTarget links a target (domain/host_group/project) to a window.
func (s *Service) AddTarget(ctx context.Context, maintenanceID int64, targetType string, targetID int64) (*postgres.MaintenanceTarget, error) {
	switch targetType {
	case "domain", "host_group", "project":
	default:
		return nil, fmt.Errorf("invalid target_type %q: must be domain, host_group, or project", targetType)
	}
	return s.store.AddTarget(ctx, maintenanceID, targetType, targetID)
}

// RemoveTarget unlinks a target.
func (s *Service) RemoveTarget(ctx context.Context, maintenanceID, targetID int64) error {
	return s.store.RemoveTarget(ctx, maintenanceID, targetID)
}

// ── IsInMaintenance ───────────────────────────────────────────────────────────

// IsInMaintenance returns (true, window) if the given domain is covered by
// an active maintenance window at the given time t.
//
// Check order:
//  1. Fetch windows covering the domain (direct + project-level) from DB.
//  2. For each window, evaluate the schedule against t.
func (s *Service) IsInMaintenance(ctx context.Context, domainID int64, t time.Time) (bool, *postgres.MaintenanceWindow, error) {
	windows, err := s.store.WindowsForDomain(ctx, domainID)
	if err != nil {
		return false, nil, fmt.Errorf("IsInMaintenance: %w", err)
	}
	for i := range windows {
		w := &windows[i]
		active, err := windowCoversTime(w, t)
		if err != nil {
			s.logger.Warn("maintenance schedule eval error",
				zap.Int64("window_id", w.ID), zap.Error(err))
			continue
		}
		if active {
			return true, w, nil
		}
	}
	return false, nil, nil
}

// ─── Schedule evaluation ──────────────────────────────────────────────────────

// windowCoversTime returns true if the window's schedule is active at time t.
func windowCoversTime(w *postgres.MaintenanceWindow, t time.Time) (bool, error) {
	switch w.Strategy {
	case "single":
		return coversSingle(w, t), nil
	case "recurring_weekly":
		return coversWeekly(w, t)
	case "recurring_monthly":
		return coversMonthly(w, t)
	case "cron":
		return coversCron(w, t)
	default:
		return false, fmt.Errorf("unknown strategy %q", w.Strategy)
	}
}

// coversSingle checks start_at <= t <= end_at.
func coversSingle(w *postgres.MaintenanceWindow, t time.Time) bool {
	if w.StartAt == nil || w.EndAt == nil {
		return false
	}
	return !t.Before(*w.StartAt) && !t.After(*w.EndAt)
}

// coversWeekly checks recurring_weekly recurrence.
func coversWeekly(w *postgres.MaintenanceWindow, t time.Time) (bool, error) {
	if len(w.Recurrence) == 0 {
		return false, errors.New("recurring_weekly window missing recurrence config")
	}
	var r WeeklyRecurrence
	if err := json.Unmarshal(w.Recurrence, &r); err != nil {
		return false, fmt.Errorf("parse weekly recurrence: %w", err)
	}
	loc, err := loadLocation(r.Timezone)
	if err != nil {
		return false, err
	}
	tLocal := t.In(loc)

	// Check if today's weekday is in the schedule.
	wdMatch := false
	for _, wd := range r.Weekdays {
		if tLocal.Weekday() == wd {
			wdMatch = true
			break
		}
	}
	if !wdMatch {
		return false, nil
	}

	// Parse "HH:MM" start time.
	windowStart, err := parseHHMM(r.StartTime, tLocal)
	if err != nil {
		return false, err
	}
	windowEnd := windowStart.Add(time.Duration(r.DurationMinutes) * time.Minute)
	return !tLocal.Before(windowStart) && !tLocal.After(windowEnd), nil
}

// coversMonthly checks recurring_monthly recurrence.
// Recurrence payload: {"day_of_month":15,"start_time":"02:00","duration_minutes":120,"timezone":"UTC"}
func coversMonthly(w *postgres.MaintenanceWindow, t time.Time) (bool, error) {
	if len(w.Recurrence) == 0 {
		return false, errors.New("recurring_monthly window missing recurrence config")
	}
	var r struct {
		DayOfMonth      int    `json:"day_of_month"`
		StartTime       string `json:"start_time"`
		DurationMinutes int    `json:"duration_minutes"`
		Timezone        string `json:"timezone"`
	}
	if err := json.Unmarshal(w.Recurrence, &r); err != nil {
		return false, fmt.Errorf("parse monthly recurrence: %w", err)
	}
	loc, err := loadLocation(r.Timezone)
	if err != nil {
		return false, err
	}
	tLocal := t.In(loc)
	if tLocal.Day() != r.DayOfMonth {
		return false, nil
	}
	windowStart, err := parseHHMM(r.StartTime, tLocal)
	if err != nil {
		return false, err
	}
	windowEnd := windowStart.Add(time.Duration(r.DurationMinutes) * time.Minute)
	return !tLocal.Before(windowStart) && !tLocal.After(windowEnd), nil
}

// coversCron evaluates a 5-field cron expression.
// Rather than pulling in a cron library we implement a minimal evaluator
// sufficient for the patterns used in practice (field = "*", "N", or "N,M").
func coversCron(w *postgres.MaintenanceWindow, t time.Time) (bool, error) {
	if len(w.Recurrence) == 0 {
		return false, errors.New("cron window missing recurrence config")
	}
	var r CronRecurrence
	if err := json.Unmarshal(w.Recurrence, &r); err != nil {
		return false, fmt.Errorf("parse cron recurrence: %w", err)
	}
	loc, err := loadLocation(r.Timezone)
	if err != nil {
		return false, err
	}
	tLocal := t.In(loc)

	// Find the most recent fire time at or before tLocal.
	fire, err := prevCronFire(r.Expression, tLocal)
	if err != nil {
		return false, err
	}
	windowEnd := fire.Add(time.Duration(r.DurationMinutes) * time.Minute)
	return !tLocal.Before(fire) && !tLocal.After(windowEnd), nil
}

// ─── Cron helpers ─────────────────────────────────────────────────────────────

// prevCronFire returns the latest fire time at or before t for the given
// standard 5-field cron expression "min hour dom month dow".
// Supports: "*", single integers, and comma-separated lists.
func prevCronFire(expr string, t time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}
	// Search backward minute-by-minute for up to 1 week.
	candidate := t.Truncate(time.Minute)
	for i := 0; i < 60*24*7; i++ {
		if matchCronField(fields[4], int(candidate.Weekday())) &&
			matchCronField(fields[3], int(candidate.Month())) &&
			matchCronField(fields[2], candidate.Day()) &&
			matchCronField(fields[1], candidate.Hour()) &&
			matchCronField(fields[0], candidate.Minute()) {
			return candidate, nil
		}
		candidate = candidate.Add(-time.Minute)
	}
	return time.Time{}, errors.New("no cron fire time found within 1 week")
}

// matchCronField checks whether value matches a cron field token.
func matchCronField(field string, value int) bool {
	if field == "*" {
		return true
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		var n int
		if _, err := fmt.Sscanf(part, "%d", &n); err == nil && n == value {
			return true
		}
	}
	return false
}

// ─── Utility ──────────────────────────────────────────────────────────────────

func loadLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}

// parseHHMM parses "HH:MM" and returns a time on the same calendar day as ref.
func parseHHMM(hhmm string, ref time.Time) (time.Time, error) {
	parts := strings.SplitN(hhmm, ":", 2)
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid time format %q, expected HH:MM", hhmm)
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil || h < 0 || h > 23 {
		return time.Time{}, fmt.Errorf("invalid hour in %q", hhmm)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil || m < 0 || m > 59 {
		return time.Time{}, fmt.Errorf("invalid minute in %q", hhmm)
	}
	return time.Date(ref.Year(), ref.Month(), ref.Day(), h, m, 0, 0, ref.Location()), nil
}

// validateStrategy checks that the correct fields are set for the chosen strategy.
func validateStrategy(strategy string, startAt, endAt *time.Time, recurrence json.RawMessage) error {
	switch strategy {
	case "single":
		if startAt == nil || endAt == nil {
			return errors.New("single maintenance window requires start_at and end_at")
		}
		if !endAt.After(*startAt) {
			return errors.New("end_at must be after start_at")
		}
	case "recurring_weekly", "recurring_monthly", "cron":
		if len(recurrence) == 0 || string(recurrence) == "null" {
			return fmt.Errorf("strategy %q requires recurrence config", strategy)
		}
	default:
		return fmt.Errorf("invalid strategy %q", strategy)
	}
	return nil
}
