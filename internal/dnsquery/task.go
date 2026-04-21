package dnsquery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/pkg/notify"
	"domain-platform/store/postgres"
)

// ── HandleDriftCheckAll ───────────────────────────────────────────────────────

// HandleDriftCheckAll is the asynq handler for TypeDNSDriftCheckAll.
// It scans all active domains that have a dns_provider_id configured and
// enqueues a TypeDNSDriftCheck task for each one.
type HandleDriftCheckAll struct {
	domainStore *postgres.DomainStore
	asynqClient *asynq.Client
	logger      *zap.Logger
}

func NewHandleDriftCheckAll(
	domainStore *postgres.DomainStore,
	asynqClient *asynq.Client,
	logger *zap.Logger,
) *HandleDriftCheckAll {
	return &HandleDriftCheckAll{
		domainStore: domainStore,
		asynqClient: asynqClient,
		logger:      logger,
	}
}

func (h *HandleDriftCheckAll) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	domains, err := h.domainStore.ListActiveWithDNSProvider(ctx)
	if err != nil {
		return fmt.Errorf("list active domains with dns provider: %w", err)
	}

	if len(domains) == 0 {
		h.logger.Info("dns:drift_check_all — no domains with DNS provider configured")
		return nil
	}

	var enqueued, skipped int
	for _, d := range domains {
		if d.DNSProviderID == nil {
			continue // guard: ListActiveWithDNSProvider should always set this
		}
		payload, err := json.Marshal(tasks.DNSDriftCheckPayload{
			DomainID:      d.ID,
			DomainUUID:    d.UUID,
			FQDN:          d.FQDN,
			DNSProviderID: *d.DNSProviderID,
		})
		if err != nil {
			h.logger.Error("marshal dns drift check payload",
				zap.Int64("domain_id", d.ID),
				zap.Error(err),
			)
			skipped++
			continue
		}

		task := asynq.NewTask(tasks.TypeDNSDriftCheck, payload,
			asynq.Queue("default"),
			asynq.MaxRetry(2),
			asynq.Timeout(60*time.Second),
		)
		if _, err := h.asynqClient.EnqueueContext(ctx, task); err != nil {
			h.logger.Error("enqueue dns:drift_check",
				zap.Int64("domain_id", d.ID),
				zap.String("fqdn", d.FQDN),
				zap.Error(err),
			)
			skipped++
			continue
		}
		enqueued++
	}

	h.logger.Info("dns:drift_check_all complete",
		zap.Int("total", len(domains)),
		zap.Int("enqueued", enqueued),
		zap.Int("skipped", skipped),
	)
	return nil
}

// ── HandleDriftCheck ─────────────────────────────────────────────────────────

// HandleDriftCheck is the asynq handler for TypeDNSDriftCheck.
// It runs a drift check for a single domain and sends an alert notification
// if drift is detected. Alert deduplication is enforced via Redis:
// at most one alert per domain per hour (Critical Rule #8).
type HandleDriftCheck struct {
	dnsSvc          *Service
	domainStore     *postgres.DomainStore
	dnsProviderStore *postgres.DNSProviderStore
	notifier        notify.Notifier
	rdb             *redis.Client
	logger          *zap.Logger
}

func NewHandleDriftCheck(
	dnsSvc *Service,
	domainStore *postgres.DomainStore,
	dnsProviderStore *postgres.DNSProviderStore,
	notifier notify.Notifier,
	rdb *redis.Client,
	logger *zap.Logger,
) *HandleDriftCheck {
	return &HandleDriftCheck{
		dnsSvc:           dnsSvc,
		domainStore:      domainStore,
		dnsProviderStore: dnsProviderStore,
		notifier:         notifier,
		rdb:              rdb,
		logger:           logger,
	}
}

