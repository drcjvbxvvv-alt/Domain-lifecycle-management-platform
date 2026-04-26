package probe

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// UptimeResult is the uptime percentage for a single domain over a period.
type UptimeResult struct {
	DomainID  int64
	UpCount   int64
	DownCount int64
	UptimePct float64 // 0.0 – 100.0
}

// DataPoint represents one time-series bucket (response time).
type DataPoint struct {
	Bucket        time.Time `db:"bucket"`
	AvgResponseMS *float64  `db:"avg_response_ms"`
	MinResponseMS *int64    `db:"min_response_ms"`
	MaxResponseMS *int64    `db:"max_response_ms"`
	P95ResponseMS *float64  `db:"p95_response_ms"`
}

// DomainUptime is a per-domain summary for the "worst performers" table.
type DomainUptime struct {
	DomainID  int64   `db:"domain_id"`
	FQDN      string  `db:"fqdn"`
	UptimePct float64 // computed
	UpCount   int64   `db:"up_count"`
	DownCount int64   `db:"down_count"`
}

// DayStatus is one calendar day for the calendar heatmap.
type DayStatus struct {
	Date      string  // "YYYY-MM-DD"
	UptimePct float64 // 0–100; -1 = no data
}

// ─── AnalyticsService ─────────────────────────────────────────────────────────

// AnalyticsService provides uptime and response-time analytics.
type AnalyticsService struct {
	db     *sqlx.DB
	logger *zap.Logger
}

// NewAnalyticsService constructs an AnalyticsService.
func NewAnalyticsService(db *sqlx.DB, logger *zap.Logger) *AnalyticsService {
	return &AnalyticsService{db: db, logger: logger}
}

// GetUptime returns the uptime percentage for a domain over the given duration.
// probeType should be "l1", "l2", or "l3".
func (s *AnalyticsService) GetUptime(ctx context.Context, domainID int64, probeType string, duration time.Duration) (*UptimeResult, error) {
	since := time.Now().Add(-duration)
	var result struct {
		UpCount   int64 `db:"up_count"`
		DownCount int64 `db:"down_count"`
	}
	err := s.db.GetContext(ctx, &result, `
		SELECT
		    COUNT(*) FILTER (WHERE status = 'up')   AS up_count,
		    COUNT(*) FILTER (WHERE status = 'down') AS down_count
		FROM probe_results
		WHERE domain_id=$1 AND probe_type=$2 AND measured_at >= $3`,
		domainID, probeType, since)
	if err != nil {
		return nil, fmt.Errorf("get uptime for domain %d: %w", domainID, err)
	}
	r := &UptimeResult{DomainID: domainID, UpCount: result.UpCount, DownCount: result.DownCount}
	total := result.UpCount + result.DownCount
	if total > 0 {
		r.UptimePct = float64(result.UpCount) / float64(total) * 100
	}
	return r, nil
}

// GetResponseTimeSeries returns hourly or daily avg response-time buckets.
// granularity should be "hourly" (use probe_stats_hourly) or "daily" (probe_stats_daily).
// Falls back to raw probe_results if the continuous aggregate isn't populated yet.
func (s *AnalyticsService) GetResponseTimeSeries(
	ctx context.Context,
	domainID int64,
	probeType string,
	from, to time.Time,
	granularity string,
) ([]DataPoint, error) {
	bucket := "1 hour"
	if granularity == "daily" {
		bucket = "1 day"
	}
	var rows []DataPoint
	err := s.db.SelectContext(ctx, &rows, `
		SELECT
		    time_bucket($1::interval, measured_at)                          AS bucket,
		    AVG(response_time_ms)                                            AS avg_response_ms,
		    MIN(response_time_ms)                                            AS min_response_ms,
		    MAX(response_time_ms)                                            AS max_response_ms,
		    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY response_time_ms)  AS p95_response_ms
		FROM probe_results
		WHERE domain_id=$2 AND probe_type=$3
		  AND measured_at >= $4 AND measured_at < $5
		GROUP BY bucket
		ORDER BY bucket`,
		bucket, domainID, probeType, from, to)
	if err != nil {
		return nil, fmt.Errorf("get response time series: %w", err)
	}
	return rows, nil
}

