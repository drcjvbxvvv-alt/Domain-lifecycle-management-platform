package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/middleware"
	"domain-platform/internal/template"
	"domain-platform/store/postgres"
)

type TemplateHandler struct {
	svc    *template.Service
	logger *zap.Logger
}

func NewTemplateHandler(svc *template.Service, logger *zap.Logger) *TemplateHandler {
	return &TemplateHandler{svc: svc, logger: logger}
}

// ── Template handlers ─────────────────────────────────────────────────────────

type CreateTemplateRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
	Kind        string  `json:"kind"` // "html" | "nginx" | "full"; defaults to "full"
}

// Create handles POST /api/v1/projects/:projectId/templates
func (h *TemplateHandler) Create(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	var req CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: name required",
		})
		return
	}

	t, err := h.svc.Create(c.Request.Context(), template.CreateInput{
		ProjectID:   projectID,
		Name:        req.Name,
		Description: req.Description,
		Kind:        req.Kind,
	})
	if err != nil {
		h.logger.Error("create template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": templateResponse(t),
	})
}

// Get handles GET /api/v1/templates/:id
func (h *TemplateHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid template id",
		})
		return
	}

	t, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, template.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "template not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": templateResponse(t),
	})
}

// List handles GET /api/v1/projects/:projectId/templates
func (h *TemplateHandler) List(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	result, err := h.svc.List(c.Request.Context(), template.ListInput{
		ProjectID: projectID,
		Cursor:    cursor,
		Limit:     limit,
	})
	if err != nil {
		h.logger.Error("list templates", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	items := make([]gin.H, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, templateResponse(&result.Items[i]))
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

type UpdateTemplateRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
}

// Update handles PUT /api/v1/templates/:id
func (h *TemplateHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid template id",
		})
		return
	}

	var req UpdateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request",
		})
		return
	}

	t, err := h.svc.Update(c.Request.Context(), template.UpdateInput{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
	})
	if errors.Is(err, template.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "template not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("update template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": templateResponse(t),
	})
}

// Delete handles DELETE /api/v1/templates/:id
func (h *TemplateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid template id",
		})
		return
	}

	err = h.svc.Delete(c.Request.Context(), id)
	if errors.Is(err, template.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "template not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("delete template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// ── TemplateVersion handlers ──────────────────────────────────────────────────

type PublishVersionRequest struct {
	ContentHTML      *string        `json:"content_html"`
	ContentNginx     *string        `json:"content_nginx"`
	DefaultVariables map[string]any `json:"default_variables"`
}

// PublishVersion handles POST /api/v1/templates/:id/versions/publish
func (h *TemplateHandler) PublishVersion(c *gin.Context) {
	templateID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid template id",
		})
		return
	}

	var req PublishVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request",
		})
		return
	}

	userID := middleware.GetUserID(c)
	v, err := h.svc.PublishVersion(c.Request.Context(), template.PublishVersionInput{
		TemplateID:       templateID,
		ContentHTML:      req.ContentHTML,
		ContentNginx:     req.ContentNginx,
		DefaultVariables: req.DefaultVariables,
		PublishedBy:      userID,
	})
	if errors.Is(err, template.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "template not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("publish template version", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": versionResponse(v),
	})
}

// ListVersions handles GET /api/v1/templates/:id/versions
func (h *TemplateHandler) ListVersions(c *gin.Context) {
	templateID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid template id",
		})
		return
	}

	versions, err := h.svc.ListVersions(c.Request.Context(), templateID)
	if err != nil {
		h.logger.Error("list template versions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	items := make([]gin.H, 0, len(versions))
	for i := range versions {
		items = append(items, versionResponse(&versions[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": items,
	})
}

// GetVersion handles GET /api/v1/template-versions/:id
func (h *TemplateHandler) GetVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid version id",
		})
		return
	}

	v, err := h.svc.GetVersion(c.Request.Context(), id)
	if errors.Is(err, template.ErrTemplateVersionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "template version not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get template version", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": versionResponse(v),
	})
}

// UpdateVersion handles PATCH /api/v1/template-versions/:id
// Returns 409 if the version is already published (immutable).
func (h *TemplateHandler) UpdateVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid version id",
		})
		return
	}

	var req struct {
		ContentHTML      *string        `json:"content_html"`
		ContentNginx     *string        `json:"content_nginx"`
		DefaultVariables map[string]any `json:"default_variables"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request",
		})
		return
	}

	v, err := h.svc.UpdateVersion(c.Request.Context(), template.UpdateVersionInput{
		ID:               id,
		ContentHTML:      req.ContentHTML,
		ContentNginx:     req.ContentNginx,
		DefaultVariables: req.DefaultVariables,
	})
	if errors.Is(err, template.ErrTemplateVersionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "template version not found",
		})
		return
	}
	if errors.Is(err, template.ErrVersionImmutable) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40900, "data": nil, "message": "template version is published and immutable",
		})
		return
	}
	if err != nil {
		h.logger.Error("update template version", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": versionResponse(v),
	})
}

// ── Response helpers ──────────────────────────────────────────────────────────

func templateResponse(t *postgres.Template) gin.H {
	return gin.H{
		"id":          t.ID,
		"uuid":        t.UUID,
		"project_id":  t.ProjectID,
		"name":        t.Name,
		"description": t.Description,
		"kind":        t.Kind,
		"created_at":  t.CreatedAt,
		"updated_at":  t.UpdatedAt,
	}
}

func versionResponse(v *postgres.TemplateVersion) gin.H {
	return gin.H{
		"id":                v.ID,
		"uuid":              v.UUID,
		"template_id":       v.TemplateID,
		"version_label":     v.VersionLabel,
		"content_html":      v.ContentHTML,
		"content_nginx":     v.ContentNginx,
		"default_variables": v.DefaultVariables,
		"checksum":          v.Checksum,
		"published_at":      v.PublishedAt,
		"published_by":      v.PublishedBy,
		"created_at":        v.CreatedAt,
		"created_by":        v.CreatedBy,
	}
}
