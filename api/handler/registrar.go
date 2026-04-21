package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/registrar"
	"domain-platform/store/postgres"
)

type RegistrarHandler struct {
	svc    *registrar.Service
	logger *zap.Logger
}

func NewRegistrarHandler(svc *registrar.Service, logger *zap.Logger) *RegistrarHandler {
	return &RegistrarHandler{svc: svc, logger: logger}
}

// ── Request / Response types ──────────────────────────────────────────────────

type CreateRegistrarRequest struct {
	Name         string          `json:"name" binding:"required"`
	URL          *string         `json:"url"`
	APIType      *string         `json:"api_type"`
	Capabilities json.RawMessage `json:"capabilities"`
	Notes        *string         `json:"notes"`
}

type UpdateRegistrarRequest struct {
	Name         string          `json:"name" binding:"required"`
	URL          *string         `json:"url"`
	APIType      *string         `json:"api_type"`
	Capabilities json.RawMessage `json:"capabilities"`
	Notes        *string         `json:"notes"`
}

type CreateAccountRequest struct {
	AccountName string          `json:"account_name" binding:"required"`
	OwnerUserID *int64          `json:"owner_user_id"`
	Credentials json.RawMessage `json:"credentials"`
	IsDefault   bool            `json:"is_default"`
	Notes       *string         `json:"notes"`
}

type UpdateAccountRequest struct {
	AccountName string          `json:"account_name" binding:"required"`
	OwnerUserID *int64          `json:"owner_user_id"`
	Credentials json.RawMessage `json:"credentials"`
	IsDefault   bool            `json:"is_default"`
	Notes       *string         `json:"notes"`
}

// ── Registrar handlers ────────────────────────────────────────────────────────

// Create handles POST /api/v1/registrars
func (h *RegistrarHandler) Create(c *gin.Context) {
	var req CreateRegistrarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: name required",
		})
		return
	}

	r, err := h.svc.Create(c.Request.Context(), registrar.CreateInput{
		Name:         req.Name,
		URL:          req.URL,
		APIType:      req.APIType,
		Capabilities: req.Capabilities,
		Notes:        req.Notes,
	})
	if errors.Is(err, registrar.ErrDuplicateName) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40900, "data": nil, "message": "registrar name already exists",
		})
		return
	}
	if err != nil {
		h.logger.Error("create registrar", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": registrarResponse(r),
	})
}

// List handles GET /api/v1/registrars
func (h *RegistrarHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.logger.Error("list registrars", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	resp := make([]gin.H, 0, len(items))
	for i := range items {
		resp = append(resp, registrarResponse(&items[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": gin.H{
			"items": resp,
			"total": len(resp),
		},
	})
}

// Get handles GET /api/v1/registrars/:id
func (h *RegistrarHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid registrar id",
		})
		return
	}

	r, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, registrar.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get registrar", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": registrarResponse(r),
	})
}

// Update handles PUT /api/v1/registrars/:id
func (h *RegistrarHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid registrar id",
		})
		return
	}

	var req UpdateRegistrarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: name required",
		})
		return
	}

	r, err := h.svc.Update(c.Request.Context(), registrar.UpdateInput{
		ID:           id,
		Name:         req.Name,
		URL:          req.URL,
		APIType:      req.APIType,
		Capabilities: req.Capabilities,
		Notes:        req.Notes,
	})
	if errors.Is(err, registrar.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar not found",
		})
		return
	}
	if errors.Is(err, registrar.ErrDuplicateName) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40900, "data": nil, "message": "registrar name already exists",
		})
		return
	}
	if err != nil {
		h.logger.Error("update registrar", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": registrarResponse(r),
	})
}