func (h *HandleDriftCheck) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.DNSDriftCheckPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal dns drift check payload: %w", err)
	}

	// Fetch domain
	domain, err := h.domainStore.GetByID(ctx, p.DomainID)
	if err != nil {
		return fmt.Errorf("get domain %d: %w", p.DomainID, err)
	}

	// Fetch DNS provider
	provider, err := h.dnsProviderStore.GetByID(ctx, p.DNSProviderID)
	if err != nil {
		return fmt.Errorf("get dns provider %d: %w", p.DNSProviderID, err)
	}

	// Run drift check
	result := h.dnsSvc.CheckDrift(ctx, domain, provider)

	// Only alert when drift is detected
	if result.Status != DriftDetected {
		h.logger.Info("dns:drift_check — no drift",
			zap.String("fqdn", p.FQDN),
			zap.String("status", string(result.Status)),
		)
		return nil
	}

	// Stamp last_drift_at on the domain (best-effort; don't fail the task on DB error)
	if err := h.domainStore.UpdateLastDriftAt(ctx, p.DomainID); err != nil {
		h.logger.Warn("dns:drift_check — failed to stamp last_drift_at",
			zap.Int64("domain_id", p.DomainID),
			zap.Error(err),
		)
	}

	h.logger.Warn("dns:drift_check — drift detected",
		zap.String("fqdn", p.FQDN),
		zap.Int("drift_count", result.DriftCount),
		zap.Int("missing_count", result.MissingCount),
		zap.Int("extra_count", result.ExtraCount),
	)

	// Deduplication: at most 1 alert per domain per hour (Critical Rule #8)
	if !h.trySetAlerted(ctx, p.DomainID) {
		h.logger.Info("dns:drift_check — alert suppressed (already sent within 1h)",
			zap.String("fqdn", p.FQDN),
		)
		return nil
	}

	// Send alert
	subject, body := formatDriftAlert(result)
	if err := h.notifier.Send(ctx, notify.Message{
		Subject:  subject,
		Body:     body,
		Severity: "warning",
	}); err != nil {
		h.logger.Warn("dns drift alert send failed",
			zap.String("fqdn", p.FQDN),
			zap.Error(err),
		)
		// Clear the dedup key so the next run can retry the notification
		h.clearAlerted(ctx, p.DomainID)
	}

	return nil
}

// trySetAlerted tries to set the dedup key in Redis.
// Returns true if the key was set (alert should be sent), false if suppressed.
// Key expires after 1 hour — enforces Critical Rule #8.
func (h *HandleDriftCheck) trySetAlerted(ctx context.Context, domainID int64) bool {
	key := fmt.Sprintf("dns:drift:alerted:%d", domainID)
	// SET NX EX — set only if not exists, expire in 1 hour
	ok, err := h.rdb.SetNX(ctx, key, 1, time.Hour).Result()
	if err != nil {
		// On Redis error, allow the alert to go through (fail open)
		h.logger.Warn("redis SetNX failed for drift dedup, allowing alert",
			zap.Int64("domain_id", domainID),
			zap.Error(err),
		)
		return true
	}
	return ok
}

// clearAlerted removes the dedup key so the next drift check run can retry.
func (h *HandleDriftCheck) clearAlerted(ctx context.Context, domainID int64) {
	key := fmt.Sprintf("dns:drift:alerted:%d", domainID)
	if err := h.rdb.Del(ctx, key).Err(); err != nil {
		h.logger.Warn("failed to clear drift alert dedup key",
			zap.Int64("domain_id", domainID),
			zap.Error(err),
		)
	}
}

// ── Message formatting ────────────────────────────────────────────────────────

func formatDriftAlert(r *DriftResult) (subject, body string) {
	subject = fmt.Sprintf("⚠️ DNS Drift 告警 — %s (%d 項差異)", r.FQDN, r.DriftCount+r.MissingCount+r.ExtraCount)

	var b strings.Builder
	fmt.Fprintf(&b, "域名 %s 的 DNS 記錄與 %s 供應商配置出現差異。\n\n", r.FQDN, r.ProviderLabel)

	if r.MissingCount > 0 {
		fmt.Fprintf(&b, "**缺少記錄**（供應商有，DNS 查詢無）: %d 筆\n", r.MissingCount)
	}
	if r.DriftCount > 0 {
		fmt.Fprintf(&b, "**數值不一致**: %d 筆\n", r.DriftCount)
	}
	if r.ExtraCount > 0 {
		fmt.Fprintf(&b, "**多餘記錄**（DNS 查詢有，供應商無）: %d 筆\n", r.ExtraCount)
	}

	// List drifted records (up to 10 to avoid message spam)
	b.WriteString("\n差異明細:\n")
	shown := 0
	for _, rec := range r.Records {
		if rec.Match {
			continue
		}
		if shown >= 10 {
			remaining := countMismatches(r.Records) - 10
			fmt.Fprintf(&b, "… 還有 %d 筆差異\n", remaining)
			break
		}
		switch {
		case rec.Expected != "" && rec.Actual == "":
			fmt.Fprintf(&b, "• [缺失] %s %s → 供應商值: %s\n", rec.Type, rec.Name, rec.Expected)
		case rec.Expected == "" && rec.Actual != "":
			fmt.Fprintf(&b, "• [多餘] %s %s → 實際值: %s\n", rec.Type, rec.Name, rec.Actual)
		default:
			fmt.Fprintf(&b, "• [不符] %s %s → 供應商: %s | 實際: %s\n", rec.Type, rec.Name, rec.Expected, rec.Actual)
		}
		shown++
	}

	fmt.Fprintf(&b, "\n查詢時間: %s", r.QueriedAt)
	return subject, b.String()
}

func countMismatches(records []DriftRecord) int {
	n := 0
	for _, r := range records {
		if !r.Match {
			n++
		}
	}
	return n
}
