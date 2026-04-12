package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/middleware"
	"domain-platform/internal/release"
	"domain-platform/store/postgres"
)

// ReleaseHandler serves release endpoints.
type ReleaseHandler struct {
	svc    *release.Service
	logger *zap.Logger
}

func NewReleaseHandler(svc *release.Service, logger *zap.Logger) *ReleaseHandler {
	return &ReleaseHandler{svc: svc, logger: logger}
}

// ── Create ──────────────────────────────────────────────────────────────────

type CreateReleaseRequest struct {
	ProjectID         int64   `json:"project_id" binding:"required"`
	ProjectSlug       string  `json:"project_slug" binding:"required"`
	TemplateVersionID int64   `json:"template_version_id" binding:"required"`
	ReleaseType       string  `json:"release_type"`    // "html" | "nginx" | "full"; defaults to "html"
	TriggerSource     string  `json:"trigger_source"`  // "ui" | "api" | "ci"; defaults to "ui"
	Description       *string `json:"description"`
	DomainIDs         []int64 `json:"domain_ids"`      // explicit scope; empty = all active
}

// Create handles POST /api/v1/releases — creates a release and returns 202.
func (h *ReleaseHandler) Create(c *gin.Context) {
	var req CreateReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: project_id, project_slug, template_version_id required",
		})
		return
	}

	userID := middleware.GetUserID(c)
	rel, err := h.svc.Create(c.Request.Context(), release.CreateInput{
		ProjectID:         req.ProjectID,
		ProjectSlug:       req.ProjectSlug,
		TemplateVersionID: req.TemplateVersionID,
		ReleaseType:       req.ReleaseType,
		TriggerSource:     req.TriggerSource,
		Description:       req.Description,
		DomainIDs:         req.DomainIDs,
		CreatedBy:         &userID,
	})
	if errors.Is(err, release.ErrDomainNotActive) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": err.Error(),
		})
		return
	}
	if errors.Is(err, release.ErrNoDomainsInScope) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40002, "data": nil, "message": "no active domains in scope",
		})
		return
	}
	if errors.Is(err, release.ErrTemplateNotPublished) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40003, "data": nil, "message": "template version not published",
		})
		return
	}
	if err != nil {
		h.logger.Error("create release", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"code": 0, "message": "ok", "data": releaseResponse(rel),
	})
}

// ── Get ─────────────────────────────────────────────────────────────────────

// Get handles GET /api/v1/releases/:id
func (h *ReleaseHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid release id",
		})
		return
	}

	rel, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, release.ErrReleaseNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "release not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get release", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": releaseResponse(rel),
	})
}

// ── List ────────────────────────────────────────────────────────────────────

// ListByProject handles GET /api/v1/projects/:projectId/releases
func (h *ReleaseHandler) ListByProject(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	result, err := h.svc.List(c.Request.Context(), release.ListInput{
		ProjectID: projectID,
		Cursor:    cursor,
		Limit:     limit,
	})
	if err != nil {
		h.logger.Error("list releases", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	items := make([]gin.H, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, releaseResponse(&result.Items[i]))
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

// ── Actions ─────────────────────────────────────────────────────────────────

// Pause handles POST /api/v1/releases/:id/pause
func (h *ReleaseHandler) Pause(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid release id"})
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)

	userID := middleware.GetUserID(c)
	triggeredBy := triggeredByStr(userID)

	if err := h.svc.Pause(c.Request.Context(), id, req.Reason, triggeredBy); err != nil {
		if errors.Is(err, release.ErrInvalidReleaseState) || errors.Is(err, release.ErrReleaseRaceCondition) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": err.Error()})
			return
		}
		h.logger.Error("pause release", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// Resume handles POST /api/v1/releases/:id/resume
func (h *ReleaseHandler) Resume(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid release id"})
		return
	}

	userID := middleware.GetUserID(c)
	triggeredBy := triggeredByStr(userID)

	if err := h.svc.Resume(c.Request.Context(), id, triggeredBy); err != nil {
		if errors.Is(err, release.ErrInvalidReleaseState) || errors.Is(err, release.ErrReleaseRaceCondition) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": err.Error()})
			return
		}
		h.logger.Error("resume release", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// Cancel handles POST /api/v1/releases/:id/cancel
func (h *ReleaseHandler) Cancel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid release id"})
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)

	userID := middleware.GetUserID(c)
	triggeredBy := triggeredByStr(userID)

	if err := h.svc.Cancel(c.Request.Context(), id, req.Reason, triggeredBy); err != nil {
		if errors.Is(err, release.ErrInvalidReleaseState) || errors.Is(err, release.ErrReleaseRaceCondition) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": err.Error()})
			return
		}
		h.logger.Error("cancel release", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// History handles GET /api/v1/releases/:id/history
func (h *ReleaseHandler) History(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid release id"})
		return
	}

	rows, err := h.svc.GetHistory(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("get release history", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, gin.H{
			"id":           r.ID,
			"from_state":   r.FromState,
			"to_state":     r.ToState,
			"reason":       r.Reason,
			"triggered_by": r.TriggeredBy,
			"created_at":   r.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": items})
}

// ── Response helpers ────────────────────────────────────────────────────────

func releaseResponse(r *postgres.Release) gin.H {
	return gin.H{
		"id":                  r.ID,
		"uuid":                r.UUID,
		"release_id":          r.ReleaseID,
		"project_id":          r.ProjectID,
		"template_version_id": r.TemplateVersionID,
		"artifact_id":         r.ArtifactID,
		"release_type":        r.ReleaseType,
		"trigger_source":      r.TriggerSource,
		"status":              r.Status,
		"requires_approval":   r.RequiresApproval,
		"total_domains":       r.TotalDomains,
		"total_shards":        r.TotalShards,
		"success_count":       r.SuccessCount,
		"failure_count":       r.FailureCount,
		"description":         r.Description,
		"created_at":          r.CreatedAt,
		"created_by":          r.CreatedBy,
		"started_at":          r.StartedAt,
		"ended_at":            r.EndedAt,
	}
}

func triggeredByStr(userID int64) string {
	return "user:" + strconv.FormatInt(userID, 10)
}
