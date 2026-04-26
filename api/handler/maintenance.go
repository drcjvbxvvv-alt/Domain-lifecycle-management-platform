package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/maintenance"
	"domain-platform/store/postgres"
)

// MaintenanceHandler exposes CRUD endpoints for maintenance windows.
type MaintenanceHandler struct {
	svc    *maintenance.Service
	logger *zap.Logger
}

// NewMaintenanceHandler constructs a MaintenanceHandler.
func NewMaintenanceHandler(svc *maintenance.Service, logger *zap.Logger) *MaintenanceHandler {
	return &MaintenanceHandler{svc: svc, logger: logger}
}

// ── Request / Response DTOs ───────────────────────────────────────────────────

type createMaintenanceRequest struct {
	Title       string   `json:"title"       binding:"required"`
	Description *string  `json:"description"`
	Strategy    string   `json:"strategy"    binding:"required"`
	StartAt     *string  `json:"start_at"`   // RFC3339 for single
	EndAt       *string  `json:"end_at"`     // RFC3339 for single
	Recurrence  any      `json:"recurrence"` // pass through as JSON
	Active      *bool    `json:"active"`
}

type updateMaintenanceRequest struct {
	Title       string  `json:"title"       binding:"required"`
	Description *string `json:"description"`
	Strategy    string  `json:"strategy"    binding:"required"`
	StartAt     *string `json:"start_at"`
	EndAt       *string `json:"end_at"`
	Recurrence  any     `json:"recurrence"`
	Active      *bool   `json:"active"`
}

type addTargetRequest struct {
	TargetType string `json:"target_type" binding:"required"`
	TargetID   int64  `json:"target_id"   binding:"required"`
}

