package maintenance

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"domain-platform/store/postgres"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ─── coversSingle ─────────────────────────────────────────────────────────────

func TestCoversSingle(t *testing.T) {
	start := mustTime("2026-04-26T02:00:00Z")
	end := mustTime("2026-04-26T04:00:00Z")
	w := &postgres.MaintenanceWindow{
		Strategy: "single",
		StartAt:  &start,
		EndAt:    &end,
	}

	tests := []struct {
		name    string
		t       time.Time
		covered bool
	}{
		{"before window", mustTime("2026-04-26T01:59:59Z"), false},
		{"at start", mustTime("2026-04-26T02:00:00Z"), true},
		{"inside window", mustTime("2026-04-26T03:00:00Z"), true},
		{"at end", mustTime("2026-04-26T04:00:00Z"), true},
		{"after window", mustTime("2026-04-26T04:00:01Z"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowCoversTime(w, tt.t)
			require.NoError(t, err)
			assert.Equal(t, tt.covered, got)
		})
	}
}

func TestCoversSingle_NilBounds(t *testing.T) {
	w := &postgres.MaintenanceWindow{Strategy: "single"}
	got, err := windowCoversTime(w, time.Now())
	require.NoError(t, err)
	assert.False(t, got)
}

// ─── coversWeekly ─────────────────────────────────────────────────────────────

func TestCoversWeekly(t *testing.T) {
	// Monday 2026-04-27 is a Monday (weekday=1).
	recurrence := rawJSON(WeeklyRecurrence{
		Weekdays:        []time.Weekday{time.Monday, time.Friday},
		StartTime:       "02:00",
		DurationMinutes: 120,
		Timezone:        "UTC",
	})
	w := &postgres.MaintenanceWindow{
		Strategy:   "recurring_weekly",
		Recurrence: recurrence,
	}

	tests := []struct {
		name    string
		t       time.Time
		covered bool
	}{
		{"Monday inside window", mustTime("2026-04-27T02:30:00Z"), true},
		{"Monday before window", mustTime("2026-04-27T01:59:00Z"), false},
		{"Monday at window start", mustTime("2026-04-27T02:00:00Z"), true},
		{"Monday at window end", mustTime("2026-04-27T04:00:00Z"), true},
		{"Monday after window", mustTime("2026-04-27T04:01:00Z"), false},
		{"Friday inside window", mustTime("2026-05-01T03:00:00Z"), true},
		{"Tuesday not scheduled", mustTime("2026-04-28T03:00:00Z"), false},
		{"Wednesday not scheduled", mustTime("2026-04-29T03:00:00Z"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowCoversTime(w, tt.t)
			require.NoError(t, err)
			assert.Equal(t, tt.covered, got)
		})
	}
}

func TestCoversWeekly_TimezoneOffset(t *testing.T) {
	// 02:00 UTC+8 = 18:00 UTC previous day.
	recurrence := rawJSON(WeeklyRecurrence{
		Weekdays:        []time.Weekday{time.Tuesday},
		StartTime:       "02:00",
		DurationMinutes: 60,
		Timezone:        "Asia/Taipei",
	})
	w := &postgres.MaintenanceWindow{
		Strategy:   "recurring_weekly",
		Recurrence: recurrence,
	}
	// Tuesday 2026-04-28 02:30 Asia/Taipei = Monday 2026-04-27 18:30 UTC
	inWindow := mustTime("2026-04-27T18:30:00Z")
	got, err := windowCoversTime(w, inWindow)
	require.NoError(t, err)
	assert.True(t, got)

	// Tuesday 2026-04-28 01:30 Asia/Taipei = Monday 2026-04-27 17:30 UTC → before window
	beforeWindow := mustTime("2026-04-27T17:30:00Z")
	got, err = windowCoversTime(w, beforeWindow)
	require.NoError(t, err)
	assert.False(t, got)
}

func TestCoversWeekly_MissingRecurrence(t *testing.T) {
	w := &postgres.MaintenanceWindow{Strategy: "recurring_weekly"}
	_, err := windowCoversTime(w, time.Now())
	assert.Error(t, err)
}

// ─── coversMonthly ────────────────────────────────────────────────────────────

