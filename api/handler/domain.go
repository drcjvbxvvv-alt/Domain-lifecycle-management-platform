package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/middleware"
	"domain-platform/internal/lifecycle"
	"domain-platform/store/postgres"
)

type DomainHandler struct {
	svc    *lifecycle.Service
	logger *zap.Logger
}

func NewDomainHandler(svc *lifecycle.Service, logger *zap.Logger) *DomainHandler {
	return &DomainHandler{svc: svc, logger: logger}
}

// ── Register ──────────────────────────────────────────────────────────────────

type RegisterDomainRequest struct {
	ProjectID          int64    `json:"project_id"           binding:"required"`
	FQDN               string   `json:"fqdn"                 binding:"required"`
	DNSProviderID      *int64   `json:"dns_provider_id"`
	RegistrarAccountID *int64   `json:"registrar_account_id"`
	CDNAccountID       *int64   `json:"cdn_account_id"`
	OriginIPs          []string `json:"origin_ips"`
	RegistrationDate   *string  `json:"registration_date"` // RFC3339 date
	ExpiryDate         *string  `json:"expiry_date"`
	AutoRenew          bool     `json:"auto_renew"`
	AnnualCost         *float64 `json:"annual_cost"`
	Currency           *string  `json:"currency"`
	Purpose            *string  `json:"purpose"`
	Notes              *string  `json:"notes"`
}

// Register handles POST /api/v1/domains — creates a domain in "requested" state.
func (h *DomainHandler) Register(c *gin.Context) {
	var req RegisterDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: project_id and fqdn required",
		})
		return
	}

	userID := middleware.GetUserID(c)

	in := lifecycle.RegisterInput{
		ProjectID:          req.ProjectID,
		FQDN:               req.FQDN,
		OwnerUserID:        &userID,
		DNSProviderID:      req.DNSProviderID,
		RegistrarAccountID: req.RegistrarAccountID,
		CDNAccountID:       req.CDNAccountID,
		OriginIPs:          req.OriginIPs,
		AutoRenew:          req.AutoRenew,
		AnnualCost:         req.AnnualCost,
		Currency:           req.Currency,
		Purpose:            req.Purpose,
		Notes:              req.Notes,
		TriggeredBy:        fmt.Sprintf("user:%d", userID),
	}

	if req.RegistrationDate != nil {
		if t, err := time.Parse(time.DateOnly, *req.RegistrationDate); err == nil {
			in.RegistrationDate = &t
		}
	}
	if req.ExpiryDate != nil {
		if t, err := time.Parse(time.DateOnly, *req.ExpiryDate); err == nil {
			in.ExpiryDate = &t
		}
	}

	d, err := h.svc.Register(c.Request.Context(), in)
	if errors.Is(err, lifecycle.ErrDuplicateFQDN) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40900, "data": nil, "message": "domain fqdn already exists",
		})
		return
	}
	if err != nil {
		h.logger.Error("register domain", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(d),
	})
}

// ── Update asset fields ───────────────────────────────────────────────────────

type UpdateDomainAssetRequest struct {
	RegistrarAccountID *int64   `json:"registrar_account_id"`
	DNSProviderID      *int64   `json:"dns_provider_id"`
	CDNAccountID       *int64   `json:"cdn_account_id"`
	OriginIPs          []string `json:"origin_ips"`
	RegistrationDate   *string  `json:"registration_date"`
	ExpiryDate         *string  `json:"expiry_date"`
	AutoRenew          bool     `json:"auto_renew"`
	TransferLock       bool     `json:"transfer_lock"`
	Hold               bool     `json:"hold"`
	DNSSECEnabled      bool     `json:"dnssec_enabled"`
	WhoisPrivacy       bool     `json:"whois_privacy"`
	AnnualCost         *float64 `json:"annual_cost"`
	Currency           *string  `json:"currency"`
	PurchasePrice      *float64 `json:"purchase_price"`
	FeeFixed           bool     `json:"fee_fixed"`
	Purpose            *string  `json:"purpose"`
	Notes              *string  `json:"notes"`
}

