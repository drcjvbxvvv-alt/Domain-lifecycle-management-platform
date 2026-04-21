package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/dnsrecord"
	"domain-platform/internal/lifecycle"
	"domain-platform/store/postgres"
)

// DNSRecordHandler handles DNS record CRUD via provider API.
type DNSRecordHandler struct {
	svc       *dnsrecord.Service
	lifecycle *lifecycle.Service
	logger    *zap.Logger
}

// NewDNSRecordHandler creates a DNSRecordHandler.
func NewDNSRecordHandler(svc *dnsrecord.Service, lifecycle *lifecycle.Service, logger *zap.Logger) *DNSRecordHandler {
	return &DNSRecordHandler{svc: svc, lifecycle: lifecycle, logger: logger}
}

// ── List ──────────────────────────────────────────────────────────────────────

// ListRecords handles GET /api/v1/domains/:id/provider-records
// Query param: type (optional, filter by record type)
func (h *DNSRecordHandler) ListRecords(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	filterType := c.Query("type")

	records, err := h.svc.ListRecords(c.Request.Context(), domain, filterType)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": records, "total": len(records)}, "message": "ok"})
}

// ── Create ────────────────────────────────────────────────────────────────────

// CreateRecord handles POST /api/v1/domains/:id/provider-records
func (h *DNSRecordHandler) CreateRecord(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	var input dnsrecord.CreateRecordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid request body"})
		return
	}

	record, err := h.svc.CreateRecord(c.Request.Context(), domain, input)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": record, "message": "ok"})
}

// ── Update ────────────────────────────────────────────────────────────────────

// UpdateRecord handles PUT /api/v1/domains/:id/provider-records/:rid
func (h *DNSRecordHandler) UpdateRecord(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	recordID := c.Param("rid")
	if recordID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "data": nil, "message": "record ID is required"})
		return
	}

	var input dnsrecord.UpdateRecordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid request body"})
		return
	}

	record, err := h.svc.UpdateRecord(c.Request.Context(), domain, recordID, input)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": record, "message": "ok"})
}

// ── Delete ────────────────────────────────────────────────────────────────────

// DeleteRecord handles DELETE /api/v1/domains/:id/provider-records/:rid
func (h *DNSRecordHandler) DeleteRecord(c *gin.Context) {
	domain, ok := h.getDomain(c)
	if !ok {
		return
	}

	recordID := c.Param("rid")
	if recordID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "data": nil, "message": "record ID is required"})
		return
	}

	if err := h.svc.DeleteRecord(c.Request.Context(), domain, recordID); err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "ok"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (h *DNSRecordHandler) getDomain(c *gin.Context) (*postgres.Domain, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid domain id"})
		return nil, false
	}

	domain, err := h.lifecycle.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "domain not found"})
		return nil, false
	}

	return domain, true
}

func (h *DNSRecordHandler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, dnsrecord.ErrNoProvider):
		c.JSON(http.StatusBadRequest, gin.H{"code": 40010, "data": nil, "message": "domain has no DNS provider configured"})
	case errors.Is(err, dnsrecord.ErrProviderInit):
		c.JSON(http.StatusBadRequest, gin.H{"code": 40011, "data": nil, "message": err.Error()})
	case errors.Is(err, dnsrecord.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"code": 40012, "data": nil, "message": err.Error()})
	default:
		h.logger.Error("dns record operation failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "DNS provider API error"})
	}
}
