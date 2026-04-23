package alert

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/pkg/notify"
	"domain-platform/store/postgres"
)

// ── HandleAlertFire ───────────────────────────────────────────────────────────

// HandleAlertFire processes TypeAlertFire tasks.
// It reconstructs an AlertEvent from the payload and delegates to Engine.Fire.
type HandleAlertFire struct {
	engine *Engine
	logger *zap.Logger
}

func NewHandleAlertFire(engine *Engine, logger *zap.Logger) *HandleAlertFire {
	return &HandleAlertFire{engine: engine, logger: logger}
}

func (h *HandleAlertFire) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.AlertFirePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("alert fire: unmarshal payload: %w", err)
	}

	ev := &postgres.AlertEvent{
		Severity:   p.Severity,
		Source:     p.Source,
		TargetKind: p.TargetKind,
		TargetID:   p.TargetID,
		Title:      p.Title,
	}
	if p.DedupKey != "" {
		dk := p.DedupKey
		ev.DedupKey = &dk
	}
	if p.Detail != "" {
		ev.Detail = json.RawMessage(p.Detail)
	}

	if err := h.engine.Fire(ctx, ev); err != nil {
		h.logger.Error("alert fire task failed",
			zap.String("severity", p.Severity),
			zap.String("title", p.Title),
			zap.Error(err),
		)
		return err
	}
	return nil
}

// ── HandleNotifySend ─────────────────────────────────────────────────────────

// HandleNotifySend processes TypeNotifySend tasks.
// It reads the channel + config from the payload, builds the appropriate
// notify.Notifier, and delivers the message.
type HandleNotifySend struct {
	logger *zap.Logger
}

func NewHandleNotifySend(logger *zap.Logger) *HandleNotifySend {
	return &HandleNotifySend{logger: logger}
}

func (h *HandleNotifySend) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.NotifySendPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("notify send: unmarshal payload: %w", err)
	}

	notifier, err := buildNotifier(p.Channel, p.Config)
	if err != nil {
		return fmt.Errorf("notify send: build notifier for channel %q: %w", p.Channel, err)
	}

	msg := notify.Message{
		Subject:  p.Subject,
		Body:     p.Body,
		Severity: p.Severity,
	}

	if err := notifier.Send(ctx, msg); err != nil {
		h.logger.Error("notify send failed",
			zap.String("channel", p.Channel),
			zap.String("subject", p.Subject),
			zap.Error(err),
		)
		return fmt.Errorf("notify send: %w", err)
	}

	h.logger.Info("notification sent",
		zap.String("channel", p.Channel),
		zap.String("subject", p.Subject),
	)
	return nil
}

// ── channel config types ──────────────────────────────────────────────────────

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type webhookConfig struct {
	URL string `json:"url"`
}

func buildNotifier(channel string, config []byte) (notify.Notifier, error) {
	switch channel {
	case "telegram":
		var cfg telegramConfig
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, fmt.Errorf("parse telegram config: %w", err)
		}
		if cfg.BotToken == "" || cfg.ChatID == "" {
			return nil, fmt.Errorf("telegram config missing bot_token or chat_id")
		}
		return notify.NewTelegram(cfg.BotToken, cfg.ChatID), nil

	case "webhook":
		var cfg webhookConfig
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, fmt.Errorf("parse webhook config: %w", err)
		}
		if cfg.URL == "" {
			return nil, fmt.Errorf("webhook config missing url")
		}
		return notify.NewWebhook(cfg.URL), nil

	default:
		return nil, fmt.Errorf("unsupported channel %q", channel)
	}
}
