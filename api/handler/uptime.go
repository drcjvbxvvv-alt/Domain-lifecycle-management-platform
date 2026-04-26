package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/internal/probe"
)

// UptimeHandler exposes uptime analytics endpoints (PC.5).
type UptimeHandler struct {
	analytics *probe.AnalyticsService
	logger    *zap.Logger
}

// NewUptimeHandler constructs an UptimeHandler.
func NewUptimeHandler(analytics *probe.AnalyticsService, logger *zap.Logger) *UptimeHandler {
	return &UptimeHandler{analytics: analytics, logger: logger}
}

// GetUptime returns uptime % for a domain over a configurable duration.
// GET /api/v1/uptime/:domain_id?probe_type=l1&days=30
func (h *UptimeHandler) GetUptime(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("domain_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid domain_id"})
		return
	}
	probeType := c.DefaultQuery("probe_type", "l1")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 || days > 365 {
		days = 30
	}
	duration := time.Duration(days) * 24 * time.Hour
	result, err := h.analytics.GetUptime(c.Request.Context(), domainID, probeType, duration)
	if err != nil {
		h.logger.Error("get uptime", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"domain_id":  result.DomainID,
		"probe_type": probeType,
		"days":       days,
		"up_count":   result.UpCount,
		"down_count": result.DownCount,
		"uptime_pct": roundPct(result.UptimePct),
	}, "message": "ok"})
}

// GetResponseTimeSeries returns bucketed response-time data for a domain.
// GET /api/v1/uptime/:domain_id/response-time?probe_type=l1&granularity=hourly&from=...&to=...
func (h *UptimeHandler) GetResponseTimeSeries(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("domain_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid domain_id"})
		return
	}
	probeType   := c.DefaultQuery("probe_type", "l1")
	granularity := c.DefaultQuery("granularity", "hourly")

	from := time.Now().Add(-7 * 24 * time.Hour)
	to   := time.Now()
	if f := c.Query("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			from = t
		}
	}
	if t := c.Query("to"); t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			to = parsed
		}
	}

	points, err := h.analytics.GetResponseTimeSeries(c.Request.Context(), domainID, probeType, from, to, granularity)
	if err != nil {
		h.logger.Error("get response time series", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}

	items := make([]gin.H, 0, len(points))
	for _, p := range points {
		items = append(items, gin.H{
			"bucket":          p.Bucket.Format(time.RFC3339),
			"avg_response_ms": p.AvgResponseMS,
			"min_response_ms": p.MinResponseMS,
			"max_response_ms": p.MaxResponseMS,
			"p95_response_ms": p.P95ResponseMS,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items}, "message": "ok"})
}

// GetWorstPerformers returns domains with the lowest uptime over a duration.
// GET /api/v1/uptime/worst?probe_type=l1&days=30&limit=10
func (h *UptimeHandler) GetWorstPerformers(c *gin.Context) {
	probeType := c.DefaultQuery("probe_type", "l1")
	days, _   := strconv.Atoi(c.DefaultQuery("days", "30"))
	limit, _  := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if days  <= 0 || days  > 365 { days  = 30 }
	if limit <= 0 || limit > 100 { limit = 10 }

	results, err := h.analytics.GetWorstPerformers(
		c.Request.Context(),
		probeType,
		time.Duration(days)*24*time.Hour,
		limit,
	)
	if err != nil {
		h.logger.Error("get worst performers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	items := make([]gin.H, 0, len(results))
	for _, r := range results {
		items = append(items, gin.H{
			"domain_id":  r.DomainID,
			"fqdn":       r.FQDN,
			"uptime_pct": roundPct(r.UptimePct),
			"up_count":   r.UpCount,
			"down_count": r.DownCount,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items}, "message": "ok"})
}

// GetUptimeCalendar returns per-day uptime for a domain in a given month.
// GET /api/v1/uptime/:domain_id/calendar?year=2026&month=4
func (h *UptimeHandler) GetUptimeCalendar(c *gin.Context) {
	domainID, err := strconv.ParseInt(c.Param("domain_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "invalid domain_id"})
		return
	}
	now   := time.Now()
	year, _ := strconv.Atoi(c.DefaultQuery("year", strconv.Itoa(now.Year())))
	mon, _  := strconv.Atoi(c.DefaultQuery("month", strconv.Itoa(int(now.Month()))))
	if year < 2020 || year > 2100 { year = now.Year() }
	if mon  < 1    || mon  > 12   { mon  = int(now.Month()) }

	days, err := h.analytics.GetUptimeCalendar(c.Request.Context(), domainID, year, time.Month(mon))
	if err != nil {
		h.logger.Error("get uptime calendar", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "message": "internal error"})
		return
	}
	items := make([]gin.H, 0, len(days))
	for _, d := range days {
		items = append(items, gin.H{
			"date":       d.Date,
			"uptime_pct": roundPct2(d.UptimePct),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"domain_id": domainID,
		"year":      year,
		"month":     mon,
		"days":      items,
	}, "message": "ok"})
}

func roundPct2(f float64) float64 {
	if f < 0 {
		return -1 // sentinel: no data
	}
	return float64(int(f*100)) / 100.0
}
