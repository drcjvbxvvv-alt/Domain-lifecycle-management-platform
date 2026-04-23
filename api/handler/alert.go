package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/alert"
	"domain-platform/store/postgres"
)

// AlertHandler handles REST endpoints for alert events and notification rules.
type AlertHandler struct {
	store  *postgres.AlertStore
	engine *alert.Engine
	logger *zap.Logger
}

func NewAlertHandler(store *postgres.AlertStore, engine *alert.Engine, logger *zap.Logger) *AlertHandler {
	return &AlertHandler{store: store, engine: engine, logger: logger}
}

// ── Alert Events ──────────────────────────────────────────────────────────────

// ListAlerts GET /alerts
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	f := postgres.AlertListFilter{}

	f.Severity = c.Query("severity")
	f.Source = c.Query("source")
	f.TargetKind = c.Query("target_kind")

	if tid := c.Query("target_id"); tid != "" {
		v, err := strconv.ParseInt(tid, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid target_id"})
			return
		}
		f.TargetID = &v
	}
	if c.Query("unresolved") == "true" {
		f.Unresolved = true
	}
	if l := c.Query("limit"); l != "" {
		v, _ := strconv.Atoi(l)
		if v > 0 {
			f.Limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		v, _ := strconv.Atoi(o)
		if v >= 0 {
			f.Offset = v
		}
	}

	items, err := h.store.List(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("list alerts", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	resp := make([]gin.H, len(items))
	for i := range items {
		resp[i] = alertEventResponse(&items[i])
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": resp, "total": len(resp)}})
}

// GetAlert GET /alerts/:id
func (h *AlertHandler) GetAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}
	ev, err := h.store.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrAlertNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "alert not found"})
			return
		}
		h.logger.Error("get alert", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": alertEventResponse(ev)})
}

// ResolveAlert POST /alerts/:id/resolve
func (h *AlertHandler) ResolveAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}
	if err := h.store.Resolve(c.Request.Context(), id); err != nil {
		if errors.Is(err, postgres.ErrAlertNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "alert not found or already resolved"})
			return
		}
		h.logger.Error("resolve alert", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// AcknowledgeAlert POST /alerts/:id/acknowledge
func (h *AlertHandler) AcknowledgeAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	// Extract authenticated user ID from JWT context.
	userIDAny, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 40100, "data": nil, "message": "unauthorized"})
		return
	}
	userID, ok := userIDAny.(int64)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	if err := h.store.Acknowledge(c.Request.Context(), id, userID); err != nil {
		if errors.Is(err, postgres.ErrAlertNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "alert not found or already acknowledged"})
			return
		}
		h.logger.Error("acknowledge alert", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// AlertSummary GET /alerts/summary
func (h *AlertHandler) AlertSummary(c *gin.Context) {
	counts, err := h.store.CountUnresolved(c.Request.Context())
	if err != nil {
		h.logger.Error("alert summary", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"unresolved": counts}})
}

// ── Notification Rules ────────────────────────────────────────────────────────

// ListRules GET /notification-rules
func (h *AlertHandler) ListRules(c *gin.Context) {
	rules, err := h.store.ListAllRules(c.Request.Context())
	if err != nil {
		h.logger.Error("list notification rules", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	items := make([]gin.H, len(rules))
	for i := range rules {
		items[i] = notificationRuleResponse(&rules[i])
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": items, "total": len(items)}})
}

// GetRule GET /notification-rules/:id
func (h *AlertHandler) GetRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}
	r, err := h.store.GetRule(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrNotificationRuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "notification rule not found"})
			return
		}
		h.logger.Error("get notification rule", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": notificationRuleResponse(r)})
}

// CreateRule POST /notification-rules
func (h *AlertHandler) CreateRule(c *gin.Context) {
	var req struct {
		Name           string          `json:"name" binding:"required"`
		ProjectID      *int64          `json:"project_id"`
		SeverityFilter *string         `json:"severity_filter"`
		TargetKind     *string         `json:"target_kind"`
		Channel        string          `json:"channel" binding:"required"`
		Config         json.RawMessage `json:"config" binding:"required"`
		Enabled        bool            `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": err.Error()})
		return
	}

	r := &postgres.NotificationRule{
		Name:           req.Name,
		ProjectID:      req.ProjectID,
		SeverityFilter: req.SeverityFilter,
		TargetKind:     req.TargetKind,
		Channel:        req.Channel,
		Config:         req.Config,
		Enabled:        req.Enabled,
	}
	if err := h.store.CreateRule(c.Request.Context(), r); err != nil {
		h.logger.Error("create notification rule", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "message": "ok", "data": notificationRuleResponse(r)})
}

// UpdateRule PUT /notification-rules/:id
func (h *AlertHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}

	existing, err := h.store.GetRule(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrNotificationRuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "notification rule not found"})
			return
		}
		h.logger.Error("get notification rule for update", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	var req struct {
		Name           *string         `json:"name"`
		SeverityFilter *string         `json:"severity_filter"`
		TargetKind     *string         `json:"target_kind"`
		Channel        *string         `json:"channel"`
		Config         json.RawMessage `json:"config"`
		Enabled        *bool           `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": err.Error()})
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.SeverityFilter != nil {
		existing.SeverityFilter = req.SeverityFilter
	}
	if req.TargetKind != nil {
		existing.TargetKind = req.TargetKind
	}
	if req.Channel != nil {
		existing.Channel = *req.Channel
	}
	if len(req.Config) > 0 {
		existing.Config = req.Config
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.store.UpdateRule(c.Request.Context(), existing); err != nil {
		h.logger.Error("update notification rule", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": notificationRuleResponse(existing)})
}

// DeleteRule DELETE /notification-rules/:id
func (h *AlertHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid id"})
		return
	}
	if err := h.store.DeleteRule(c.Request.Context(), id); err != nil {
		if errors.Is(err, postgres.ErrNotificationRuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "notification rule not found"})
			return
		}
		h.logger.Error("delete notification rule", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Response builders ─────────────────────────────────────────────────────────

func alertEventResponse(ev *postgres.AlertEvent) gin.H {
	return gin.H{
		"id":              ev.ID,
		"uuid":            ev.UUID,
		"severity":        ev.Severity,
		"source":          ev.Source,
		"target_kind":     ev.TargetKind,
		"target_id":       ev.TargetID,
		"title":           ev.Title,
		"detail":          ev.Detail,
		"dedup_key":       ev.DedupKey,
		"notified_at":     ev.NotifiedAt,
		"resolved_at":     ev.ResolvedAt,
		"acknowledged_at": ev.AcknowledgedAt,
		"acknowledged_by": ev.AcknowledgedBy,
		"created_at":      ev.CreatedAt,
	}
}

func notificationRuleResponse(r *postgres.NotificationRule) gin.H {
	return gin.H{
		"id":              r.ID,
		"uuid":            r.UUID,
		"name":            r.Name,
		"project_id":      r.ProjectID,
		"severity_filter": r.SeverityFilter,
		"target_kind":     r.TargetKind,
		"channel":         r.Channel,
		"config":          r.Config,
		"enabled":         r.Enabled,
		"created_at":      r.CreatedAt,
	}
}
