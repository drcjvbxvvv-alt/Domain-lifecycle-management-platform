package statuspage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ─── worstStatus ─────────────────────────────────────────────────────────────

func TestWorstStatus(t *testing.T) {
	tests := []struct {
		name     string
		monitors []MonitorStatus
		want     string
	}{
		{
			name:     "all up",
			monitors: []MonitorStatus{{Status: "up"}, {Status: "up"}},
			want:     "up",
		},
		{
			name:     "one down",
			monitors: []MonitorStatus{{Status: "up"}, {Status: "down"}},
			want:     "down",
		},
		{
			name:     "one maintenance",
			monitors: []MonitorStatus{{Status: "up"}, {Status: "maintenance"}},
			want:     "maintenance",
		},
		{
			name:     "down beats maintenance",
			monitors: []MonitorStatus{{Status: "maintenance"}, {Status: "down"}},
			want:     "down",
		},
		{
			name:     "unknown",
			monitors: []MonitorStatus{{Status: "unknown"}},
			want:     "unknown",
		},
		{
			name:     "empty",
			monitors: nil,
			want:     "up",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, worstStatus(tt.monitors))
		})
	}
}

// ─── computeOverall ───────────────────────────────────────────────────────────

func TestComputeOverall(t *testing.T) {
	tests := []struct {
		name   string
		groups []GroupStatus
		want   OverallStatus
	}{
		{
			name:   "empty",
			groups: nil,
			want:   OverallOperational,
		},
		{
			name: "all up",
			groups: []GroupStatus{
				{Monitors: []MonitorStatus{{Status: "up"}, {Status: "up"}}},
			},
			want: OverallOperational,
		},
		{
			name: "one maintenance no down",
			groups: []GroupStatus{
				{Monitors: []MonitorStatus{{Status: "up"}, {Status: "maintenance"}}},
			},
			want: OverallMaintenance,
		},
		{
			name: "one down out of four — degraded",
			groups: []GroupStatus{
				{Monitors: []MonitorStatus{
					{Status: "up"}, {Status: "up"}, {Status: "up"}, {Status: "down"},
				}},
			},
			want: OverallDegraded,
		},
		{
			name: "two down out of four — outage (50%)",
			groups: []GroupStatus{
				{Monitors: []MonitorStatus{
					{Status: "up"}, {Status: "up"}, {Status: "down"}, {Status: "down"},
				}},
			},
			want: OverallOutage,
		},
		{
			name: "all down",
			groups: []GroupStatus{
				{Monitors: []MonitorStatus{{Status: "down"}, {Status: "down"}}},
			},
			want: OverallOutage,
		},
		{
			name: "down takes priority over maintenance",
			groups: []GroupStatus{
				{Monitors: []MonitorStatus{
					{Status: "maintenance"}, {Status: "down"}, {Status: "up"},
				}},
			},
			// 1 down out of 3 → < 50% → degraded
			want: OverallDegraded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeOverall(tt.groups))
		})
	}
}

// ─── validSeverity ────────────────────────────────────────────────────────────

func TestValidSeverity(t *testing.T) {
	assert.True(t, validSeverity("info"))
	assert.True(t, validSeverity("warning"))
	assert.True(t, validSeverity("danger"))
	assert.False(t, validSeverity("critical"))
	assert.False(t, validSeverity(""))
	assert.False(t, validSeverity("INFO"))
}
