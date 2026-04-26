package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/dnsrecord"
	"domain-platform/internal/tasks"
	"domain-platform/store/postgres"
)

// DNSBindingHandler handles domain ↔ DNS provider binding endpoints.
//
// Routes:
//   PUT  /api/v1/domains/:id/dns-binding          — bind or unbind a DNS provider
//   GET  /api/v1/domains/:id/dns-binding          — read current binding + NS status
//   POST /api/v1/domains/:id/dns-binding/verify   — manually trigger NS delegation check
type DNSBindingHandler struct {
	svc         *dnsrecord.Service
	domains     *postgres.DomainStore
	asynqClient *asynq.Client
	logger      *zap.Logger
}

// NewDNSBindingHandler creates a new DNSBindingHandler.
func NewDNSBindingHandler(svc *dnsrecord.Service, domains *postgres.DomainStore, asynqClient *asynq.Client, logger *zap.Logger) *DNSBindingHandler {
	return &DNSBindingHandler{svc: svc, domains: domains, asynqClient: asynqClient, logger: logger}
}

// ── Request types ─────────────────────────────────────────────────────────────

// BindDNSProviderRequest is the body for PUT /domains/:id/dns-binding.
// Set dns_provider_id to null to unbind.
type BindDNSProviderRequest struct {
	DNSProviderID *int64 `json:"dns_provider_id"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Bind handles PUT /api/v1/domains/:id/dns-binding.
// Pass { "dns_provider_id": 5 } to bind; pass { "dns_provider_id": null } to unbind.
func (h *DNSBindingHandler) Bind(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	var req BindDNSProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	domain, err := h.domains.GetByID(ctx, domainID)
	if err != nil {
		if errors.Is(err, postgres.ErrDomainNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "domain not found"})
			return
		}
		h.logger.Error("get domain for dns binding", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	if req.DNSProviderID == nil {
		// Unbind
		if err := h.svc.UnbindDNSProvider(ctx, domain); err != nil {
			h.logger.Error("unbind dns provider", zap.Int64("domain_id", domainID), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to unbind dns provider"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"data":    gin.H{"ns_delegation_status": "unset"},
			"message": "ok",
		})
		return
	}

	// Bind
	status, err := h.svc.BindDNSProvider(ctx, domain, *req.DNSProviderID)
	if err != nil {
		if errors.Is(err, dnsrecord.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40402, "data": nil, "message": "dns provider not found"})
			return
		}
		h.logger.Error("bind dns provider",
			zap.Int64("domain_id", domainID),
			zap.Int64("provider_id", *req.DNSProviderID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to bind dns provider"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": status, "message": "ok"})
}

// GetStatus handles GET /api/v1/domains/:id/dns-binding.
// Returns the current binding status, expected NSes from provider, and actual NSes.
func (h *DNSBindingHandler) GetStatus(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	ctx := c.Request.Context()
	domain, err := h.domains.GetByID(ctx, domainID)
	if err != nil {
		if errors.Is(err, postgres.ErrDomainNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "domain not found"})
			return
		}
		h.logger.Error("get domain for dns binding status", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	status, err := h.svc.GetBindingStatus(ctx, domain)
	if err != nil {
		h.logger.Error("get dns binding status", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to get binding status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": status, "message": "ok"})
}

// TriggerVerify handles POST /api/v1/domains/:id/dns-binding/verify.
// Manually enqueues a TypeNSCheck task so the operator does not have to wait
// for the hourly scheduler.
func (h *DNSBindingHandler) TriggerVerify(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	ctx := c.Request.Context()
	domain, err := h.domains.GetByID(ctx, domainID)
	if err != nil {
		if errors.Is(err, postgres.ErrDomainNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "domain not found"})
			return
		}
		h.logger.Error("get domain for ns verify trigger", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	if domain.DNSProviderID == nil {
		c.JSON(http.StatusConflict, gin.H{"code": 40901, "data": nil, "message": "domain has no DNS provider bound"})
		return
	}

	// Fetch expected NS for the payload.
	bindStatus, _ := h.svc.GetBindingStatus(ctx, domain)
	var expectedNS []string
	if bindStatus != nil {
		expectedNS = bindStatus.ExpectedNameservers
	}

	payload, _ := json.Marshal(tasks.NSCheckPayload{
		DomainID:      domain.ID,
		FQDN:          domain.FQDN,
		DNSProviderID: *domain.DNSProviderID,
		ExpectedNS:    expectedNS,
	})

	task := asynq.NewTask(tasks.TypeNSCheck, payload,
		asynq.Queue("default"),
		asynq.MaxRetry(1),
		asynq.Timeout(30*time.Second),
	)
	if _, err := h.asynqClient.EnqueueContext(ctx, task); err != nil {
		h.logger.Error("enqueue ns check",
			zap.Int64("domain_id", domainID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to enqueue ns check"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": gin.H{"queued": true}, "message": "ns check queued"})
}
