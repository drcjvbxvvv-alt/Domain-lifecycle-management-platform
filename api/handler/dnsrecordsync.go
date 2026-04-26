package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/dnsrecord"
	"domain-platform/internal/dnstemplate"
	"domain-platform/store/postgres"
)

// DNSRecordSyncHandler handles DNS record CRUD backed by domain_dns_records (B.3).
//
// Routes (nested under /domains/:id):
//   POST   /dns-records/sync                — pull from provider, upsert local
//   GET    /dns-records/local               — read local snapshot
//   POST   /dns-records/local               — create at provider + persist
//   PUT    /dns-records/local/:rid          — update at provider + persist
//   DELETE /dns-records/local/:rid          — delete at provider + soft-delete
//   POST   /dns-records/local/batch-delete  — batch delete
type DNSRecordSyncHandler struct {
	syncSvc      *dnsrecord.SyncService
	templateSvc  *dnstemplate.Service
	domains      *postgres.DomainStore
	logger       *zap.Logger
}

func NewDNSRecordSyncHandler(
	syncSvc *dnsrecord.SyncService,
	templateSvc *dnstemplate.Service,
	domains *postgres.DomainStore,
	logger *zap.Logger,
) *DNSRecordSyncHandler {
	return &DNSRecordSyncHandler{syncSvc: syncSvc, templateSvc: templateSvc, domains: domains, logger: logger}
}

// ── request types ─────────────────────────────────────────────────────────────

type BatchDeleteRecordsRequest struct {
	RecordIDs []int64 `json:"record_ids" binding:"required"`
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *DNSRecordSyncHandler) getDomain(c *gin.Context) (*postgres.Domain, bool) {
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
		h.logger.Error("get domain", zap.Int64("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return nil, false
	}
	return domain, true
}

// ── handlers ──────────────────────────────────────────────────────────────────

// Sync handles POST /domains/:id/dns-records/sync
func (h *DNSRecordSyncHandler) Sync(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	result, err := h.syncSvc.Sync(c.Request.Context(), domain)
	if err != nil {
		if errors.Is(err, dnsrecord.ErrNoProvider) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": "domain has no DNS provider bound"})
			return
		}
		h.logger.Error("dns record sync", zap.String("fqdn", domain.FQDN), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "sync failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result, "message": "ok"})
}

// ListLocal handles GET /domains/:id/dns-records/local
func (h *DNSRecordSyncHandler) ListLocal(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	filterType := strings.ToUpper(c.Query("type"))
	records, err := h.syncSvc.ListRecords(c.Request.Context(), domain, filterType)
	if err != nil {
		h.logger.Error("list local dns records", zap.String("fqdn", domain.FQDN), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": records, "total": len(records)}, "message": "ok"})
}

// CreateLocal handles POST /domains/:id/dns-records/local
func (h *DNSRecordSyncHandler) CreateLocal(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	var in dnsrecord.CreateRecordInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	rec, err := h.syncSvc.CreateRecord(c.Request.Context(), domain, in)
	if err != nil {
		if errors.Is(err, dnsrecord.ErrNoProvider) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": "domain has no DNS provider bound"})
			return
		}
		if errors.Is(err, dnsrecord.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": err.Error()})
			return
		}
		h.logger.Error("create dns record", zap.String("fqdn", domain.FQDN), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "create record failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": rec, "message": "ok"})
}

// UpdateLocal handles PUT /domains/:id/dns-records/local/:rid
func (h *DNSRecordSyncHandler) UpdateLocal(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	localID, err := strconv.ParseInt(c.Param("rid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid record id"})
		return
	}

	var in dnsrecord.UpdateRecordInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid request body"})
		return
	}

	rec, err := h.syncSvc.UpdateRecord(c.Request.Context(), domain, localID, in)
	if err != nil {
		if errors.Is(err, dnsrecord.ErrDNSRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40402, "data": nil, "message": "dns record not found"})
			return
		}
		if errors.Is(err, dnsrecord.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": err.Error()})
			return
		}
		h.logger.Error("update dns record", zap.Int64("local_id", localID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "update record failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rec, "message": "ok"})
}

// DeleteLocal handles DELETE /domains/:id/dns-records/local/:rid
func (h *DNSRecordSyncHandler) DeleteLocal(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	localID, err := strconv.ParseInt(c.Param("rid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "invalid record id"})
		return
	}

	if err := h.syncSvc.DeleteRecord(c.Request.Context(), domain, localID); err != nil {
		if errors.Is(err, dnsrecord.ErrDNSRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40402, "data": nil, "message": "dns record not found"})
			return
		}
		if errors.Is(err, dnsrecord.ErrNoProvider) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": "domain has no DNS provider bound"})
			return
		}
		h.logger.Error("delete dns record", zap.Int64("local_id", localID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "delete record failed"})
		return
	}

	c.Status(http.StatusNoContent)
}

