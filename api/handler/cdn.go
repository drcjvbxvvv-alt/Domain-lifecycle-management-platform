package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	cdnsvc "domain-platform/internal/cdn"
)

// CDNHandler handles /api/v1/cdn-providers and /api/v1/cdn-accounts.
type CDNHandler struct {
	svc    *cdnsvc.Service
	logger *zap.Logger
}

func NewCDNHandler(svc *cdnsvc.Service, logger *zap.Logger) *CDNHandler {
	return &CDNHandler{svc: svc, logger: logger}
}

// ── Request / Response types ──────────────────────────────────────────────────

type CreateCDNProviderRequest struct {
	Name         string  `json:"name"          binding:"required"`
	ProviderType string  `json:"provider_type" binding:"required"`
	Description  *string `json:"description"`
}

type UpdateCDNProviderRequest struct {
	Name         string  `json:"name"          binding:"required"`
	ProviderType string  `json:"provider_type" binding:"required"`
	Description  *string `json:"description"`
}

type CreateCDNAccountRequest struct {
	AccountName string          `json:"account_name" binding:"required"`
	Credentials json.RawMessage `json:"credentials"`
	Notes       *string         `json:"notes"`
	Enabled     *bool           `json:"enabled"`
}

type UpdateCDNAccountRequest struct {
	AccountName string          `json:"account_name" binding:"required"`
	Credentials json.RawMessage `json:"credentials"`
	Notes       *string         `json:"notes"`
	Enabled     *bool           `json:"enabled"`
}

// ── CDN Provider handlers ─────────────────────────────────────────────────────

// ListProviders handles GET /api/v1/cdn-providers.
func (h *CDNHandler) ListProviders(c *gin.Context) {
	providers, err := h.svc.ListProviders(c.Request.Context())
	if err != nil {
		h.logger.Error("list cdn providers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to list cdn providers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": providers, "total": len(providers)}, "message": "ok"})
}

// CreateProvider handles POST /api/v1/cdn-providers.
func (h *CDNHandler) CreateProvider(c *gin.Context) {
	var req CreateCDNProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	p, err := h.svc.CreateProvider(c.Request.Context(), cdnsvc.CreateProviderInput{
		Name:         req.Name,
		ProviderType: req.ProviderType,
		Description:  req.Description,
	})
	if err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": p, "message": "ok"})
}

// GetProvider handles GET /api/v1/cdn-providers/:id.
func (h *CDNHandler) GetProvider(c *gin.Context) {
	id, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	p, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": p, "message": "ok"})
}

// UpdateProvider handles PUT /api/v1/cdn-providers/:id.
func (h *CDNHandler) UpdateProvider(c *gin.Context) {
	id, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	var req UpdateCDNProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	if err := h.svc.UpdateProvider(c.Request.Context(), id, cdnsvc.UpdateProviderInput{
		Name:         req.Name,
		ProviderType: req.ProviderType,
		Description:  req.Description,
	}); err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "ok"})
}

// DeleteProvider handles DELETE /api/v1/cdn-providers/:id.
func (h *CDNHandler) DeleteProvider(c *gin.Context) {
	id, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	if err := h.svc.DeleteProvider(c.Request.Context(), id); err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.Status(http.StatusNoContent)
}

// ── CDN Account handlers ──────────────────────────────────────────────────────

// ListAccounts handles GET /api/v1/cdn-providers/:id/accounts.
func (h *CDNHandler) ListAccounts(c *gin.Context) {
	providerID, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid provider id"})
		return
	}

	accounts, err := h.svc.ListAccountsByProvider(c.Request.Context(), providerID)
	if err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": accounts, "total": len(accounts)}, "message": "ok"})
}

// ListAllAccounts handles GET /api/v1/cdn-accounts (all providers, enabled only).
func (h *CDNHandler) ListAllAccounts(c *gin.Context) {
	accounts, err := h.svc.ListAllAccounts(c.Request.Context())
	if err != nil {
		h.logger.Error("list all cdn accounts", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to list cdn accounts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": accounts, "total": len(accounts)}, "message": "ok"})
}

// CreateAccount handles POST /api/v1/cdn-providers/:id/accounts.
func (h *CDNHandler) CreateAccount(c *gin.Context) {
	providerID, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid provider id"})
		return
	}

	var req CreateCDNAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	userID := userIDFromContext(c)
	a, err := h.svc.CreateAccount(c.Request.Context(), cdnsvc.CreateAccountInput{
		CDNProviderID: providerID,
		AccountName:   req.AccountName,
		Credentials:   req.Credentials,
		Notes:         req.Notes,
		Enabled:       enabled,
		CreatedBy:     userID,
	})
	if err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": a, "message": "ok"})
}

// GetAccount handles GET /api/v1/cdn-accounts/:id.
func (h *CDNHandler) GetAccount(c *gin.Context) {
	id, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	a, err := h.svc.GetAccount(c.Request.Context(), id)
	if err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": a, "message": "ok"})
}

// UpdateAccount handles PUT /api/v1/cdn-accounts/:id.
func (h *CDNHandler) UpdateAccount(c *gin.Context) {
	id, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	var req UpdateCDNAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := h.svc.UpdateAccount(c.Request.Context(), id, cdnsvc.UpdateAccountInput{
		AccountName: req.AccountName,
		Credentials: req.Credentials,
		Notes:       req.Notes,
		Enabled:     enabled,
	}); err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "ok"})
}

// DeleteAccount handles DELETE /api/v1/cdn-accounts/:id.
func (h *CDNHandler) DeleteAccount(c *gin.Context) {
	id, err := parseParamID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	if err := h.svc.DeleteAccount(c.Request.Context(), id); err != nil {
		status, code, msg := cdnErrStatus(err)
		c.JSON(status, gin.H{"code": code, "data": nil, "message": msg})
		return
	}
	c.Status(http.StatusNoContent)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseParamID(c *gin.Context, param string) (int64, error) {
	return strconv.ParseInt(c.Param(param), 10, 64)
}

// userIDFromContext extracts the authenticated user's ID from the Gin context.
// Returns nil when the user is not authenticated (should not happen on authed routes).
func userIDFromContext(c *gin.Context) *int64 {
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(int64); ok {
			return &id
		}
	}
	return nil
}

// cdnErrStatus maps service errors to HTTP status, error code, and message.
func cdnErrStatus(err error) (int, int, string) {
	switch {
	case errors.Is(err, cdnsvc.ErrProviderNotFound),
		errors.Is(err, cdnsvc.ErrAccountNotFound):
		return http.StatusNotFound, 40400, err.Error()
	case errors.Is(err, cdnsvc.ErrProviderDuplicate),
		errors.Is(err, cdnsvc.ErrAccountDuplicate):
		return http.StatusConflict, 40900, err.Error()
	case errors.Is(err, cdnsvc.ErrProviderHasDependents),
		errors.Is(err, cdnsvc.ErrAccountHasDependents):
		return http.StatusConflict, 40901, err.Error()
	default:
		return http.StatusBadRequest, 40000, err.Error()
	}
}
