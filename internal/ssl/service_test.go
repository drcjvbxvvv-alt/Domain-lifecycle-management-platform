package ssl

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputeSSLStatus(t *testing.T) {
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		want      string
	}{
		{
			name:      "active — 90 days left",
			expiresAt: now.Add(90 * 24 * time.Hour),
			want:      StatusActive,
		},
		{
			name:      "active — exactly 31 days left",
			expiresAt: now.Add(31 * 24 * time.Hour),
			want:      StatusActive,
		},
		{
			name:      "expiring — exactly 30 days left",
			expiresAt: now.Add(30 * 24 * time.Hour),
			want:      StatusExpiring,
		},
		{
			name:      "expiring — 7 days left",
			expiresAt: now.Add(7 * 24 * time.Hour),
			want:      StatusExpiring,
		},
		{
			name:      "expiring — 1 day left",
			expiresAt: now.Add(24 * time.Hour),
			want:      StatusExpiring,
		},
		{
			name:      "expiring — 1 second left",
			expiresAt: now.Add(time.Second),
			want:      StatusExpiring,
		},
		{
			name:      "expired — exactly now",
			expiresAt: now,
			want:      StatusExpired,
		},
		{
			name:      "expired — 1 second ago",
			expiresAt: now.Add(-time.Second),
			want:      StatusExpired,
		},
		{
			name:      "expired — 30 days ago",
			expiresAt: now.Add(-30 * 24 * time.Hour),
			want:      StatusExpired,
		},
		{
			name:      "expired — 1 year ago",
			expiresAt: now.Add(-365 * 24 * time.Hour),
			want:      StatusExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeSSLStatusAt(tt.expiresAt, now)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatusConstants(t *testing.T) {
	// Verify the status constants are distinct strings — they map to a DB CHECK constraint.
	statuses := []string{StatusActive, StatusExpiring, StatusExpired, StatusRevoked}
	seen := make(map[string]bool)
	for _, s := range statuses {
		assert.False(t, seen[s], "duplicate status constant: %q", s)
		assert.NotEmpty(t, s)
		seen[s] = true
	}
}

func TestExpiryThreshold(t *testing.T) {
	// Boundary: 30 days + 1 nanosecond → active
	now := time.Now()
	justOver30d := now.Add(ExpiryThreshold30d + time.Nanosecond)
	assert.Equal(t, StatusActive, computeSSLStatusAt(justOver30d, now))

	// Boundary: exactly 30 days → expiring
	exactly30d := now.Add(ExpiryThreshold30d)
	assert.Equal(t, StatusExpiring, computeSSLStatusAt(exactly30d, now))
}