func TestCoversMonthly(t *testing.T) {
	recurrence := rawJSON(map[string]any{
		"day_of_month":     15,
		"start_time":       "03:00",
		"duration_minutes": 90,
		"timezone":         "UTC",
	})
	w := &postgres.MaintenanceWindow{
		Strategy:   "recurring_monthly",
		Recurrence: recurrence,
	}

	tests := []struct {
		name    string
		t       time.Time
		covered bool
	}{
		{"15th inside", mustTime("2026-04-15T03:30:00Z"), true},
		{"15th before", mustTime("2026-04-15T02:59:00Z"), false},
		{"15th after", mustTime("2026-04-15T04:31:00Z"), false},
		{"14th wrong day", mustTime("2026-04-14T03:30:00Z"), false},
		{"16th wrong day", mustTime("2026-04-16T03:30:00Z"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowCoversTime(w, tt.t)
			require.NoError(t, err)
			assert.Equal(t, tt.covered, got)
		})
	}
}

// ─── coversCron ───────────────────────────────────────────────────────────────

func TestCoversCron(t *testing.T) {
	// "0 2 * * 1" = Monday at 02:00
	recurrence := rawJSON(CronRecurrence{
		Expression:      "0 2 * * 1",
		DurationMinutes: 120,
		Timezone:        "UTC",
	})
	w := &postgres.MaintenanceWindow{
		Strategy:   "cron",
		Recurrence: recurrence,
	}

	tests := []struct {
		name    string
		t       time.Time
		covered bool
	}{
		{"Monday 02:30 inside", mustTime("2026-04-27T02:30:00Z"), true},
		{"Monday 01:59 before", mustTime("2026-04-27T01:59:00Z"), false},
		{"Monday 04:01 after", mustTime("2026-04-27T04:01:00Z"), false},
		{"Tuesday 02:30 wrong day", mustTime("2026-04-28T02:30:00Z"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowCoversTime(w, tt.t)
			require.NoError(t, err)
			assert.Equal(t, tt.covered, got)
		})
	}
}

func TestCoversCron_InvalidExpression(t *testing.T) {
	recurrence := rawJSON(CronRecurrence{Expression: "bad", DurationMinutes: 60})
	w := &postgres.MaintenanceWindow{Strategy: "cron", Recurrence: recurrence}
	_, err := windowCoversTime(w, time.Now())
	assert.Error(t, err)
}

// ─── validateStrategy ─────────────────────────────────────────────────────────

func TestValidateStrategy(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)

	rec := rawJSON(WeeklyRecurrence{
		Weekdays:        []time.Weekday{time.Monday},
		StartTime:       "02:00",
		DurationMinutes: 60,
		Timezone:        "UTC",
	})

	tests := []struct {
		name       string
		strategy   string
		startAt    *time.Time
		endAt      *time.Time
		recurrence json.RawMessage
		wantErr    bool
	}{
		{"valid single", "single", &now, &future, nil, false},
		{"single missing start", "single", nil, &future, nil, true},
		{"single missing end", "single", &now, nil, nil, true},
		{"single end before start", "single", &future, &now, nil, true},
		{"valid recurring_weekly", "recurring_weekly", nil, nil, rec, false},
		{"recurring_weekly no recurrence", "recurring_weekly", nil, nil, nil, true},
		{"valid cron", "cron", nil, nil, rawJSON(CronRecurrence{Expression: "0 2 * * 1", DurationMinutes: 60}), false},
		{"cron no recurrence", "cron", nil, nil, nil, true},
		{"unknown strategy", "unknown", nil, nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStrategy(tt.strategy, tt.startAt, tt.endAt, tt.recurrence)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ─── matchCronField ───────────────────────────────────────────────────────────

func TestMatchCronField(t *testing.T) {
	assert.True(t, matchCronField("*", 5))
	assert.True(t, matchCronField("5", 5))
	assert.False(t, matchCronField("5", 4))
	assert.True(t, matchCronField("1,3,5", 3))
	assert.False(t, matchCronField("1,3,5", 2))
}

// ─── parseHHMM ────────────────────────────────────────────────────────────────

func TestParseHHMM(t *testing.T) {
	ref := mustTime("2026-04-27T00:00:00Z")
	got, err := parseHHMM("14:30", ref)
	require.NoError(t, err)
	assert.Equal(t, 14, got.Hour())
	assert.Equal(t, 30, got.Minute())

	_, err = parseHHMM("25:00", ref)
	assert.Error(t, err)

	_, err = parseHHMM("badformat", ref)
	assert.Error(t, err)
}