type maintenanceResponse struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	Title       string `json:"title"`
	Description *string `json:"description,omitempty"`
	Strategy    string  `json:"strategy"`
	StartAt     *string `json:"start_at,omitempty"`
	EndAt       *string `json:"end_at,omitempty"`
	Recurrence  any     `json:"recurrence,omitempty"`
	Active      bool    `json:"active"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type maintenanceWithTargetsResponse struct {
	maintenanceResponse
	Targets []targetResponse `json:"targets"`
}

type targetResponse struct {
	ID         int64  `json:"id"`
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// List returns all maintenance windows.
func (h *MaintenanceHandler) List(c *gin.Context) {
	ws, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.logger.Error("list maintenance windows", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	items := make([]maintenanceResponse, 0, len(ws))
	for i := range ws {
		items = append(items, toMaintenanceResponse(&ws[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": len(items)}, "message": "ok"})
}

// Get returns a single window with its targets.
func (h *MaintenanceHandler) Get(c *gin.Context) {
	id, err := parseMaintenanceID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	w, targets, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrMaintenanceWindowNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "not found"})
			return
		}
		h.logger.Error("get maintenance window", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	resp := maintenanceWithTargetsResponse{
		maintenanceResponse: toMaintenanceResponse(w),
		Targets:             toTargetResponses(targets),
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": "ok"})
}

// Create creates a new maintenance window.
func (h *MaintenanceHandler) Create(c *gin.Context) {
	var req createMaintenanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	in, err := buildCreateInput(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	// Attach creator from JWT context.
	if uid, ok := c.Get("userID"); ok {
		if id, ok := uid.(int64); ok {
			in.CreatedBy = &id
		}
	}
	w, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40003, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": toMaintenanceResponse(w), "message": "ok"})
}

// Update saves changes to an existing window.
func (h *MaintenanceHandler) Update(c *gin.Context) {
	id, err := parseMaintenanceID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	var req updateMaintenanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	in, err := buildUpdateInput(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	w, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		if errors.Is(err, postgres.ErrMaintenanceWindowNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 40003, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": toMaintenanceResponse(w), "message": "ok"})
}

// Delete removes a maintenance window.
func (h *MaintenanceHandler) Delete(c *gin.Context) {
	id, err := parseMaintenanceID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		h.logger.Error("delete maintenance window", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// AddTarget links a target to a maintenance window.
func (h *MaintenanceHandler) AddTarget(c *gin.Context) {
	id, err := parseMaintenanceID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	var req addTargetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": err.Error()})
		return
	}
	t, err := h.svc.AddTarget(c.Request.Context(), id, req.TargetType, req.TargetID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": targetResponse{
		ID:         t.ID,
		TargetType: t.TargetType,
		TargetID:   t.TargetID,
	}, "message": "ok"})
}

// RemoveTarget unlinks a target from a maintenance window.
func (h *MaintenanceHandler) RemoveTarget(c *gin.Context) {
	id, err := parseMaintenanceID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid id"})
		return
	}
	tid, err := strconv.ParseInt(c.Param("tid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid target id"})
		return
	}
	if err := h.svc.RemoveTarget(c.Request.Context(), id, tid); err != nil {
		h.logger.Error("remove maintenance target", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseMaintenanceID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

func toMaintenanceResponse(w *postgres.MaintenanceWindow) maintenanceResponse {
	r := maintenanceResponse{
		ID:          w.ID,
		UUID:        w.UUID,
		Title:       w.Title,
		Description: w.Description,
		Strategy:    w.Strategy,
		Active:      w.Active,
		CreatedAt:   w.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   w.UpdatedAt.Format(time.RFC3339),
	}
	if w.StartAt != nil {
		s := w.StartAt.Format(time.RFC3339)
		r.StartAt = &s
	}
	if w.EndAt != nil {
		e := w.EndAt.Format(time.RFC3339)
		r.EndAt = &e
	}
	if len(w.Recurrence) > 0 && string(w.Recurrence) != "null" {
		var v any
		if err := json.Unmarshal(w.Recurrence, &v); err == nil {
			r.Recurrence = v
		}
	}
	return r
}

func toTargetResponses(ts []postgres.MaintenanceTarget) []targetResponse {
	out := make([]targetResponse, 0, len(ts))
	for _, t := range ts {
		out = append(out, targetResponse{
			ID:         t.ID,
			TargetType: t.TargetType,
			TargetID:   t.TargetID,
		})
	}
	return out
}

func buildCreateInput(req createMaintenanceRequest) (maintenance.CreateWindowInput, error) {
	in := maintenance.CreateWindowInput{
		Title:       req.Title,
		Description: req.Description,
		Strategy:    req.Strategy,
	}
	if req.Active == nil {
		b := true
		in.Active = b
	} else {
		in.Active = *req.Active
	}
	var err error
	in.StartAt, in.EndAt, err = parseStartEnd(req.StartAt, req.EndAt)
	if err != nil {
		return in, err
	}
	in.Recurrence, err = marshalRecurrence(req.Recurrence)
	return in, err
}

func buildUpdateInput(req updateMaintenanceRequest) (maintenance.UpdateWindowInput, error) {
	in := maintenance.UpdateWindowInput{
		Title:       req.Title,
		Description: req.Description,
		Strategy:    req.Strategy,
	}
	if req.Active == nil {
		in.Active = true
	} else {
		in.Active = *req.Active
	}
	var err error
	in.StartAt, in.EndAt, err = parseStartEnd(req.StartAt, req.EndAt)
	if err != nil {
		return in, err
	}
	in.Recurrence, err = marshalRecurrence(req.Recurrence)
	return in, err
}

func parseStartEnd(startStr, endStr *string) (*time.Time, *time.Time, error) {
	var startAt, endAt *time.Time
	if startStr != nil && *startStr != "" {
		t, err := time.Parse(time.RFC3339, *startStr)
		if err != nil {
			return nil, nil, err
		}
		startAt = &t
	}
	if endStr != nil && *endStr != "" {
		t, err := time.Parse(time.RFC3339, *endStr)
		if err != nil {
			return nil, nil, err
		}
		endAt = &t
	}
	return startAt, endAt, nil
}

func marshalRecurrence(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
