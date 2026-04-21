package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

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

type RegisterDomainRequest struct {
	ProjectID     int64  `json:"project_id" binding:"required"`
	FQDN          string `json:"fqdn" binding:"required"`
	DNSProviderID *int64 `json:"dns_provider_id"`
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
	d, err := h.svc.Register(c.Request.Context(), lifecycle.RegisterInput{
		ProjectID:     req.ProjectID,
		FQDN:          req.FQDN,
		OwnerUserID:   &userID,
		DNSProviderID: req.DNSProviderID,
		TriggeredBy:   fmt.Sprintf("user:%d", userID),
	})
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

	// Read current state to provide as `from`
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

	// Re-read to return the updated domain
	updated, _ := h.svc.GetByID(c.Request.Context(), id)
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": domainResponse(updated),
	})
}

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

// List handles GET /api/v1/domains?project_id=X
func (h *DomainHandler) List(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Query("project_id"), 10, 64)
	if err != nil || projectID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "project_id query parameter required",
		})
		return
	}

	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	result, err := h.svc.List(c.Request.Context(), lifecycle.ListInput{
		ProjectID: projectID,
		Cursor:    cursor,
		Limit:     limit,
	})
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

func domainResponse(d *postgres.Domain) gin.H {
	return gin.H{
		"id":                   d.ID,
		"uuid":                 d.UUID,
		"project_id":           d.ProjectID,
		"fqdn":                 d.FQDN,
		"lifecycle_state":      d.LifecycleState,
		"owner_user_id":        d.OwnerUserID,
		"tld":                  d.TLD,
		"registrar_account_id": d.RegistrarAccountID,
		"dns_provider_id":      d.DNSProviderID,
		"expiry_date":          d.ExpiryDate,
		"auto_renew":           d.AutoRenew,
		"annual_cost":          d.AnnualCost,
		"currency":             d.Currency,
		"created_at":           d.CreatedAt,
		"updated_at":           d.UpdatedAt,
	}
}
