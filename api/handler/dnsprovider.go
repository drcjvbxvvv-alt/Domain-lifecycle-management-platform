package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/dnsprovider"
	"domain-platform/store/postgres"
)

type DNSProviderHandler struct {
	svc    *dnsprovider.Service
	logger *zap.Logger
}

func NewDNSProviderHandler(svc *dnsprovider.Service, logger *zap.Logger) *DNSProviderHandler {
	return &DNSProviderHandler{svc: svc, logger: logger}
}

// ── Request types ─────────────────────────────────────────────────────────────

type CreateDNSProviderRequest struct {
	Name         string          `json:"name" binding:"required"`
	ProviderType string          `json:"provider_type" binding:"required"`
	Config       json.RawMessage `json:"config"`
	Credentials  json.RawMessage `json:"credentials"`
	Notes        *string         `json:"notes"`
}

type UpdateDNSProviderRequest struct {
	Name         string          `json:"name" binding:"required"`
	ProviderType string          `json:"provider_type" binding:"required"`
	Config       json.RawMessage `json:"config"`
	Credentials  json.RawMessage `json:"credentials"`
	Notes        *string         `json:"notes"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Create handles POST /api/v1/dns-providers
func (h *DNSProviderHandler) Create(c *gin.Context) {
	var req CreateDNSProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: name and provider_type required",
		})
		return
	}

	p, err := h.svc.Create(c.Request.Context(), dnsprovider.CreateInput{
		Name:         req.Name,
		ProviderType: req.ProviderType,
		Config:       req.Config,
		Credentials:  req.Credentials,
		Notes:        req.Notes,
	})
	if errors.Is(err, dnsprovider.ErrInvalidProviderType) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    40001,
			"data":    gin.H{"supported_types": dnsprovider.SupportedTypes()},
			"message": "unknown provider_type — see data.supported_types for valid values",
		})
		return
	}
	if err != nil {
		h.logger.Error("create dns provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": dnsProviderResponse(p),
	})
}

// List handles GET /api/v1/dns-providers
func (h *DNSProviderHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.logger.Error("list dns providers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	resp := make([]gin.H, 0, len(items))
	for i := range items {
		resp = append(resp, dnsProviderResponse(&items[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": gin.H{
			"items": resp,
			"total": len(resp),
		},
	})
}

// Get handles GET /api/v1/dns-providers/:id
func (h *DNSProviderHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid dns provider id",
		})
		return
	}

	p, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, dnsprovider.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "dns provider not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get dns provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": dnsProviderResponse(p),
	})
}

// Update handles PUT /api/v1/dns-providers/:id
func (h *DNSProviderHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid dns provider id",
		})
		return
	}

	var req UpdateDNSProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: name and provider_type required",
		})
		return
	}

	p, err := h.svc.Update(c.Request.Context(), dnsprovider.UpdateInput{
		ID:           id,
		Name:         req.Name,
		ProviderType: req.ProviderType,
		Config:       req.Config,
		Credentials:  req.Credentials,
		Notes:        req.Notes,
	})
	if errors.Is(err, dnsprovider.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "dns provider not found",
		})
		return
	}
	if errors.Is(err, dnsprovider.ErrInvalidProviderType) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    40001,
			"data":    gin.H{"supported_types": dnsprovider.SupportedTypes()},
			"message": "unknown provider_type",
		})
		return
	}
	if err != nil {
		h.logger.Error("update dns provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": dnsProviderResponse(p),
	})
}

// Delete handles DELETE /api/v1/dns-providers/:id
func (h *DNSProviderHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid dns provider id",
		})
		return
	}

	err = h.svc.Delete(c.Request.Context(), id)
	if errors.Is(err, dnsprovider.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "dns provider not found",
		})
		return
	}
	if errors.Is(err, dnsprovider.ErrHasDependents) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40901, "data": nil, "message": "dns provider has dependent domains — detach first",
		})
		return
	}
	if err != nil {
		h.logger.Error("delete dns provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// SupportedTypes handles GET /api/v1/dns-providers/types
// Returns the list of known provider_type values for frontend dropdowns.
func (h *DNSProviderHandler) SupportedTypes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": dnsprovider.SupportedTypes(),
	})
}

// ── Response builder ──────────────────────────────────────────────────────────

func dnsProviderResponse(p *postgres.DNSProvider) gin.H {
	return gin.H{
		"id":            p.ID,
		"uuid":          p.UUID,
		"name":          p.Name,
		"provider_type": p.ProviderType,
		"config":        p.Config,
		"notes":         p.Notes,
		"created_at":    p.CreatedAt,
		"updated_at":    p.UpdatedAt,
		// NOTE: credentials intentionally omitted (security)
	}
}
