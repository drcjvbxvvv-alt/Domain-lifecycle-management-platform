package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/middleware"
	"domain-platform/internal/domain"
	"domain-platform/store/postgres"
)

// DomainPermissionHandler handles zone-level RBAC endpoints.
type DomainPermissionHandler struct {
	svc    *domain.PermissionService
	logger *zap.Logger
}

// NewDomainPermissionHandler constructs a DomainPermissionHandler.
func NewDomainPermissionHandler(svc *domain.PermissionService, logger *zap.Logger) *DomainPermissionHandler {
	return &DomainPermissionHandler{svc: svc, logger: logger}
}

// ── Response types ─────────────────────────────────────────────────────────

type DomainPermissionResponse struct {
	ID          int64   `json:"id"`
	DomainID    int64   `json:"domain_id"`
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name,omitempty"`
	Permission  string  `json:"permission"`
	GrantedBy   *int64  `json:"granted_by,omitempty"`
	GrantedAt   string  `json:"granted_at"`
}

func toPermissionResponse(p postgres.DomainPermissionWithUser) DomainPermissionResponse {
	return DomainPermissionResponse{
		ID:          p.ID,
		DomainID:    p.DomainID,
		UserID:      p.UserID,
		Username:    p.Username,
		DisplayName: p.DisplayName,
		Permission:  p.Permission,
		GrantedBy:   p.GrantedBy,
		GrantedAt:   p.GrantedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ── List ──────────────────────────────────────────────────────────────────

// List handles GET /api/v1/domains/:id/permissions
func (h *DomainPermissionHandler) List(c *gin.Context) {
	domainID, err := parseDomainID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	rows, err := h.svc.ListPermissions(c.Request.Context(), domainID)
	if err != nil {
		h.logger.Error("list domain permissions", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to list permissions"})
		return
	}

	items := make([]DomainPermissionResponse, len(rows))
	for i, r := range rows {
		items[i] = toPermissionResponse(r)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": len(items)}, "message": "ok"})
}

// ── Grant ─────────────────────────────────────────────────────────────────

type GrantPermissionRequest struct {
	UserID     int64  `json:"user_id"    binding:"required"`
	Permission string `json:"permission" binding:"required"`
}

// Grant handles POST /api/v1/domains/:id/permissions
func (h *DomainPermissionHandler) Grant(c *gin.Context) {
	domainID, err := parseDomainID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	var req GrantPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "user_id and permission required"})
		return
	}

	grantedBy := middleware.GetUserID(c)
	if err := h.svc.GrantPermission(c.Request.Context(), domainID, req.UserID, req.Permission, grantedBy); err != nil {
		h.logger.Warn("grant domain permission", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "permission granted"})
}

// ── Revoke ────────────────────────────────────────────────────────────────

// Revoke handles DELETE /api/v1/domains/:id/permissions/:user_id
func (h *DomainPermissionHandler) Revoke(c *gin.Context) {
	domainID, err := parseDomainID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	rawUID := c.Param("user_id")
	userID, err := strconv.ParseInt(rawUID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid user id"})
		return
	}

	if err := h.svc.RevokePermission(c.Request.Context(), domainID, userID); err != nil {
		if errors.Is(err, postgres.ErrDomainPermissionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "permission not found"})
			return
		}
		h.logger.Error("revoke domain permission", zap.Int64("domain_id", domainID), zap.Int64("user_id", userID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to revoke permission"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "permission revoked"})
}

// ── My Permission ─────────────────────────────────────────────────────────

// MyPermission handles GET /api/v1/domains/:id/my-permission
// Returns the caller's effective permission level on this domain.
func (h *DomainPermissionHandler) MyPermission(c *gin.Context) {
	domainID, err := parseDomainID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	userID := middleware.GetUserID(c)
	eff, err := h.svc.EffectivePermission(c.Request.Context(), domainID, userID)
	if err != nil {
		h.logger.Error("my-permission", zap.Int64("domain_id", domainID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to check permission"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"permission": eff}, "message": "ok"})
}

// ── helpers ───────────────────────────────────────────────────────────────

func parseDomainID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}