// BatchDeleteLocal handles POST /domains/:id/dns-records/local/batch-delete
func (h *DNSRecordSyncHandler) BatchDeleteLocal(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	var req BatchDeleteRecordsRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.RecordIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "record_ids required"})
		return
	}

	deleted, err := h.syncSvc.BatchDelete(c.Request.Context(), domain, req.RecordIDs)
	if err != nil {
		if errors.Is(err, dnsrecord.ErrNoProvider) {
			c.JSON(http.StatusConflict, gin.H{"code": 40900, "data": nil, "message": "domain has no DNS provider bound"})
			return
		}
		h.logger.Error("batch delete dns records", zap.String("fqdn", domain.FQDN), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "batch delete failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"deleted": deleted}, "message": "ok"})
}

// ── B.4: Apply DNS template ───────────────────────────────────────────────────

// ApplyTemplateSyncRequest is the body for POST /domains/:id/dns-records/apply-template.
type ApplyTemplateSyncRequest struct {
	TemplateID int64             `json:"template_id" binding:"required"`
	Variables  map[string]string `json:"variables"`
	DryRun     bool              `json:"dry_run"` // true = render only, do not create
}

// ApplyTemplateResult summarises the apply outcome.
type ApplyTemplateResult struct {
	Created  int                          `json:"created"`
	Errors   []string                     `json:"errors,omitempty"`
	Records  []postgres.DomainDNSRecord   `json:"records,omitempty"`  // created rows (non-dry-run)
	Preview  []dnstemplate.RenderedRecord `json:"preview,omitempty"` // dry-run preview
}

// ApplyTemplate handles POST /domains/:id/dns-records/apply-template (B.4).
// When dry_run=true it renders the template and returns a preview without
// creating any records. Otherwise it creates every rendered record at the
// provider and persists them locally.
func (h *DNSRecordSyncHandler) ApplyTemplate(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	var req ApplyTemplateSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40000, "data": nil, "message": "template_id is required"})
		return
	}

	ctx := c.Request.Context()

	rendered, err := h.templateSvc.ApplyTemplate(ctx, req.TemplateID, req.Variables)
	if errors.Is(err, dnstemplate.ErrTemplateNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": 40400, "data": nil, "message": "template not found"})
		return
	}
	if errors.Is(err, dnstemplate.ErrMissingVariable) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": err.Error()})
		return
	}
	if err != nil {
		h.logger.Error("render dns template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "data": nil, "message": "template render failed"})
		return
	}

	if req.DryRun {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"data":    ApplyTemplateResult{Preview: rendered},
			"message": "dry run complete",
		})
		return
	}

	// Create each rendered record at provider + local DB.
	result := ApplyTemplateResult{}
	for _, r := range rendered {
		in := dnsrecord.CreateRecordInput{
			Type:     r.Type,
			Name:     r.Name,
			Content:  r.Content,
			TTL:      r.TTL,
			Priority: r.Priority,
		}
		saved, err := h.syncSvc.CreateRecord(ctx, domain, in)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			h.logger.Warn("apply template create record failed",
				zap.String("fqdn", domain.FQDN),
				zap.String("name", r.Name),
				zap.Error(err),
			)
			continue
		}
		result.Created++
		result.Records = append(result.Records, *saved)
	}

	status := http.StatusOK
	if result.Created == 0 && len(result.Errors) > 0 {
		status = http.StatusInternalServerError
	}

	c.JSON(status, gin.H{"code": 0, "data": result, "message": "ok"})
}