// UpdateAsset handles PUT /api/v1/domains/:id — updates asset fields only.
// Does NOT change lifecycle_state (use /transition for that).
func (h *DomainHandler) UpdateAsset(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	var req UpdateDomainAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request body",
		})
		return
	}

	in := lifecycle.UpdateAssetInput{
		ID:                 id,
		RegistrarAccountID: req.RegistrarAccountID,
		DNSProviderID:      req.DNSProviderID,
		CDNAccountID:       req.CDNAccountID,
		OriginIPs:          req.OriginIPs,
		AutoRenew:          req.AutoRenew,
		TransferLock:       req.TransferLock,
		Hold:               req.Hold,
		DNSSECEnabled:      req.DNSSECEnabled,
		WhoisPrivacy:       req.WhoisPrivacy,
		AnnualCost:         req.AnnualCost,
		Currency:           req.Currency,
		PurchasePrice:      req.PurchasePrice,
		FeeFixed:           req.FeeFixed,
		Purpose:            req.Purpose,
		Notes:              req.Notes,
	}

	if req.RegistrationDate != nil {
		if t, err := time.Parse(time.DateOnly, *req.RegistrationDate); err == nil {
			in.RegistrationDate = &t
		}
	}
	if req.ExpiryDate != nil {
		if t, err := time.Parse(time.DateOnly, *req.ExpiryDate); err == nil {
			in.ExpiryDate = &t
		}
	}

	d, err := h.svc.UpdateAsset(c.Request.Context(), in)
	if errors.Is(err, lifecycle.ErrDomainNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "domain not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("update domain asset", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(d),
	})
}

// ── Transition ────────────────────────────────────────────────────────────────

type TransitionRequest struct {
	To     string `json:"to" binding:"required"`
	Reason string `json:"reason"`
}

// Transition handles POST /api/v1/domains/:id/transition
func (h *DomainHandler) Transition(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	var req TransitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: 'to' state required",
		})
		return
	}

	domain, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, postgres.ErrDomainNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "domain not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get domain for transition", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	userID := middleware.GetUserID(c)
	triggeredBy := fmt.Sprintf("user:%d", userID)

	err = h.svc.Transition(c.Request.Context(), id, domain.LifecycleState, req.To, req.Reason, triggeredBy)
	if errors.Is(err, lifecycle.ErrInvalidLifecycleState) {
		c.JSON(http.StatusConflict, gin.H{
			"code":    40900,
			"data":    nil,
			"message": fmt.Sprintf("invalid transition: %s → %s", domain.LifecycleState, req.To),
		})
		return
	}
	if errors.Is(err, lifecycle.ErrLifecycleRaceCondition) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40901, "data": nil, "message": "state changed concurrently, please retry",
		})
		return
	}
	if err != nil {
		h.logger.Error("transition domain", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	updated, _ := h.svc.GetByID(c.Request.Context(), id)
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(updated),
	})
}

// ── Get ───────────────────────────────────────────────────────────────────────

// Get handles GET /api/v1/domains/:id
func (h *DomainHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	d, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, postgres.ErrDomainNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "domain not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get domain", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(d),
	})
}

// ── List ──────────────────────────────────────────────────────────────────────

// List handles GET /api/v1/domains with optional filters.
// Supported query params: project_id, registrar_id, dns_provider_id, tld,
// expiry_status, lifecycle_state, cursor, limit.
func (h *DomainHandler) List(c *gin.Context) {
	in := lifecycle.ListInput{}

	if v := c.Query("project_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.ProjectID = &id
		}
	}
	if v := c.Query("registrar_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.RegistrarID = &id
		}
	}
	if v := c.Query("dns_provider_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.DNSProviderID = &id
		}
	}
	if v := c.Query("cdn_account_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.CDNAccountID = &id
		}
	}
	if v := c.Query("tld"); v != "" {
		in.TLD = &v
	}
	if v := c.Query("expiry_status"); v != "" {
		in.ExpiryStatus = &v
	}
	if v := c.Query("lifecycle_state"); v != "" {
		in.LifecycleState = &v
	}
	if v := c.Query("tag_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.TagID = &id
		}
	}

	in.Cursor, _ = strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	in.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))

	result, err := h.svc.List(c.Request.Context(), in)
	if err != nil {
		h.logger.Error("list domains", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	items := make([]gin.H, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, domainResponse(&result.Items[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok",
		"data": gin.H{
			"items":  items,
			"total":  result.Total,
			"cursor": result.Cursor,
		},
	})
}

// ── Expiring ──────────────────────────────────────────────────────────────────

// Expiring handles GET /api/v1/domains/expiring?days=30
func (h *DomainHandler) Expiring(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 {
		days = 30
	}

	domains, err := h.svc.ListExpiring(c.Request.Context(), days)
	if err != nil {
		h.logger.Error("list expiring domains", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	items := make([]gin.H, 0, len(domains))
	for i := range domains {
		items = append(items, domainResponse(&domains[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok",
		"data": gin.H{"items": items, "total": len(items), "days": days},
	})
}

// ── Stats ─────────────────────────────────────────────────────────────────────

// Stats handles GET /api/v1/domains/stats?project_id=X
func (h *DomainHandler) Stats(c *gin.Context) {
	var projectID *int64
	if v := c.Query("project_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			projectID = &id
		}
	}

	stats, err := h.svc.GetStats(c.Request.Context(), projectID)
	if err != nil {
		h.logger.Error("get domain stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok",
		"data": gin.H{
			"total":        stats.Total,
			"by_registrar": stats.ByRegistrar,
			"by_tld":       stats.ByTLD,
			"by_lifecycle": stats.ByLifecycle,
			"expiring_30d": stats.Expiring30d,
			"expiring_7d":  stats.Expiring7d,
		},
	})
}

// ── History ───────────────────────────────────────────────────────────────────

// History handles GET /api/v1/domains/:id/history
func (h *DomainHandler) History(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	rows, err := h.svc.GetHistory(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("get domain history", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": rows,
	})
}

// ── Transfer ──────────────────────────────────────────────────────────────────

type InitiateTransferRequest struct {
	GainingRegistrar *string `json:"gaining_registrar"`
	Notes            *string `json:"notes"`
}

// InitiateTransfer handles POST /api/v1/domains/:id/transfer
func (h *DomainHandler) InitiateTransfer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	var req InitiateTransferRequest
	_ = c.ShouldBindJSON(&req)

	d, err := h.svc.InitiateTransfer(c.Request.Context(), lifecycle.InitiateTransferInput{
		DomainID:                id,
		GainingRegistrarAccount: req.GainingRegistrar,
		Notes:                   req.Notes,
	})
	if errors.Is(err, lifecycle.ErrDomainNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "domain not found",
		})
		return
	}
	if errors.Is(err, lifecycle.ErrTransferAlreadyPending) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40902, "data": nil, "message": "domain already has a pending transfer",
		})
		return
	}
	if err != nil {
		h.logger.Error("initiate transfer", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(d),
	})
}

