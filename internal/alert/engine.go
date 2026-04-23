// Package alert implements the alert engine: deduplication, persistence,
// severity routing, and notification dispatch.
package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/store/postgres"
)

const dedupWindow = time.Hour

// Engine is the single entry point for firing and resolving alerts.
// All alert_events writes go through Engine; nothing writes alert_events directly.
type Engine struct {
	store       *postgres.AlertStore
	asynqClient *asynq.Client
	logger      *zap.Logger
}

func NewEngine(store *postgres.AlertStore, asynqClient *asynq.Client, logger *zap.Logger) *Engine {
	return &Engine{store: store, asynqClient: asynqClient, logger: logger}
}

// Fire persists an alert event (with deduplication) and fans out TypeNotifySend tasks
// to all matching notification rules.
//
// Dedup rule (Critical Rule #8): if a non-nil DedupKey matches an active alert
// created within the last hour, the event is dropped silently.
func (e *Engine) Fire(ctx context.Context, ev *postgres.AlertEvent) error {
	// ── 1. Dedup check ────────────────────────────────────────────────────────
	if ev.DedupKey != nil && *ev.DedupKey != "" {
		exists, err := e.store.ExistsActiveDedupKey(ctx, *ev.DedupKey, dedupWindow)
		if err != nil {
			return fmt.Errorf("alert fire: dedup check: %w", err)
		}
		if exists {
			e.logger.Debug("alert suppressed by dedup",
				zap.String("dedup_key", *ev.DedupKey),
				zap.String("severity", ev.Severity),
			)
			return nil
		}
	}

	// ── 2. Persist ───────────────────────────────────────────────────────────
	if err := e.store.Insert(ctx, ev); err != nil {
		return fmt.Errorf("alert fire: insert: %w", err)
	}

	e.logger.Info("alert fired",
		zap.Int64("alert_id", ev.ID),
		zap.String("severity", ev.Severity),
		zap.String("source", ev.Source),
		zap.String("title", ev.Title),
	)

	// ── 3. Match notification rules ──────────────────────────────────────────
	rules, err := e.store.ListMatchingRules(ctx, ev.Severity, ev.TargetKind)
	if err != nil {
		// Non-fatal: alert is already persisted. Log and continue.
		e.logger.Error("alert fire: list matching rules",
			zap.Int64("alert_id", ev.ID),
			zap.Error(err),
		)
		return nil
	}

	if len(rules) == 0 {
		return nil
	}

	// ── 4. Fan out TypeNotifySend per rule ────────────────────────────────────
	subject, body := formatMessage(ev)
	enqueued := 0
	for _, rule := range rules {
		if err := e.enqueueNotify(ctx, rule, subject, body, ev.Severity); err != nil {
			e.logger.Warn("alert fire: enqueue notify",
				zap.Int64("alert_id", ev.ID),
				zap.Int64("rule_id", rule.ID),
				zap.String("channel", rule.Channel),
				zap.Error(err),
			)
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		if err := e.store.MarkNotified(ctx, ev.ID); err != nil {
			e.logger.Warn("alert fire: mark notified", zap.Int64("alert_id", ev.ID), zap.Error(err))
		}
	}

	return nil
}

// Resolve auto-clears all active alerts matching the given dedup key.
// Called when a probe or drift check recovers.
func (e *Engine) Resolve(ctx context.Context, dedupKey string) error {
	if err := e.store.ResolveByDedupKey(ctx, dedupKey); err != nil {
		return fmt.Errorf("alert resolve: %w", err)
	}
	e.logger.Info("alert auto-resolved", zap.String("dedup_key", dedupKey))
	return nil
}

// FirePayload is a convenience constructor used by callers that want to
// submit an alert as an async asynq task (TypeAlertFire) rather than calling
// Fire directly. This keeps probe handlers decoupled from the alert store.
func EnqueueFire(ctx context.Context, client *asynq.Client, p tasks.AlertFirePayload) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("enqueue alert fire: marshal: %w", err)
	}
	task := asynq.NewTask(tasks.TypeAlertFire, raw,
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
		asynq.Queue("default"),
	)
	if _, err := client.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue alert fire: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (e *Engine) enqueueNotify(ctx context.Context, rule postgres.NotificationRule, subject, body, severity string) error {
	payload := tasks.NotifySendPayload{
		Channel:  rule.Channel,
		Config:   rule.Config, // raw JSONB from DB — self-contained credentials
		Subject:  subject,
		Body:     body,
		Severity: severityToLevel(severity),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal notify payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeNotifySend, raw,
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
		asynq.Queue("default"),
	)
	if _, err := e.asynqClient.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue notify: %w", err)
	}
	return nil
}

func severityToLevel(s string) string {
	switch s {
	case "P1":
		return "critical"
	case "P2":
		return "error"
	case "P3":
		return "warning"
	default:
		return "info"
	}
}
