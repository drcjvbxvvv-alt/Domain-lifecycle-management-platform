package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/dnstemplate"
	"domain-platform/store/postgres"
)

// DNSTemplateHandler handles DNS record template CRUD + apply-template.
type DNSTemplateHandler struct {
	svc    *dnstemplate.Service
	logger *zap.Logger
}

// NewDNSTemplateHandler constructs a DNSTemplateHandler.
func NewDNSTemplateHandler(svc *dnstemplate.Service, logger *zap.Logger) *DNSTemplateHandler {
	return &DNSTemplateHandler{svc: svc, logger: logger}
}

// ── Response types ─────────────────────────────────────────────────────────

type DNSTemplateResponse struct {
	ID          int64                    `json:"id"`
	UUID        string                   `json:"uuid"`
	Name        string                   `json:"name"`
	Description *string                  `json:"description,omitempty"`
	Records     []postgres.TemplateRecord `json:"records"`
	Variables   map[string]string        `json:"variables"`
	RecordCount int                      `json:"record_count"`
	CreatedAt   string                   `json:"created_at"`
	UpdatedAt   string                   `json:"updated_at"`
}

func toTemplateResponse(t *postgres.DNSRecordTemplate) (DNSTemplateResponse, error) {
	var records []postgres.TemplateRecord
	if err := json.Unmarshal(t.Records, &records); err != nil {
		return DNSTemplateResponse{}, err
	}
	var variables map[string]string
	if err := json.Unmarshal(t.Variables, &variables); err != nil {
		variables = map[string]string{}
	}
	return DNSTemplateResponse{
		ID:          t.ID,
		UUID:        t.UUID,
		Name:        t.Name,
		Description: t.Description,
		Records:     records,
		Variables:   variables,
		RecordCount: len(records),
		CreatedAt:   t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// ── List ──────────────────────────────────────────────────────────────────

// List handles GET /api/v1/dns-templates
func (h *DNSTemplateHandler) List(c *gin.Context) {
	templates, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.logger.Error("list dns templates", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to list templates"})
		return
	}

	items := make([]DNSTemplateResponse, 0, len(templates))
	for i := range templates {
		resp, err := toTemplateResponse(&templates[i])
		if err != nil {
			continue
		}
		items = append(items, resp)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": len(items)}, "message": "ok"})
}

// ── Get ───────────────────────────────────────────────────────────────────

// Get handles GET /api/v1/dns-templates/:id
func (h *DNSTemplateHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid template id"})
		return
	}

	t, err := h.svc.Get(c.Request.Context(), id)
	if errors.Is(err, dnstemplate.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "template not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to get template"})
		return
	}

	resp, _ := toTemplateResponse(t)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// ── Create ────────────────────────────────────────────────────────────────

type TemplateRecordRequest struct {
	Name     string `json:"name"     binding:"required"`
	Type     string `json:"type"     binding:"required"`
	Content  string `json:"content"  binding:"required"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

type CreateDNSTemplateRequest struct {
	Name        string                  `json:"name"        binding:"required"`
	Description *string                 `json:"description"`
	Records     []TemplateRecordRequest `json:"records"     binding:"required"`
	Variables   map[string]string       `json:"variables"`
}

// Create handles POST /api/v1/dns-templates
func (h *DNSTemplateHandler) Create(c *gin.Context) {
	var req CreateDNSTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "name and records are required"})
		return
	}

	records := make([]postgres.TemplateRecord, len(req.Records))
	for i, r := range req.Records {
		records[i] = postgres.TemplateRecord{
			Name: r.Name, Type: r.Type, Content: r.Content, TTL: r.TTL, Priority: r.Priority,
		}
	}

	t, err := h.svc.Create(c.Request.Context(), dnstemplate.CreateInput{
		Name:        req.Name,
		Description: req.Description,
		Records:     records,
		Variables:   req.Variables,
	})
	if errors.Is(err, dnstemplate.ErrInvalidTemplate) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": err.Error()})
		return
	}
	if err != nil {
		h.logger.Error("create dns template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to create template"})
		return
	}

	resp, _ := toTemplateResponse(t)
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// ── Update ────────────────────────────────────────────────────────────────

// Update handles PUT /api/v1/dns-templates/:id
func (h *DNSTemplateHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid template id"})
		return
	}

	var req CreateDNSTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "name and records are required"})
		return
	}

	records := make([]postgres.TemplateRecord, len(req.Records))
	for i, r := range req.Records {
		records[i] = postgres.TemplateRecord{
			Name: r.Name, Type: r.Type, Content: r.Content, TTL: r.TTL, Priority: r.Priority,
		}
	}

	t, err := h.svc.Update(c.Request.Context(), id, dnstemplate.UpdateInput{
		Name:        req.Name,
		Description: req.Description,
		Records:     records,
		Variables:   req.Variables,
	})
	if errors.Is(err, dnstemplate.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "template not found"})
		return
	}
	if errors.Is(err, dnstemplate.ErrInvalidTemplate) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to update template"})
		return
	}

	resp, _ := toTemplateResponse(t)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// ── Delete ────────────────────────────────────────────────────────────────

// Delete handles DELETE /api/v1/dns-templates/:id
func (h *DNSTemplateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid template id"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, dnstemplate.ErrTemplateNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "template not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to delete template"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "deleted"})
}

// ── ApplyTemplate ─────────────────────────────────────────────────────────

type ApplyTemplateRequest struct {
	TemplateID int64             `json:"template_id" binding:"required"`
	Variables  map[string]string `json:"variables"`
}

// ApplyTemplate handles POST /api/v1/domains/:id/dns/apply-template
// Renders a template's records with variable substitution and returns them
// for the client to stage in the DNS management UI.
func (h *DNSTemplateHandler) ApplyTemplate(c *gin.Context) {
	_, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid domain id"})
		return
	}

	var req ApplyTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "template_id is required"})
		return
	}

	rendered, err := h.svc.ApplyTemplate(c.Request.Context(), req.TemplateID, req.Variables)
	if errors.Is(err, dnstemplate.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "template not found"})
		return
	}
	if errors.Is(err, dnstemplate.ErrMissingVariable) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "failed to apply template"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"data":    gin.H{"records": rendered, "count": len(rendered)},
		"message": "ok",
	})
}
