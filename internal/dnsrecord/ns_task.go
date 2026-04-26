package dnsrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/pkg/notify"
	"domain-platform/store/postgres"
)

// ── HandleNSCheckAll ──────────────────────────────────────────────────────────

// HandleNSCheckAll is the asynq handler for TypeNSCheckAll.
// It scans all domains with ns_delegation_status IN ('pending','mismatch')
// and enqueues a TypeNSCheck task for each one.
type HandleNSCheckAll struct {
	domainStore     *postgres.DomainStore
	dnsProviders    *postgres.DNSProviderStore
	bindingSvc      *Service
	asynqClient     *asynq.Client
	logger          *zap.Logger
}

func NewHandleNSCheckAll(
	domainStore *postgres.DomainStore,
	dnsProviders *postgres.DNSProviderStore,
	bindingSvc *Service,
	asynqClient *asynq.Client,
	logger *zap.Logger,
) *HandleNSCheckAll {
	return &HandleNSCheckAll{
		domainStore:  domainStore,
		dnsProviders: dnsProviders,
		bindingSvc:   bindingSvc,
		asynqClient:  asynqClient,
		logger:       logger,
	}
}

func (h *HandleNSCheckAll) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	domains, err := h.domainStore.ListWithPendingNS(ctx)
	if err != nil {
		return fmt.Errorf("list domains with pending ns: %w", err)
	}

	if len(domains) == 0 {
		h.logger.Info("domain:ns_check_all — no domains with pending/mismatch NS")
		return nil
	}

	var enqueued, skipped int
	for _, d := range domains {
		if d.DNSProviderID == nil {
			skipped++
			continue
		}

		// Resolve expected NS from provider.
		var expectedNS []string
		provider, err := h.dnsProviders.GetByID(ctx, *d.DNSProviderID)
		if err == nil {
			expectedNS, _ = fetchExpectedNSFromProvider(ctx, h.bindingSvc, provider, d.FQDN)
		}

		payload, err := json.Marshal(tasks.NSCheckPayload{
			DomainID:      d.ID,
			FQDN:          d.FQDN,
			DNSProviderID: *d.DNSProviderID,
			ExpectedNS:    expectedNS,
		})
		if err != nil {
			h.logger.Error("marshal ns check payload", zap.Int64("domain_id", d.ID), zap.Error(err))
			skipped++
			continue
		}

		task := asynq.NewTask(tasks.TypeNSCheck, payload,
			asynq.Queue("default"),
			asynq.MaxRetry(2),
			asynq.Timeout(30*time.Second),
		)
		if _, err := h.asynqClient.EnqueueContext(ctx, task); err != nil {
			h.logger.Error("enqueue domain:ns_check",
				zap.Int64("domain_id", d.ID),
				zap.String("fqdn", d.FQDN),
				zap.Error(err),
			)
			skipped++
			continue
		}
		enqueued++
	}

	h.logger.Info("domain:ns_check_all complete",
		zap.Int("total", len(domains)),
		zap.Int("enqueued", enqueued),
		zap.Int("skipped", skipped),
	)
	return nil
}

// fetchExpectedNSFromProvider resolves expected nameservers using the provider's API.
func fetchExpectedNSFromProvider(ctx context.Context, svc *Service, provider *postgres.DNSProvider, fqdn string) ([]string, error) {
	p, err := resolveProviderFromRecord(svc, provider)
	if err != nil {
		return nil, err
	}
	return fetchExpectedNS(ctx, p, fqdn)
}

// ── HandleNSCheck ─────────────────────────────────────────────────────────────

// HandleNSCheck is the asynq handler for TypeNSCheck.
// It verifies the NS delegation for a single domain and persists the result.
// If mismatch persists for > 24h it fires a warning alert.
type HandleNSCheck struct {
	domainStore  *postgres.DomainStore
	notifier     notify.Notifier
	rdb          *redis.Client
	logger       *zap.Logger
}

func NewHandleNSCheck(
	domainStore *postgres.DomainStore,
	notifier notify.Notifier,
	rdb *redis.Client,
	logger *zap.Logger,
) *HandleNSCheck {
	return &HandleNSCheck{
		domainStore: domainStore,
		notifier:    notifier,
		rdb:         rdb,
		logger:      logger,
	}
}

