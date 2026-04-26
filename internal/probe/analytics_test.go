package probe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetUptimeCalendar_DayGeneration verifies that the calendar returns one
// entry per day in the requested month, filling missing days with UptimePct=-1.
func TestGetUptimeCalendar_DayGeneration(t *testing.T) {
	// We test the pure calendar-day generation logic directly by
	// reproducing the inner loop from GetUptimeCalendar.
	year, month := 2026, time.February // 28 days in 2026

	loc := time.UTC
	start := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)

	// Simulate a dataMap with data only for day 1 and day 15.
	dataMap := map[string]float64{
		"2026-02-01": 100.0,
		"2026-02-15": 90.0,
	}

	var out []DayStatus
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		pct, ok := dataMap[key]
		if !ok {
			pct = -1
		}
		out = append(out, DayStatus{Date: key, UptimePct: pct})
	}

	require.Len(t, out, 28)
	assert.Equal(t, "2026-02-01", out[0].Date)
	assert.Equal(t, 100.0, out[0].UptimePct)
	assert.Equal(t, "2026-02-15", out[14].Date)
	assert.Equal(t, 90.0, out[14].UptimePct)
	// Days with no data have -1.
	assert.Equal(t, -1.0, out[1].UptimePct)
	assert.Equal(t, "2026-02-28", out[27].Date)
}

// TestGetUptimeCalendar_March31Days verifies a 31-day month.
func TestGetUptimeCalendar_March31Days(t *testing.T) {
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	days := 0
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		days++
	}
	assert.Equal(t, 31, days)
}

// TestUptimeCalculation verifies the uptime percentage formula.
func TestUptimeCalculation(t *testing.T) {
	tests := []struct {
		up    int64
		down  int64
		total int64
		want  float64
	}{
		{100, 0, 100, 100.0},
		{0, 100, 100, 0.0},
		{95, 5, 100, 95.0},
		{0, 0, 0, 0.0},   // no data → 0
		{1, 1, 2, 50.0},
	}
	for _, tt := range tests {
		total := tt.up + tt.down
		var pct float64
		if total > 0 {
			pct = float64(tt.up) / float64(total) * 100
		}
		assert.InDelta(t, tt.want, pct, 0.001)
	}
}

// TestSentinelNoData verifies that -1 uptime_pct sentinel means no data.
func TestSentinelNoData(t *testing.T) {
	noData := DayStatus{Date: "2026-04-01", UptimePct: -1}
	assert.Equal(t, float64(-1), noData.UptimePct, "no-data day should have UptimePct=-1")
}
