package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	spsvc "domain-platform/internal/statuspage"
	"domain-platform/store/postgres"
)

// StatusPageHandler exposes admin CRUD and public read endpoints for status pages.
type StatusPageHandler struct {
	svc    *spsvc.Service
	logger *zap.Logger
}

// NewStatusPageHandler constructs a StatusPageHandler.
func NewStatusPageHandler(svc *spsvc.Service, logger *zap.Logger) *StatusPageHandler {
	return &StatusPageHandler{svc: svc, logger: logger}
}

// ── Request / Response DTOs ───────────────────────────────────────────────────

type createPageRequest struct {
	Slug               string  `json:"slug"                 binding:"required"`
	Title              string  `json:"title"                binding:"required"`
	Description        *string `json:"description"`
	Published          *bool   `json:"published"`
	Password           *string `json:"password"`
	CustomDomain       *string `json:"custom_domain"`
	Theme              string  `json:"theme"`
	LogoURL            *string `json:"logo_url"`
	FooterText         *string `json:"footer_text"`
	CustomCSS          *string `json:"custom_css"`
	AutoRefreshSeconds int     `json:"auto_refresh_seconds"`
}

type updatePageRequest struct {
	Slug               string  `json:"slug"                 binding:"required"`
	Title              string  `json:"title"                binding:"required"`
	Description        *string `json:"description"`
	Published          *bool   `json:"published"`
	Password           *string `json:"password"`
	ClearPassword      bool    `json:"clear_password"`
	CustomDomain       *string `json:"custom_domain"`
	Theme              string  `json:"theme"`
	LogoURL            *string `json:"logo_url"`
	FooterText         *string `json:"footer_text"`
	CustomCSS          *string `json:"custom_css"`
	AutoRefreshSeconds int     `json:"auto_refresh_seconds"`
}

type createGroupRequest struct {
	Name      string `json:"name"       binding:"required"`
	SortOrder int    `json:"sort_order"`
}

type addMonitorRequest struct {
	DomainID    int64   `json:"domain_id"    binding:"required"`
	DisplayName *string `json:"display_name"`
	SortOrder   int     `json:"sort_order"`
}

type createIncidentRequest struct {
	Title    string  `json:"title"    binding:"required"`
	Content  *string `json:"content"`
	Severity string  `json:"severity" binding:"required"`
	Pinned   bool    `json:"pinned"`
}

type updateIncidentRequest struct {
	Title    string  `json:"title"    binding:"required"`
	Content  *string `json:"content"`
	Severity string  `json:"severity" binding:"required"`
	Pinned   bool    `json:"pinned"`
	Active   *bool   `json:"active"`
}

type authPageRequest struct {
	Password string `json:"password" binding:"required"`
}

// ── Response helpers ──────────────────────────────────────────────────────────

func pageResponse(p *postgres.StatusPage) gin.H {
	r := gin.H{
		"id":                   p.ID,
		"uuid":                 p.UUID,
		"slug":                 p.Slug,
		"title":                p.Title,
		"description":          p.Description,
		"published":            p.Published,
		"has_password":         p.PasswordHash != nil,
		"custom_domain":        p.CustomDomain,
		"theme":                p.Theme,
		"logo_url":             p.LogoURL,
		"footer_text":          p.FooterText,
		"custom_css":           p.CustomCSS,
		"auto_refresh_seconds": p.AutoRefreshSeconds,
		"created_at":           p.CreatedAt.Format(time.RFC3339),
		"updated_at":           p.UpdatedAt.Format(time.RFC3339),
	}
	return r
}

func groupResponse(g *postgres.StatusPageGroup) gin.H {
	return gin.H{
		"id":             g.ID,
		"status_page_id": g.StatusPageID,
		"name":           g.Name,
		"sort_order":     g.SortOrder,
	}
}

func monitorResponse(m *postgres.StatusPageMonitor) gin.H {
	return gin.H{
		"id":           m.ID,
		"group_id":     m.GroupID,
		"domain_id":    m.DomainID,
		"display_name": m.DisplayName,
		"sort_order":   m.SortOrder,
	}
}

func incidentResponse(inc *postgres.StatusPageIncident) gin.H {
	return gin.H{
		"id":             inc.ID,
		"status_page_id": inc.StatusPageID,
		"title":          inc.Title,
		"content":        inc.Content,
		"severity":       inc.Severity,
		"pinned":         inc.Pinned,
		"active":         inc.Active,
		"created_at":     inc.CreatedAt.Format(time.RFC3339),
		"updated_at":     inc.UpdatedAt.Format(time.RFC3339),
	}
}

// ── Admin: Status Page CRUD ───────────────────────────────────────────────────