// GetWorstPerformers returns the domains with the lowest uptime percentage
// over the given duration, limited to `limit` results.
func (s *AnalyticsService) GetWorstPerformers(
	ctx context.Context,
	probeType string,
	duration time.Duration,
	limit int,
) ([]DomainUptime, error) {
	since := time.Now().Add(-duration)
	type row struct {
		DomainID  int64  `db:"domain_id"`
		FQDN      string `db:"fqdn"`
		UpCount   int64  `db:"up_count"`
		DownCount int64  `db:"down_count"`
	}
	var rows []row
	err := s.db.SelectContext(ctx, &rows, `
		SELECT
		    pr.domain_id,
		    d.fqdn,
		    COUNT(*) FILTER (WHERE pr.status = 'up')   AS up_count,
		    COUNT(*) FILTER (WHERE pr.status = 'down') AS down_count
		FROM probe_results pr
		JOIN domains d ON d.id = pr.domain_id
		WHERE pr.probe_type=$1 AND pr.measured_at >= $2
		GROUP BY pr.domain_id, d.fqdn
		HAVING (COUNT(*) FILTER (WHERE pr.status = 'up') + COUNT(*) FILTER (WHERE pr.status = 'down')) > 0
		ORDER BY (COUNT(*) FILTER (WHERE pr.status = 'up')::float /
		    NULLIF(COUNT(*) FILTER (WHERE pr.status IN ('up','down')), 0)) ASC
		LIMIT $3`,
		probeType, since, limit)
	if err != nil {
		return nil, fmt.Errorf("get worst performers: %w", err)
	}
	out := make([]DomainUptime, 0, len(rows))
	for _, r := range rows {
		total := r.UpCount + r.DownCount
		pct := float64(0)
		if total > 0 {
			pct = float64(r.UpCount) / float64(total) * 100
		}
		out = append(out, DomainUptime{
			DomainID:  r.DomainID,
			FQDN:      r.FQDN,
			UptimePct: pct,
			UpCount:   r.UpCount,
			DownCount: r.DownCount,
		})
	}
	return out, nil
}

// GetUptimeCalendar returns per-day uptime percentages for the given domain
// over the specified year/month. Each calendar day of the month is returned;
// days with no data have UptimePct = -1.
func (s *AnalyticsService) GetUptimeCalendar(ctx context.Context, domainID int64, year int, month time.Month) ([]DayStatus, error) {
	loc := time.UTC
	start := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	end   := start.AddDate(0, 1, 0)

	type row struct {
		Day       time.Time `db:"day"`
		UpCount   int64     `db:"up_count"`
		DownCount int64     `db:"down_count"`
	}
	var rows []row
	err := s.db.SelectContext(ctx, &rows, `
		SELECT
		    date_trunc('day', measured_at)             AS day,
		    COUNT(*) FILTER (WHERE status = 'up')      AS up_count,
		    COUNT(*) FILTER (WHERE status = 'down')    AS down_count
		FROM probe_results
		WHERE domain_id=$1 AND probe_type='l1'
		  AND measured_at >= $2 AND measured_at < $3
		GROUP BY day
		ORDER BY day`,
		domainID, start, end)
	if err != nil {
		return nil, fmt.Errorf("get uptime calendar: %w", err)
	}

	// Build a map keyed by "YYYY-MM-DD".
	dataMap := make(map[string]float64)
	for _, r := range rows {
		total := r.UpCount + r.DownCount
		pct := float64(-1)
		if total > 0 {
			pct = float64(r.UpCount) / float64(total) * 100
		}
		dataMap[r.Day.Format("2006-01-02")] = pct
	}

	// Generate all days of the month.
	var out []DayStatus
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		pct, ok := dataMap[key]
		if !ok {
			pct = -1 // no data
		}
		out = append(out, DayStatus{Date: key, UptimePct: pct})
	}
	return out, nil
}
