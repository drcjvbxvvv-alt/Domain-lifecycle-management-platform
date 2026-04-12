package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	agentsvc "domain-platform/internal/agent"
	"domain-platform/pkg/agentprotocol"
)

// AgentProtocolHandler implements the /agent/v1/* endpoints
// that the Pull Agent binary calls.
type AgentProtocolHandler struct {
	svc    *agentsvc.Service
	logger *zap.Logger
}

func NewAgentProtocolHandler(svc *agentsvc.Service, logger *zap.Logger) *AgentProtocolHandler {
	return &AgentProtocolHandler{svc: svc, logger: logger}
}

// Register handles POST /agent/v1/register
func (h *AgentProtocolHandler) Register(c *gin.Context) {
	var req agentprotocol.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request body",
		})
		return
	}

	if req.Hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "hostname is required",
		})
		return
	}

	resp, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		h.logger.Error("register agent", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "registration failed",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0, "data": resp, "message": "ok",
	})
}

// Heartbeat handles POST /agent/v1/heartbeat
func (h *AgentProtocolHandler) Heartbeat(c *gin.Context) {
	var req agentprotocol.HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request body",
		})
		return
	}

	if req.AgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "agent_id is required",
		})
		return
	}

	resp, err := h.svc.Heartbeat(c.Request.Context(), req)
	if err != nil {
		if err == agentsvc.ErrAgentNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 40400, "data": nil, "message": "agent not found",
			})
			return
		}
		h.logger.Error("heartbeat", zap.Error(err), zap.String("agent_id", req.AgentID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "heartbeat failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": resp, "message": "ok",
	})
}

// PollTasks handles GET /agent/v1/tasks?agent_id=xxx
func (h *AgentProtocolHandler) PollTasks(c *gin.Context) {
	agentID := c.Query("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "agent_id query param required",
		})
		return
	}

	envelope, err := h.svc.PullNextTask(c.Request.Context(), agentID)
	if err != nil {
		if err == agentsvc.ErrAgentNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 40400, "data": nil, "message": "agent not found",
			})
			return
		}
		h.logger.Error("poll tasks", zap.Error(err), zap.String("agent_id", agentID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "poll failed",
		})
		return
	}

	if envelope == nil {
		c.Status(http.StatusNoContent)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": envelope, "message": "ok",
	})
}

// ClaimTask handles POST /agent/v1/tasks/:taskId/claim
func (h *AgentProtocolHandler) ClaimTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "task_id path param required",
		})
		return
	}

	if err := h.svc.ClaimTask(c.Request.Context(), taskID); err != nil {
		h.logger.Error("claim task", zap.Error(err), zap.String("task_id", taskID))
		c.JSON(http.StatusConflict, gin.H{
			"code": 40900, "data": nil, "message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": nil, "message": "ok",
	})
}

// ReportTask handles POST /agent/v1/tasks/:taskId/report
func (h *AgentProtocolHandler) ReportTask(c *gin.Context) {
	var report agentprotocol.TaskReport
	if err := c.ShouldBindJSON(&report); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request body",
		})
		return
	}

	// Use path param as task_id if not set in body
	taskID := c.Param("taskId")
	if report.TaskID == "" {
		report.TaskID = taskID
	}

	if err := h.svc.ReportTask(c.Request.Context(), report); err != nil {
		h.logger.Error("report task", zap.Error(err), zap.String("task_id", report.TaskID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "report failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": nil, "message": "ok",
	})
}

// ── Management API endpoints for agents (viewed from /api/v1/agents) ────

// AgentHandler handles management API requests for viewing agent state.
type AgentHandler struct {
	svc    *agentsvc.Service
	logger *zap.Logger
}

func NewAgentHandler(svc *agentsvc.Service, logger *zap.Logger) *AgentHandler {
	return &AgentHandler{svc: svc, logger: logger}
}

// List handles GET /api/v1/agents
func (h *AgentHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	agents, total, err := h.svc.ListAgents(c.Request.Context(), limit, offset)
	if err != nil {
		h.logger.Error("list agents", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "list failed",
		})
		return
	}

	type agentResp struct {
		ID           int64   `json:"id"`
		AgentID      string  `json:"agent_id"`
		Hostname     string  `json:"hostname"`
		Region       *string `json:"region"`
		Status       string  `json:"status"`
		AgentVersion *string `json:"agent_version"`
		LastSeenAt   *string `json:"last_seen_at"`
	}

	items := make([]agentResp, 0, len(agents))
	for _, a := range agents {
		r := agentResp{
			ID:           a.ID,
			AgentID:      a.AgentID,
			Hostname:     a.Hostname,
			Region:       a.Region,
			Status:       a.Status,
			AgentVersion: a.AgentVersion,
		}
		if a.LastSeenAt != nil {
			ts := a.LastSeenAt.Format("2006-01-02T15:04:05Z07:00")
			r.LastSeenAt = &ts
		}
		items = append(items, r)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": items,
			"total": total,
		},
		"message": "ok",
	})
}

// Get handles GET /api/v1/agents/:id
func (h *AgentHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "invalid agent id",
		})
		return
	}

	agent, err := h.svc.GetAgent(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("get agent", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "get failed",
		})
		return
	}
	if agent == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 40400, "data": nil, "message": "agent not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": agent, "message": "ok",
	})
}

// Transition handles POST /api/v1/agents/:id/transition
func (h *AgentHandler) Transition(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "invalid agent id",
		})
		return
	}

	var req struct {
		From    string `json:"from" binding:"required"`
		To      string `json:"to" binding:"required"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "from and to are required",
		})
		return
	}

	triggeredBy := "api"
	if err := h.svc.TransitionAgent(c.Request.Context(), id, req.From, req.To, req.Reason, triggeredBy); err != nil {
		status := http.StatusConflict
		if err == agentsvc.ErrInvalidAgentState {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{
			"code": 40900, "data": nil, "message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": nil, "message": "ok",
	})
}

// History handles GET /api/v1/agents/:id/history
func (h *AgentHandler) History(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001, "data": nil, "message": "invalid agent id",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := h.svc.ListStateHistory(c.Request.Context(), id, limit)
	if err != nil {
		h.logger.Error("list agent history", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "list history failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "data": gin.H{"items": rows}, "message": "ok",
	})
}