// ListPages returns all status pages.
func (h *StatusPageHandler) ListPages(c *gin.Context) {
	pages, err := h.svc.ListPages(c.Request.Context())
	if err != nil {
		h.logger.Error("list status pages", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	items := make([]gin.H, 0, len(pages))
	for i := range pages {
		items = append(items, pageResponse(&pages[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": len(items)}, "message": "ok"})
}

// GetPage returns a single status page with its groups.
func (h *StatusPageHandler) GetPage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	page, err := h.svc.GetPage(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrStatusPageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "not found"})
			return
		}
		h.logger.Error("get status page", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	groups, _ := h.svc.ListGroups(c.Request.Context(), page.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"page":   pageResponse(page),
		"groups": groups,
	}, "message": "ok"})
}

// CreatePage creates a new status page.
func (h *StatusPageHandler) CreatePage(c *gin.Context) {
	var req createPageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	published := true
	if req.Published != nil {
		published = *req.Published
	}
	var createdBy *int64
	if uid, ok := c.Get("userID"); ok {
		if id, ok := uid.(int64); ok {
			createdBy = &id
		}
	}
	page, err := h.svc.CreatePage(c.Request.Context(), spsvc.CreatePageInput{
		Slug:               req.Slug,
		Title:              req.Title,
		Description:        req.Description,
		Published:          published,
		Password:           req.Password,
		CustomDomain:       req.CustomDomain,
		Theme:              req.Theme,
		LogoURL:            req.LogoURL,
		FooterText:         req.FooterText,
		CustomCSS:          req.CustomCSS,
		AutoRefreshSeconds: req.AutoRefreshSeconds,
		CreatedBy:          createdBy,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": pageResponse(page), "message": "ok"})
}

// UpdatePage saves changes to a status page.
func (h *StatusPageHandler) UpdatePage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	var req updatePageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	published := true
	if req.Published != nil {
		published = *req.Published
	}
	page, err := h.svc.UpdatePage(c.Request.Context(), id, spsvc.UpdatePageInput{
		Slug:               req.Slug,
		Title:              req.Title,
		Description:        req.Description,
		Published:          published,
		Password:           req.Password,
		ClearPassword:      req.ClearPassword,
		CustomDomain:       req.CustomDomain,
		Theme:              req.Theme,
		LogoURL:            req.LogoURL,
		FooterText:         req.FooterText,
		CustomCSS:          req.CustomCSS,
		AutoRefreshSeconds: req.AutoRefreshSeconds,
	})
	if err != nil {
		if errors.Is(err, postgres.ErrStatusPageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": pageResponse(page), "message": "ok"})
}

// DeletePage removes a status page.
func (h *StatusPageHandler) DeletePage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	if err := h.svc.DeletePage(c.Request.Context(), id); err != nil {
		h.logger.Error("delete status page", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ── Admin: Groups ─────────────────────────────────────────────────────────────

// CreateGroup adds a group to a page.
func (h *StatusPageHandler) CreateGroup(c *gin.Context) {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid page id"})
		return
	}
	var req createGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	g, err := h.svc.CreateGroup(c.Request.Context(), pageID, req.Name, req.SortOrder)
	if err != nil {
		h.logger.Error("create group", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": groupResponse(g), "message": "ok"})
}

// UpdateGroup saves changes to a group.
func (h *StatusPageHandler) UpdateGroup(c *gin.Context) {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid page id"})
		return
	}
	groupID, err := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid group id"})
		return
	}
	var req createGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	if err := h.svc.UpdateGroup(c.Request.Context(), &postgres.StatusPageGroup{
		ID: groupID, StatusPageID: pageID, Name: req.Name, SortOrder: req.SortOrder,
	}); err != nil {
		h.logger.Error("update group", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"id": groupID, "name": req.Name}, "message": "ok"})
}

// DeleteGroup removes a group from a page.
func (h *StatusPageHandler) DeleteGroup(c *gin.Context) {
	pageID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err := h.svc.DeleteGroup(c.Request.Context(), pageID, groupID); err != nil {
		h.logger.Error("delete group", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ── Admin: Monitors ───────────────────────────────────────────────────────────

// AddMonitor links a domain to a group.
func (h *StatusPageHandler) AddMonitor(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid group id"})
		return
	}
	var req addMonitorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	m, err := h.svc.AddMonitor(c.Request.Context(), groupID, req.DomainID, req.DisplayName, req.SortOrder)
	if err != nil {
		h.logger.Error("add monitor", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": monitorResponse(m), "message": "ok"})
}

// RemoveMonitor unlinks a monitor from a group.
func (h *StatusPageHandler) RemoveMonitor(c *gin.Context) {
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	monitorID, _ := strconv.ParseInt(c.Param("mid"), 10, 64)
	if err := h.svc.RemoveMonitor(c.Request.Context(), groupID, monitorID); err != nil {
		h.logger.Error("remove monitor", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ── Admin: Incidents ──────────────────────────────────────────────────────────

// ListIncidents returns all incidents for a page.
func (h *StatusPageHandler) ListIncidents(c *gin.Context) {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid page id"})
		return
	}
	incs, err := h.svc.ListIncidents(c.Request.Context(), pageID, false)
	if err != nil {
		h.logger.Error("list incidents", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	items := make([]gin.H, 0, len(incs))
	for i := range incs {
		items = append(items, incidentResponse(&incs[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items}, "message": "ok"})
}

// CreateIncident creates a new incident post.
func (h *StatusPageHandler) CreateIncident(c *gin.Context) {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid page id"})
		return
	}
	var req createIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	var createdBy *int64
	if uid, ok := c.Get("userID"); ok {
		if id, ok := uid.(int64); ok {
			createdBy = &id
		}
	}
	inc, err := h.svc.CreateIncident(c.Request.Context(), pageID, req.Title, req.Content, req.Severity, req.Pinned, createdBy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": incidentResponse(inc), "message": "ok"})
}

// UpdateIncident saves changes to an incident.
func (h *StatusPageHandler) UpdateIncident(c *gin.Context) {
	pageID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	incID, err := strconv.ParseInt(c.Param("iid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid incident id"})
		return
	}
	var req updateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	inc := &postgres.StatusPageIncident{
		ID:           incID,
		StatusPageID: pageID,
		Title:        req.Title,
		Content:      req.Content,
		Severity:     req.Severity,
		Pinned:       req.Pinned,
		Active:       active,
	}
	if err := h.svc.UpdateIncident(c.Request.Context(), inc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": incidentResponse(inc), "message": "ok"})
}

// DeleteIncident removes an incident.
func (h *StatusPageHandler) DeleteIncident(c *gin.Context) {
	pageID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	incID, _ := strconv.ParseInt(c.Param("iid"), 10, 64)
	if err := h.svc.DeleteIncident(c.Request.Context(), pageID, incID); err != nil {
		h.logger.Error("delete incident", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ── Public endpoints (no auth) ────────────────────────────────────────────────

// GetPublicStatus returns the full public status for a page.
func (h *StatusPageHandler) GetPublicStatus(c *gin.Context) {
	slug := c.Param("slug")
	status, err := h.svc.GetPublicStatus(c.Request.Context(), slug)
	if err != nil {
		if errors.Is(err, postgres.ErrStatusPageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "status page not found"})
			return
		}
		h.logger.Error("get public status", zap.String("slug", slug), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}

	// If password-protected: check auth header or return 401.
	if status.Page.PasswordHash != nil {
		token := c.GetHeader("X-Status-Token")
		if !h.svc.VerifyPassword(status.Page, token) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 40101, "message": "password required", "data": gin.H{"password_required": true}})
			return
		}
	}

	// Build response — redact password hash.
	page := status.Page
	groups := make([]gin.H, 0, len(status.Groups))
	for _, g := range status.Groups {
		monitors := make([]gin.H, 0, len(g.Monitors))
		for _, m := range g.Monitors {
			monitors = append(monitors, gin.H{
				"monitor_id":   m.MonitorID,
				"domain_id":    m.DomainID,
				"display_name": m.DisplayName,
				"status":       m.Status,
				"uptime_pct":   roundPct(m.UptimePct),
				"response_ms":  m.ResponseMS,
			})
		}
		groups = append(groups, gin.H{
			"group_id":   g.GroupID,
			"group_name": g.GroupName,
			"status":     g.Status,
			"monitors":   monitors,
		})
	}
	incidents := make([]gin.H, 0, len(status.Incidents))
	for i := range status.Incidents {
		incidents = append(incidents, incidentResponse(&status.Incidents[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"page": gin.H{
			"slug":                 page.Slug,
			"title":                page.Title,
			"description":          page.Description,
			"theme":                page.Theme,
			"logo_url":             page.LogoURL,
			"footer_text":          page.FooterText,
			"custom_css":           page.CustomCSS,
			"auto_refresh_seconds": page.AutoRefreshSeconds,
		},
		"overall":   status.Overall,
		"groups":    groups,
		"incidents": incidents,
	}, "message": "ok"})
}

// AuthPage verifies a password and returns a token (just the password itself,
// client sends it back via X-Status-Token header).
func (h *StatusPageHandler) AuthPage(c *gin.Context) {
	slug := c.Param("slug")
	var req authPageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	// We need the page to verify the password.
	// Use internal GetPublicStatus but we just need the page here.
	status, err := h.svc.GetPublicStatus(c.Request.Context(), slug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "not found"})
		return
	}
	if !h.svc.VerifyPassword(status.Page, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 40101, "message": "incorrect password"})
		return
	}
	// Return the password as the bearer token (simple, sufficient for this use-case).
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"token": req.Password}, "message": "ok"})
}

// ── helper ────────────────────────────────────────────────────────────────────

func roundPct(f float64) float64 {
	return float64(int(f*100)) / 100.0
}