// CompleteTransfer handles POST /api/v1/domains/:id/transfer/complete
func (h *DomainHandler) CompleteTransfer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	d, err := h.svc.CompleteTransfer(c.Request.Context(), id)
	if errors.Is(err, lifecycle.ErrDomainNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "domain not found",
		})
		return
	}
	if errors.Is(err, lifecycle.ErrNoActiveTransfer) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40903, "data": nil, "message": "no active transfer to complete",
		})
		return
	}
	if err != nil {
		h.logger.Error("complete transfer", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(d),
	})
}

// CancelTransfer handles POST /api/v1/domains/:id/transfer/cancel
func (h *DomainHandler) CancelTransfer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid domain id",
		})
		return
	}

	d, err := h.svc.CancelTransfer(c.Request.Context(), id)
	if errors.Is(err, lifecycle.ErrDomainNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "domain not found",
		})
		return
	}
	if errors.Is(err, lifecycle.ErrNoActiveTransfer) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40903, "data": nil, "message": "no active transfer to cancel",
		})
		return
	}
	if err != nil {
		h.logger.Error("cancel transfer", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(d),
	})
}

// ── Response builder ──────────────────────────────────────────────────────────

func domainResponse(d *postgres.Domain) gin.H {
	return gin.H{
		// Core identity
		"id":              d.ID,
		"uuid":            d.UUID,
		"project_id":      d.ProjectID,
		"fqdn":            d.FQDN,
		"tld":             d.TLD,
		"lifecycle_state": d.LifecycleState,
		"owner_user_id":   d.OwnerUserID,

		// Provider binding
		"registrar_account_id": d.RegistrarAccountID,
		"dns_provider_id":      d.DNSProviderID,
		"cdn_account_id":       d.CDNAccountID,
		"origin_ips":           d.OriginIPs,

		// Registration & expiry
		"registration_date": d.RegistrationDate,
		"expiry_date":        d.ExpiryDate,
		"auto_renew":         d.AutoRenew,
		"grace_end_date":     d.GraceEndDate,
		"expiry_status":      d.ExpiryStatus,

		// Status flags
		"transfer_lock": d.TransferLock,
		"hold":          d.Hold,

		// Transfer tracking
		"transfer_status":            d.TransferStatus,
		"transfer_gaining_registrar": d.TransferGainingRegistrar,
		"transfer_requested_at":      d.TransferRequestedAt,
		"transfer_completed_at":      d.TransferCompletedAt,
		"last_transfer_at":           d.LastTransferAt,
		"last_renewed_at":            d.LastRenewedAt,

		// DNS
		"nameservers":    d.Nameservers,
		"dnssec_enabled": d.DNSSECEnabled,

		// WHOIS
		"whois_privacy":      d.WhoisPrivacy,
		"registrant_contact": d.RegistrantContact,
		"admin_contact":      d.AdminContact,
		"tech_contact":       d.TechContact,

		// Financial
		"annual_cost":    d.AnnualCost,
		"currency":       d.Currency,
		"purchase_price": d.PurchasePrice,
		"fee_fixed":      d.FeeFixed,

		// Metadata
		"purpose":  d.Purpose,
		"notes":    d.Notes,
		"metadata": d.Metadata,

		// Drift / sync tracking
		"last_sync_at":  d.LastSyncAt,
		"last_drift_at": d.LastDriftAt,

		"created_at": d.CreatedAt,
		"updated_at": d.UpdatedAt,
	}
}
