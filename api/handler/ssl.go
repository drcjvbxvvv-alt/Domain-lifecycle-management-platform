package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/ssl"
	"domain-platform/store/postgres"
)

// SSLHandler handles HTTP requests for SSL certificate management.
type SSLHandler struct {
	svc    *ssl.Service
	logger *zap.Logger
}

func NewSSLHandler(svc *ssl.Service, logger *zap.Logger) *SSLHandler {
	return &SSLHandler{svc: svc, logger: logger}
}

// ── Request / Response types ──────────────────────────────────────────────────

type CreateSSLCertRequest struct {
	Issuer       *string `json:"issuer"`
	CertType     *string `json:"cert_type"`
	SerialNumber *string `json:"serial_number"`
	IssuedAt     *string `json:"issued_at"`  // YYYY-MM-DD, optional
	ExpiresAt    string  `json:"expires_at"` // YYYY-MM-DD, required
	AutoRenew    bool    `json:"auto_renew"`
	Notes        *string `json:"notes"`
}

type SSLCertResponse struct {
	ID           int64   `json:"id"`
	UUID         string  `json:"uuid"`
	DomainID     int64   `json:"domain_id"`
	Issuer       *string `json:"issuer"`
	CertType     *string `json:"cert_type"`
	SerialNumber *string `json:"serial_number"`
	IssuedAt     *string `json:"issued_at"`
	ExpiresAt    string  `json:"expires_at"`
	DaysLeft     int     `json:"days_left"`
	AutoRenew    bool    `json:"auto_renew"`
	Status       string  `json:"status"`
	LastCheckAt  *string `json:"last_check_at"`
	Notes        *string `json:"notes"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func certResponse(c *postgres.SSLCertificate) SSLCertResponse {
	r := SSLCertResponse{
		ID:           c.ID,
		UUID:         c.UUID,
		DomainID:     c.DomainID,
		Issuer:       c.Issuer,
		CertType:     c.CertType,
		SerialNumber: c.SerialNumber,
		ExpiresAt:    c.ExpiresAt.Format("2006-01-02"),
		DaysLeft:     int(time.Until(c.ExpiresAt).Hours() / 24),
		AutoRenew:    c.AutoRenew,
		Status:       c.Status,
		Notes:        c.Notes,
		CreatedAt:    c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.Format(time.RFC3339),
	}
	if c.IssuedAt != nil {
		s := c.IssuedAt.Format("2006-01-02")
		r.IssuedAt = &s
	}
	if c.LastCheckAt != nil {
		s := c.LastCheckAt.Format(time.RFC3339)
		r.LastCheckAt = &s
	}
	return r
}

func certsResponse(certs []postgres.SSLCertificate) []SSLCertResponse {
	out := make([]SSLCertResponse, len(certs))
	for i, c := range certs {
		out[i] = certResponse(&c)
	}
	return out
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Create handles POST /api/v1/domains/:id/ssl-certs (manual add).
func (h *SSLHandler) Create(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid domain id"})
		return
	}

	var req CreateSSLCertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "data": nil, "message": err.Error()})
		return
	}
	if req.ExpiresAt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40003, "data": nil, "message": "expires_at is required"})
		return
	}

	expiresAt, err := time.Parse("2006-01-02", req.ExpiresAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40004, "data": nil, "message": "expires_at must be YYYY-MM-DD"})
		return
	}

	in := ssl.CreateInput{
		DomainID:     domainID,
		Issuer:       req.Issuer,
		CertType:     req.CertType,
		SerialNumber: req.SerialNumber,
		ExpiresAt:    expiresAt,
		AutoRenew:    req.AutoRenew,
		Notes:        req.Notes,
	}
	if req.IssuedAt != nil {
		t, err := time.Parse("2006-01-02", *req.IssuedAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40005, "data": nil, "message": "issued_at must be YYYY-MM-DD"})
			return
		}
		in.IssuedAt = &t
	}

	created, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		h.logger.Error("create ssl cert", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "create ssl cert failed"})
		return
	}

	resp := certResponse(created)
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// List handles GET /api/v1/domains/:id/ssl-certs.
func (h *SSLHandler) List(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid domain id"})
		return
	}

	certs, err := h.svc.List(c.Request.Context(), domainID)
	if err != nil {
		h.logger.Error("list ssl certs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "list ssl certs failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"items": certsResponse(certs),
		"total": len(certs),
	}, "message": "ok"})
}

// Check handles POST /api/v1/domains/:id/ssl-certs/check — live TLS probe.
func (h *SSLHandler) Check(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid domain id"})
		return
	}

	var req struct {
		FQDN string `json:"fqdn" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "data": nil, "message": "fqdn is required"})
		return
	}

	cert, err := h.svc.CheckExpiry(c.Request.Context(), domainID, req.FQDN)
	if err != nil {
		if errors.Is(err, ssl.ErrCheckFailed) {
			c.JSON(http.StatusBadGateway, gin.H{"code": 50201, "data": nil, "message": err.Error()})
			return
		}
		h.logger.Error("ssl check", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "ssl check failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": certResponse(cert), "message": "ok"})
}

// ListExpiring handles GET /api/v1/ssl-certs/expiring?days=30.
func (h *SSLHandler) ListExpiring(c *gin.Context) {
	days := 30
	if d := c.Query("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 {
			days = v
		}
	}

	certs, err := h.svc.ListExpiring(c.Request.Context(), days)
	if err != nil {
		h.logger.Error("list expiring ssl certs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "list expiring certs failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"items": certsResponse(certs),
		"total": len(certs),
		"days":  days,
	}, "message": "ok"})
}

// Delete handles DELETE /api/v1/ssl-certs/:id.
func (h *SSLHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "data": nil, "message": "invalid id"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, ssl.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "data": nil, "message": "ssl cert not found"})
			return
		}
		h.logger.Error("delete ssl cert", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "data": nil, "message": "delete ssl cert failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "ok"})
}
