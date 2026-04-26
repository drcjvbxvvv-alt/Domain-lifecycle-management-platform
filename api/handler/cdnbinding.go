package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/cdndomain"
	cdnprovider "domain-platform/pkg/provider/cdn"
	"domain-platform/store/postgres"
)

// CDNBindingHandler handles CDN domain lifecycle endpoints.
//
// Routes (nested under /api/v1/domains/:id):
//   POST   /cdn-bindings              — bind domain to a CDN account
//   GET    /cdn-bindings              — list all active bindings for the domain
//   GET    /cdn-bindings/:bid         — get a single binding (local snapshot)
//   DELETE /cdn-bindings/:bid         — unbind (remove from CDN + soft-delete locally)
//   POST   /cdn-bindings/:bid/refresh — poll CDN provider for live status + CNAME
type CDNBindingHandler struct {
	svc     *cdndomain.Service
	domains *postgres.DomainStore
	logger  *zap.Logger
}

// NewCDNBindingHandler creates a CDNBindingHandler.
func NewCDNBindingHandler(
	svc *cdndomain.Service,
	domains *postgres.DomainStore,
	logger *zap.Logger,
) *CDNBindingHandler {
	return &CDNBindingHandler{svc: svc, domains: domains, logger: logger}
}

// ── Request types ─────────────────────────────────────────────────────────────

// BindCDNRequest is the body for POST /domains/:id/cdn-bindings.
type BindCDNRequest struct {
	CDNAccountID int64  `json:"cdn_account_id" binding:"required"`
	BusinessType string `json:"business_type"` // web | download | media; defaults to web
}

// ── helpers ───────────────────────────────────────────────────────────────────

// getDomain parses the :id param and loads the domain, writing error responses on failure.
func (h *CDNBindingHandler) getDomain(c *gin.Context) (*postgres.Domain, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return nil, false
	}
	domain, err := h.domains.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrDomainNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "domain not found"})
			return nil, false
		}
		h.logger.Error("get domain for cdn binding", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return nil, false
	}
	return domain, true
}

// getBindingID parses the :bid URL param.
func (h *CDNBindingHandler) getBindingID(c *gin.Context) (int64, bool) {
	bid, err := strconv.ParseInt(c.Param("bid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid binding id"})
		return 0, false
	}
	return bid, true
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Bind handles POST /domains/:id/cdn-bindings.
// Registers the domain on the CDN provider and persists the binding locally.
func (h *CDNBindingHandler) Bind(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	var req BindCDNRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "cdn_account_id is required"})
		return
	}

	resp, err := h.svc.BindDomain(c.Request.Context(), domain, req.CDNAccountID, req.BusinessType)
	if err != nil {
		switch {
		case errors.Is(err, cdndomain.ErrBindingAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": "domain already bound to this cdn account"})
		case errors.Is(err, cdndomain.ErrAccountNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": 40402, "data": nil, "message": "cdn account not found"})
		case errors.Is(err, cdndomain.ErrNoCDNProvider):
			c.JSON(http.StatusConflict, gin.H{"code": 40901, "data": nil, "message": err.Error()})
		case errors.Is(err, cdnprovider.ErrUnauthorized):
			c.JSON(http.StatusUnauthorized, gin.H{"code": 40100, "data": nil, "message": "cdn provider credentials rejected"})
		default:
			h.logger.Error("bind cdn domain",
				zap.String("fqdn", domain.FQDN),
				zap.Int64("cdn_account_id", req.CDNAccountID),
				zap.Error(err),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to bind cdn domain"})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// ListBindings handles GET /domains/:id/cdn-bindings.
// Returns all active CDN bindings for the domain.
func (h *CDNBindingHandler) ListBindings(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	bindings, err := h.svc.ListBindings(c.Request.Context(), domain.ID)
	if err != nil {
		h.logger.Error("list cdn bindings", zap.String("fqdn", domain.FQDN), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": bindings, "total": len(bindings)}, "message": "ok"})
}

// GetBinding handles GET /domains/:id/cdn-bindings/:bid.
// Returns the local snapshot of a specific CDN binding.
func (h *CDNBindingHandler) GetBinding(c *gin.Context) {
	_, ok := h.getDomain(c)
	if !ok {
		return
	}
	bid, ok := h.getBindingID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetBinding(c.Request.Context(), bid)
	if err != nil {
		if errors.Is(err, cdndomain.ErrBindingNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40403, "data": nil, "message": "cdn binding not found"})
			return
		}
		h.logger.Error("get cdn binding", zap.Int64("bid", bid), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// Unbind handles DELETE /domains/:id/cdn-bindings/:bid.
// Removes the domain from the CDN provider and soft-deletes the local binding.
func (h *CDNBindingHandler) Unbind(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}
	bid, ok := h.getBindingID(c)
	if !ok {
		return
	}

	if err := h.svc.UnbindDomain(c.Request.Context(), domain, bid); err != nil {
		switch {
		case errors.Is(err, cdndomain.ErrBindingNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": 40403, "data": nil, "message": "cdn binding not found"})
		case errors.Is(err, cdndomain.ErrNoCDNProvider):
			c.JSON(http.StatusConflict, gin.H{"code": 40901, "data": nil, "message": err.Error()})
		default:
			h.logger.Error("unbind cdn domain",
				zap.String("fqdn", domain.FQDN),
				zap.Int64("binding_id", bid),
				zap.Error(err),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to unbind cdn domain"})
		}
		return
	}

	c.Status(http.StatusNoContent)
}

// RefreshStatus handles POST /domains/:id/cdn-bindings/:bid/refresh.
// Polls the CDN provider for the current domain status and CNAME, then
// updates the local binding record.
func (h *CDNBindingHandler) RefreshStatus(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}
	bid, ok := h.getBindingID(c)
	if !ok {
		return
	}

	resp, err := h.svc.RefreshStatus(c.Request.Context(), domain, bid)
	if err != nil {
		switch {
		case errors.Is(err, cdndomain.ErrBindingNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": 40403, "data": nil, "message": "cdn binding not found"})
		case errors.Is(err, cdndomain.ErrNoCDNProvider):
			c.JSON(http.StatusConflict, gin.H{"code": 40901, "data": nil, "message": err.Error()})
		default:
			h.logger.Error("refresh cdn binding status",
				zap.String("fqdn", domain.FQDN),
				zap.Int64("binding_id", bid),
				zap.Error(err),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to refresh cdn status"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": "ok"})
}