func (h *HandleNSCheck) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.NSCheckPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal ns check payload: %w", err)
	}

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := CheckNSDelegation(checkCtx, p.FQDN, p.ExpectedNS)
	if err != nil {
		// DNS lookup error — mark mismatch but don't fail the task (will retry).
		h.logger.Warn("ns delegation lookup failed",
			zap.String("fqdn", p.FQDN),
			zap.Error(err),
		)
		if updateErr := h.domainStore.UpdateNSDelegation(ctx, p.DomainID, "mismatch", pq.StringArray{}); updateErr != nil {
			h.logger.Error("update ns delegation on lookup error",
				zap.Int64("domain_id", p.DomainID),
				zap.Error(updateErr),
			)
		}
		return nil // don't retry on DNS errors — scheduler will re-enqueue next hour
	}

	// Persist result.
	newStatus := result.Status // "verified" | "mismatch"
	actual := pq.StringArray(result.Actual)
	if err := h.domainStore.UpdateNSDelegation(ctx, p.DomainID, newStatus, actual); err != nil {
		return fmt.Errorf("update ns delegation %d: %w", p.DomainID, err)
	}

	h.logger.Info("domain:ns_check complete",
		zap.String("fqdn", p.FQDN),
		zap.String("status", newStatus),
		zap.Strings("actual", result.Actual),
	)

	if newStatus == "verified" {
		// Clear any mismatch alert dedup key so future mismatches re-alert.
		h.clearMismatchAlerted(ctx, p.DomainID)
		return nil
	}

	// status == "mismatch" — check if it has been persisted for > 24h.
	domain, err := h.domainStore.GetByID(ctx, p.DomainID)
	if err != nil {
		h.logger.Warn("could not load domain for mismatch age check",
			zap.Int64("domain_id", p.DomainID),
			zap.Error(err),
		)
		return nil
	}

	// NSLastCheckedAt was just written by UpdateNSDelegation; use ns_verified_at
	// absence or age to determine how long mismatch has persisted.
	// If ns_verified_at is nil the domain was never verified — compute age from
	// ns_last_checked_at. If that is also nil, fallback to domain created_at.
	mismatchAge := time.Since(domain.CreatedAt)
	if domain.NSLastCheckedAt != nil {
		// How long since the domain entered non-verified state.
		// We approximate by: time since last check that came back as mismatch,
		// but we track it as "first mismatch" via the Redis dedup key set below.
		_ = domain.NSLastCheckedAt // used implicitly via trySetMismatchStart
	}

	if !h.trySetMismatchStart(ctx, p.DomainID) {
		// Already recorded start of mismatch — compute duration.
		mismatchAge = h.mismatchAge(ctx, p.DomainID)
	}

	if mismatchAge < 24*time.Hour {
		return nil
	}

	// Alert once per hour to avoid flooding (Critical Rule #8).
	if !h.trySetMismatchAlerted(ctx, p.DomainID) {
		h.logger.Info("ns:mismatch alert suppressed (dedup)",
			zap.String("fqdn", p.FQDN),
		)
		return nil
	}

	msg := notify.Message{
		Subject:  fmt.Sprintf("⚠️ NS 委派未生效 — %s (超過 24h)", p.FQDN),
		Body:     fmt.Sprintf("域名 %s 的 Nameserver 委派驗證失敗，已持續超過 24 小時。\n\n預期 NS：%v\n實際 NS：%v\n\n請確認域名在 Registrar 側的 NS 記錄是否已更新為 DNS 供應商的 NS。", p.FQDN, p.ExpectedNS, result.Actual),
		Severity: "warning",
	}
	if err := h.notifier.Send(ctx, msg); err != nil {
		h.logger.Warn("ns mismatch alert send failed",
			zap.String("fqdn", p.FQDN),
			zap.Error(err),
		)
		h.clearMismatchAlerted(ctx, p.DomainID)
	}

	return nil
}

// ── Redis dedup helpers ───────────────────────────────────────────────────────

const nsMismatchStartKey = "ns:mismatch:start:%d"
const nsMismatchAlertedKey = "ns:mismatch:alerted:%d"

// trySetMismatchStart records when the domain first entered mismatch state.
// Returns true if the key was newly set (first time), false if already set.
func (h *HandleNSCheck) trySetMismatchStart(ctx context.Context, domainID int64) bool {
	key := fmt.Sprintf(nsMismatchStartKey, domainID)
	// NX: only set if not already set. Expire after 7 days.
	ok, err := h.rdb.SetNX(ctx, key, time.Now().Unix(), 7*24*time.Hour).Result()
	if err != nil {
		return true // fail open
	}
	return ok
}

// mismatchAge returns how long the domain has been in mismatch state.
func (h *HandleNSCheck) mismatchAge(ctx context.Context, domainID int64) time.Duration {
	key := fmt.Sprintf(nsMismatchStartKey, domainID)
	ts, err := h.rdb.Get(ctx, key).Int64()
	if err != nil {
		return 0
	}
	return time.Since(time.Unix(ts, 0))
}

// trySetMismatchAlerted sets a 1h dedup key to suppress repeated alerts.
// Returns true if alert should be sent (key was set), false if suppressed.
func (h *HandleNSCheck) trySetMismatchAlerted(ctx context.Context, domainID int64) bool {
	key := fmt.Sprintf(nsMismatchAlertedKey, domainID)
	ok, err := h.rdb.SetNX(ctx, key, 1, time.Hour).Result()
	if err != nil {
		return true // fail open
	}
	return ok
}

// clearMismatchAlerted removes the alert dedup key and mismatch start time.
func (h *HandleNSCheck) clearMismatchAlerted(ctx context.Context, domainID int64) {
	keys := []string{
		fmt.Sprintf(nsMismatchAlertedKey, domainID),
		fmt.Sprintf(nsMismatchStartKey, domainID),
	}
	if err := h.rdb.Del(ctx, keys...).Err(); err != nil {
		h.logger.Warn("failed to clear ns mismatch dedup keys",
			zap.Int64("domain_id", domainID),
			zap.Error(err),
		)
	}
}