// Delete handles DELETE /api/v1/registrars/:id
func (h *RegistrarHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid registrar id",
		})
		return
	}

	err = h.svc.Delete(c.Request.Context(), id)
	if errors.Is(err, registrar.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar not found",
		})
		return
	}
	if errors.Is(err, registrar.ErrHasDependents) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40901, "data": nil, "message": "registrar has dependent accounts or domains — detach first",
		})
		return
	}
	if err != nil {
		h.logger.Error("delete registrar", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// ── Account handlers ──────────────────────────────────────────────────────────

// CreateAccount handles POST /api/v1/registrars/:id/accounts
func (h *RegistrarHandler) CreateAccount(c *gin.Context) {
	registrarID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid registrar id",
		})
		return
	}

	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: account_name required",
		})
		return
	}

	a, err := h.svc.CreateAccount(c.Request.Context(), registrar.CreateAccountInput{
		RegistrarID: registrarID,
		AccountName: req.AccountName,
		OwnerUserID: req.OwnerUserID,
		Credentials: req.Credentials,
		IsDefault:   req.IsDefault,
		Notes:       req.Notes,
	})
	if errors.Is(err, registrar.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("create registrar account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": accountResponse(a),
	})
}

// ListAccounts handles GET /api/v1/registrars/:id/accounts
func (h *RegistrarHandler) ListAccounts(c *gin.Context) {
	registrarID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid registrar id",
		})
		return
	}

	accounts, err := h.svc.ListAccounts(c.Request.Context(), registrarID)
	if errors.Is(err, registrar.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("list registrar accounts", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	resp := make([]gin.H, 0, len(accounts))
	for i := range accounts {
		resp = append(resp, accountResponse(&accounts[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": gin.H{
			"items": resp,
			"total": len(resp),
		},
	})
}

// GetAccount handles GET /api/v1/registrar-accounts/:id
func (h *RegistrarHandler) GetAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid account id",
		})
		return
	}

	a, err := h.svc.GetAccount(c.Request.Context(), id)
	if errors.Is(err, registrar.ErrAccountNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar account not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get registrar account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": accountResponse(a),
	})
}

// UpdateAccount handles PUT /api/v1/registrar-accounts/:id
func (h *RegistrarHandler) UpdateAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid account id",
		})
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: account_name required",
		})
		return
	}

	a, err := h.svc.UpdateAccount(c.Request.Context(), registrar.UpdateAccountInput{
		ID:          id,
		AccountName: req.AccountName,
		OwnerUserID: req.OwnerUserID,
		Credentials: req.Credentials,
		IsDefault:   req.IsDefault,
		Notes:       req.Notes,
	})
	if errors.Is(err, registrar.ErrAccountNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar account not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("update registrar account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": accountResponse(a),
	})
}

// DeleteAccount handles DELETE /api/v1/registrar-accounts/:id
func (h *RegistrarHandler) DeleteAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid account id",
		})
		return
	}

	err = h.svc.DeleteAccount(c.Request.Context(), id)
	if errors.Is(err, registrar.ErrAccountNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "registrar account not found",
		})
		return
	}
	if errors.Is(err, registrar.ErrHasDependents) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40901, "data": nil, "message": "account has dependent domains — detach first",
		})
		return
	}
	if err != nil {
		h.logger.Error("delete registrar account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// ── Response builders ─────────────────────────────────────────────────────────

func registrarResponse(r *postgres.Registrar) gin.H {
	return gin.H{
		"id":           r.ID,
		"uuid":         r.UUID,
		"name":         r.Name,
		"url":          r.URL,
		"api_type":     r.APIType,
		"capabilities": r.Capabilities,
		"notes":        r.Notes,
		"created_at":   r.CreatedAt,
		"updated_at":   r.UpdatedAt,
	}
}

func accountResponse(a *postgres.RegistrarAccount) gin.H {
	return gin.H{
		"id":            a.ID,
		"uuid":          a.UUID,
		"registrar_id":  a.RegistrarID,
		"account_name":  a.AccountName,
		"owner_user_id": a.OwnerUserID,
		"is_default":    a.IsDefault,
		"notes":         a.Notes,
		"created_at":    a.CreatedAt,
		"updated_at":    a.UpdatedAt,
		// NOTE: credentials are intentionally omitted from responses (security)
	}
}
