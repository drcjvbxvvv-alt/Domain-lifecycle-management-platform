package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// ArtifactHandler serves read-only artifact endpoints.
// Artifacts are created by the release flow (via asynq), not directly by users.
type ArtifactHandler struct {
	store  *postgres.ArtifactStore
	logger *zap.Logger
}

func NewArtifactHandler(store *postgres.ArtifactStore, logger *zap.Logger) *ArtifactHandler {
	return &ArtifactHandler{store: store, logger: logger}
}

// Get handles GET /api/v1/artifacts/:id
func (h *ArtifactHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid artifact id",
		})
		return
	}

	a, err := h.store.GetByID(c.Request.Context(), id)
	if errors.Is(err, postgres.ErrArtifactNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "artifact not found",
		})
		return
	}
	if err != nil {
		h.logger.Error("get artifact", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok", "data": artifactResponse(a),
	})
}

// ListByProject handles GET /api/v1/projects/:projectId/artifacts
func (h *ArtifactHandler) ListByProject(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid project id",
		})
		return
	}

	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	items, err := h.store.ListByProject(c.Request.Context(), projectID, cursor, limit)
	if err != nil {
		h.logger.Error("list artifacts", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	resp := make([]gin.H, 0, len(items))
	for i := range items {
		resp = append(resp, artifactResponse(&items[i]))
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok",
		"data": gin.H{
			"items":  resp,
			"total":  len(resp),
			"cursor": nextCursor,
		},
	})
}

func artifactResponse(a *postgres.Artifact) gin.H {
	return gin.H{
		"id":                  a.ID,
		"uuid":                a.UUID,
		"project_id":          a.ProjectID,
		"release_id":          a.ReleaseID,
		"template_version_id": a.TemplateVersionID,
		"artifact_id":         a.ArtifactID,
		"storage_uri":         a.StorageURI,
		"checksum":            a.Checksum,
		"signature":           a.Signature,
		"domain_count":        a.DomainCount,
		"file_count":          a.FileCount,
		"total_size_bytes":    a.TotalSizeBytes,
		"built_at":            a.BuiltAt,
		"built_by":            a.BuiltBy,
		"signed_at":           a.SignedAt,
	}
}
