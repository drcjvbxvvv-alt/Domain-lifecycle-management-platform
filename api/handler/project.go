package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/middleware"
	"domain-platform/internal/project"
	"domain-platform/store/postgres"
)

type ProjectHandler struct {
	svc    *project.Service
	logger *zap.Logger
}

func NewProjectHandler(svc *project.Service, logger *zap.Logger) *ProjectHandler {
	return &ProjectHandler{svc: svc, logger: logger}
}

type CreateProjectRequest struct {
	Name        string  `json:"name" binding:"required"`
	Slug        string  `json:"slug" binding:"required"`
	Description *string `json:"description"`
	IsProd      bool    `json:"is_prod"`
}

// Create handles POST /api/v1/projects
func (h *ProjectHandler) Create(c *gin.Context) {
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request",
		})
		return
	}

	userID := middleware.GetUserID(c)
	p, err := h.svc.Create(c.Request.Context(), project.CreateInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		IsProd:      req.IsProd,
		OwnerID:     &userID,
	})
	if errors.Is(err, project.ErrDuplicateSlug) || errors.Is(err, project.ErrDuplicateName) {
		c.JSON(http.StatusConflict, gin.H{
			"code": 40900, "data": nil, "message": err.Error(),
		})
		return
	}
	if errors.Is(err, project.ErrInvalidSlug) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid slug: must match ^[a-z0-9][a-z0-9-]{1,98}[a-z0-9]$",
		})
		return
	}
	if err != nil {
		h.logger.Error("create project", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "message": "ok", "data": projectResponse(p),
	})
}

// Get handles GET /api/v1/projects/:id
func (h *ProjectHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	p, err := h.svc.GetByID(c.Request.Context(), id)
	if errors.Is(err, postgres.ErrProjectNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "project not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get project", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": projectResponse(p),
	})
}

// List handles GET /api/v1/projects
func (h *ProjectHandler) List(c *gin.Context) {
	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	result, err := h.svc.List(c.Request.Context(), project.ListInput{
		Cursor: cursor,
		Limit:  limit,
	})
	if err != nil {
		h.logger.Error("list projects", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	items := make([]gin.H, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, projectResponse(&result.Items[i]))
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

type UpdateProjectRequest struct {
	Name        string  `json:"name" binding:"required"`
	Slug        string  `json:"slug" binding:"required"`
	Description *string `json:"description"`
	IsProd      bool    `json:"is_prod"`
}

// Update handles PUT /api/v1/projects/:id
func (h *ProjectHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	var req UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request",
		})
		return
	}

	p, err := h.svc.Update(c.Request.Context(), project.UpdateInput{
		ID:          id,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		IsProd:      req.IsProd,
	})
	if errors.Is(err, postgres.ErrProjectNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "project not found",
		})
		return
	}
	if errors.Is(err, project.ErrInvalidSlug) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid slug",
		})
		return
	}
	if err != nil {
		h.logger.Error("update project", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": projectResponse(p),
	})
}

// Delete handles DELETE /api/v1/projects/:id
func (h *ProjectHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	err = h.svc.Delete(c.Request.Context(), id)
	if errors.Is(err, postgres.ErrProjectNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "project not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("delete project", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func projectResponse(p *postgres.Project) gin.H {
	return gin.H{
		"id":          p.ID,
		"uuid":        p.UUID,
		"name":        p.Name,
		"slug":        p.Slug,
		"description": p.Description,
		"is_prod":     p.IsProd,
		"owner_id":    p.OwnerID,
		"created_at":  p.CreatedAt,
		"updated_at":  p.UpdatedAt,
	}
}
