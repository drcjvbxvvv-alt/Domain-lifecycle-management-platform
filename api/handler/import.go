package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	importsvc "domain-platform/internal/importer"
	"domain-platform/store/postgres"
)

// ImportHandler handles bulk domain CSV import requests.
type ImportHandler struct {
	svc    *importsvc.Service
	logger *zap.Logger
}

// NewImportHandler returns an ImportHandler.
func NewImportHandler(svc *importsvc.Service, logger *zap.Logger) *ImportHandler {
	return &ImportHandler{svc: svc, logger: logger}
}

// ── Response types ────────────────────────────────────────────────────────────

type importJobResponse struct {
	ID                 int64   `json:"id"`
	UUID               string  `json:"uuid"`
	ProjectID          int64   `json:"project_id"`
	RegistrarAccountID *int64  `json:"registrar_account_id,omitempty"`
	SourceType         string  `json:"source_type"`
	Status             string  `json:"status"`
	TotalCount         int     `json:"total_count"`
	ImportedCount      int     `json:"imported_count"`
	SkippedCount       int     `json:"skipped_count"`
	FailedCount        int     `json:"failed_count"`
	ErrorDetails       *string `json:"error_details,omitempty"`
	CreatedBy          *int64  `json:"created_by,omitempty"`
	StartedAt          *string `json:"started_at,omitempty"`
	CompletedAt        *string `json:"completed_at,omitempty"`
	CreatedAt          string  `json:"created_at"`
}

func toImportJobResponse(j *postgres.DomainImportJob) importJobResponse {
	r := importJobResponse{
		ID:                 j.ID,
		UUID:               j.UUID,
		ProjectID:          j.ProjectID,
		RegistrarAccountID: j.RegistrarAccountID,
		SourceType:         j.SourceType,
		Status:             j.Status,
		TotalCount:         j.TotalCount,
		ImportedCount:      j.ImportedCount,
		SkippedCount:       j.SkippedCount,
		FailedCount:        j.FailedCount,
		ErrorDetails:       j.ErrorDetails,
		CreatedBy:          j.CreatedBy,
		CreatedAt:          j.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if j.StartedAt != nil {
		s := j.StartedAt.Format("2006-01-02T15:04:05Z")
		r.StartedAt = &s
	}
	if j.CompletedAt != nil {
		s := j.CompletedAt.Format("2006-01-02T15:04:05Z")
		r.CompletedAt = &s
	}
	return r
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Upload handles POST /api/v1/domains/import
// Accepts multipart/form-data with fields:
//   - csv_file (file): the CSV file to import
//   - project_id (form): target project ID
//   - registrar_account_id (form, optional): default registrar account for all rows
func (h *ImportHandler) Upload(c *gin.Context) {
	// project_id is required
	projectIDStr := c.PostForm("project_id")
	if projectIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "project_id is required"})
		return
	}
	projectID, err := strconv.ParseInt(projectIDStr, 10, 64)
	if err != nil || projectID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "data": nil, "message": "invalid project_id"})
		return
	}

	// optional registrar_account_id
	var registrarAccountID *int64
	if raw := c.PostForm("registrar_account_id"); raw != "" {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40003, "data": nil, "message": "invalid registrar_account_id"})
			return
		}
		registrarAccountID = &id
	}

	// Read CSV file from multipart
	file, _, err := c.Request.FormFile("csv_file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40004, "data": nil, "message": "csv_file is required"})
		return
	}
	defer file.Close()

	// Limit to 10 MB to avoid memory exhaustion
	const maxSize = 10 << 20 // 10 MB
	limited := io.LimitReader(file, maxSize+1)
	raw, readErr := io.ReadAll(limited)
	if readErr != nil {
		h.logger.Error("read csv upload", zap.Error(readErr))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "failed to read file"})
		return
	}
	if len(raw) > maxSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 41301, "data": nil, "message": "CSV file exceeds 10 MB limit"})
		return
	}

	// Extract caller identity from JWT context (set by JWTAuth middleware)
	var createdBy *int64
	if uid, ok := c.Get("userID"); ok {
		if id, ok2 := uid.(int64); ok2 {
			createdBy = &id
		}
	}

	job, svcErr := h.svc.CreateJob(c.Request.Context(), importsvc.CreateJobInput{
		ProjectID:          projectID,
		RegistrarAccountID: registrarAccountID,
		RawCSV:             string(raw),
		CreatedBy:          createdBy,
	})
	if svcErr != nil {
		switch {
		case errors.Is(svcErr, importsvc.ErrEmptyCSV):
			c.JSON(http.StatusBadRequest, gin.H{"code": 40005, "data": nil, "message": svcErr.Error()})
		case errors.Is(svcErr, importsvc.ErrTooManyRows):
			c.JSON(http.StatusBadRequest, gin.H{"code": 40006, "data": nil, "message": svcErr.Error()})
		case errors.Is(svcErr, importsvc.ErrInvalidHeader):
			c.JSON(http.StatusBadRequest, gin.H{"code": 40007, "data": nil, "message": svcErr.Error()})
		default:
			h.logger.Error("create import job", zap.Error(svcErr))
			c.JSON(http.StatusInternalServerError, gin.H{"code": 50002, "data": nil, "message": "failed to create import job"})
		}
		return
	}

	resp := toImportJobResponse(job)
	c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// Preview handles POST /api/v1/domains/import/preview
// Parses the CSV and returns the parsed rows + errors without creating a job.
func (h *ImportHandler) Preview(c *gin.Context) {
	file, _, err := c.Request.FormFile("csv_file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40004, "data": nil, "message": "csv_file is required"})
		return
	}
	defer file.Close()

	const maxSize = 10 << 20
	raw, readErr := io.ReadAll(io.LimitReader(file, maxSize+1))
	if readErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "failed to read file"})
		return
	}
	if len(raw) > maxSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 41301, "data": nil, "message": "CSV file exceeds 10 MB limit"})
		return
	}

	result, parseErr := importsvc.ParseCSV(string(raw))
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40008, "data": nil, "message": parseErr.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"valid_count": len(result.Rows),
			"error_count": len(result.Errors),
			"rows":        result.Rows,
			"errors":      result.Errors,
		},
		"message": "ok",
	})
}

// GetJob handles GET /api/v1/domains/import/jobs/:jobid
func (h *ImportHandler) GetJob(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("jobid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid job id"})
		return
	}
	job, svcErr := h.svc.GetJob(c.Request.Context(), id)
	if svcErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "import job not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": toImportJobResponse(job), "message": "ok"})
}

// ListJobs handles GET /api/v1/domains/import/jobs
// Query params: project_id (optional), limit (default 50), offset (default 0)
func (h *ImportHandler) ListJobs(c *gin.Context) {
	var projectID int64
	if raw := c.Query("project_id"); raw != "" {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid project_id"})
			return
		}
		projectID = id
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	jobs, svcErr := h.svc.ListJobs(c.Request.Context(), projectID, limit, offset)
	if svcErr != nil {
		h.logger.Error("list import jobs", zap.Error(svcErr))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "failed to list import jobs"})
		return
	}

	items := make([]importJobResponse, len(jobs))
	for i := range jobs {
		items[i] = toImportJobResponse(&jobs[i])
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": len(items)}, "message": "ok"})
}
