package dns

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// makeRecords creates n dummy A records for testing.
func makeRecords(n int) []Record {
	records := make([]Record, n)
	for i := range records {
		records[i] = Record{
			ID:      fmt.Sprintf("rec-%d", i),
			Type:    "A",
			Name:    "example.com",
			Content: fmt.Sprintf("1.2.3.%d", i%256),
			TTL:     300,
		}
	}
	return records
}

// makeDeletes creates n PlanChange entries with ActionDelete.
func makeDeletes(n int) []PlanChange {
	changes := make([]PlanChange, n)
	for i := range changes {
		rec := &Record{ID: fmt.Sprintf("del-%d", i), Type: "A", Name: "example.com", Content: fmt.Sprintf("1.2.3.%d", i)}
		changes[i] = PlanChange{Action: ActionDelete, Before: rec}
	}
	return changes
}

// makeUpdates creates n PlanChange entries with ActionUpdate.
func makeUpdates(n int) []PlanChange {
	changes := make([]PlanChange, n)
	for i := range changes {
		before := &Record{ID: fmt.Sprintf("upd-%d", i), Type: "A", Name: "example.com", Content: fmt.Sprintf("1.2.3.%d", i)}
		after := &Record{ID: fmt.Sprintf("upd-%d", i), Type: "A", Name: "example.com", Content: fmt.Sprintf("9.9.9.%d", i)}
		changes[i] = PlanChange{Action: ActionUpdate, Before: before, After: after}
	}
	return changes
}

// ── Acceptance criteria tests ────────────────────────────────────────────────

// AC: Zone with 20 records, plan deletes 8 → Passed=false, reason contains "40%"
func TestCheckSafety_DeleteExceedsThreshold(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone:    "example.com",
		Deletes: makeDeletes(8),
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.False(t, result.Passed)
	assert.True(t, result.RequiresForce)
	assert.Contains(t, result.Reason, "40%")
	assert.Contains(t, result.Reason, "delete")
	assert.InDelta(t, 0.4, result.DeletePct, 0.001)
}

// AC: Zone with 20 records, plan deletes 5 → Passed=true
func TestCheckSafety_DeleteWithinThreshold(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone:    "example.com",
		Deletes: makeDeletes(5),
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed)
	assert.Empty(t, result.Reason)
	assert.InDelta(t, 0.25, result.DeletePct, 0.001)
}

// AC: Zone with 5 records, plan deletes 4 → Passed=true (below MinExisting)
func TestCheckSafety_SmallZoneBypass(t *testing.T) {
	existing := makeRecords(5)
	plan := &Plan{
		Zone:    "example.com",
		Deletes: makeDeletes(4),
	}
	config := DefaultSafetyConfig() // MinExistingRecords=10

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed, "small zone should bypass safety")
	assert.Equal(t, 5, result.ExistingCount)
	// Percentage still reported for informational purposes
	assert.InDelta(t, 0.8, result.DeletePct, 0.001)
}

// AC: Plan changes NS @ record → RootNSChanged=true, requires force
func TestCheckSafety_RootNSChangeBlocked(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone: "example.com",
		Updates: []PlanChange{
			{
				Action: ActionUpdate,
				Before: &Record{Type: "NS", Name: "@", Content: "ns1.old.com"},
				After:  &Record{Type: "NS", Name: "@", Content: "ns1.new.com"},
			},
		},
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.False(t, result.Passed)
	assert.True(t, result.RootNSChanged)
	assert.True(t, result.RequiresForce)
	assert.Contains(t, result.Reason, "root NS")
}

// AC: Custom threshold: provider has delete_threshold_pct: 0.5 → uses 50%
func TestCheckSafety_CustomThreshold(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone:    "example.com",
		Deletes: makeDeletes(8), // 40% — would fail at default 30% but pass at custom 50%
	}
	config := SafetyConfig{
		UpdateThresholdPct: 0.3,
		DeleteThresholdPct: 0.5, // custom: 50%
		MinExistingRecords: 10,
		ProtectRootNS:      true,
	}

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed, "40% deletes should pass with 50% threshold")
	assert.InDelta(t, 0.5, result.DeleteThreshold, 0.001)
}

// Update threshold exceeded
func TestCheckSafety_UpdateExceedsThreshold(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone:    "example.com",
		Updates: makeUpdates(8), // 40% > 30%
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.False(t, result.Passed)
	assert.Contains(t, result.Reason, "update")
	assert.Contains(t, result.Reason, "40%")
	assert.InDelta(t, 0.4, result.UpdatePct, 0.001)
}

// Both updates and deletes within threshold → pass
func TestCheckSafety_BothWithinThreshold(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone:    "example.com",
		Updates: makeUpdates(5), // 25%
		Deletes: makeDeletes(5), // 25%
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed)
}

// Empty plan → passed (empty plans are rejected at the handler level, not in safety)
func TestCheckSafety_EmptyPlan(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{Zone: "example.com"}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed)
	assert.InDelta(t, 0.0, result.UpdatePct, 0.001)
	assert.InDelta(t, 0.0, result.DeletePct, 0.001)
}

// Empty existing → pass (small zone bypass)
func TestCheckSafety_NoExistingRecords(t *testing.T) {
	plan := &Plan{
		Zone:    "example.com",
		Creates: []PlanChange{{Action: ActionCreate, After: &Record{Type: "A", Name: "example.com", Content: "1.2.3.4"}}},
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, nil, config)

	assert.True(t, result.Passed)
	assert.Equal(t, 0, result.ExistingCount)
}

// Root NS change blocked even on small zone
func TestCheckSafety_RootNSBlockedOnSmallZone(t *testing.T) {
	existing := makeRecords(3)
	plan := &Plan{
		Zone: "example.com",
		Deletes: []PlanChange{
			{Action: ActionDelete, Before: &Record{Type: "NS", Name: "example.com", Content: "ns1.old.com"}},
		},
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.False(t, result.Passed, "root NS protection applies even on small zones")
	assert.True(t, result.RootNSChanged)
	assert.Contains(t, result.Reason, "root NS")
}

// Root NS protection disabled → pass
func TestCheckSafety_RootNSProtectionDisabled(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone: "example.com",
		Updates: []PlanChange{
			{Action: ActionUpdate, Before: &Record{Type: "NS", Name: "@"}, After: &Record{Type: "NS", Name: "@", Content: "ns1.new.com"}},
		},
	}
	config := DefaultSafetyConfig()
	config.ProtectRootNS = false

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed, "root NS change allowed when protection is disabled")
	assert.True(t, result.RootNSChanged, "RootNSChanged still reported")
}

// Exactly at threshold boundary → pass (threshold is strict >, not >=)
func TestCheckSafety_ExactlyAtThreshold(t *testing.T) {
	existing := makeRecords(10)
	plan := &Plan{
		Zone:    "example.com",
		Deletes: makeDeletes(3), // 30% == threshold, should pass
	}
	config := DefaultSafetyConfig() // 30%

	result := CheckSafety(plan, existing, config)

	assert.True(t, result.Passed, "exactly at threshold should pass (> not >=)")
}

// Just over threshold → fail
func TestCheckSafety_JustOverThreshold(t *testing.T) {
	existing := makeRecords(10)
	plan := &Plan{
		Zone:    "example.com",
		Deletes: makeDeletes(4), // 40% > 30%
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.False(t, result.Passed)
}

// Update check runs before delete check (first failure wins)
func TestCheckSafety_UpdateCheckRunsFirst(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{
		Zone:    "example.com",
		Updates: makeUpdates(8), // 40% > 30%
		Deletes: makeDeletes(8), // 40% > 30%
	}
	config := DefaultSafetyConfig()

	result := CheckSafety(plan, existing, config)

	assert.False(t, result.Passed)
	assert.Contains(t, result.Reason, "update", "update check should trigger first")
}

// ── Plan type tests ──────────────────────────────────────────────────────────

func TestPlan_TotalChanges(t *testing.T) {
	plan := &Plan{
		Creates: []PlanChange{{}, {}},
		Updates: []PlanChange{{}},
		Deletes: []PlanChange{{}, {}, {}},
	}
	assert.Equal(t, 6, plan.TotalChanges())
}

func TestPlan_IsEmpty(t *testing.T) {
	assert.True(t, (&Plan{}).IsEmpty())
	assert.False(t, (&Plan{Creates: []PlanChange{{}}}).IsEmpty())
}

func TestPlan_HasRootNSChange(t *testing.T) {
	tests := []struct {
		name   string
		zone   string
		record Record
		want   bool
	}{
		{"NS at @", "example.com", Record{Type: "NS", Name: "@"}, true},
		{"NS at zone name", "example.com", Record{Type: "NS", Name: "example.com"}, true},
		{"NS at zone name with dot", "example.com", Record{Type: "NS", Name: "example.com."}, true},
		{"NS at subdomain", "example.com", Record{Type: "NS", Name: "sub.example.com"}, false},
		{"A at @", "example.com", Record{Type: "A", Name: "@"}, false},
		{"NS at empty name", "example.com", Record{Type: "NS", Name: ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &Plan{
				Zone:    tt.zone,
				Updates: []PlanChange{{Action: ActionUpdate, After: &tt.record}},
			}
			assert.Equal(t, tt.want, plan.HasRootNSChange(tt.zone))
		})
	}
}

// ── ParseSafetyConfig tests ──────────────────────────────────────────────────

func TestParseSafetyConfig_Empty(t *testing.T) {
	cfg := ParseSafetyConfig(nil)
	assert.Equal(t, DefaultSafetyConfig(), cfg)
}

func TestParseSafetyConfig_NoSafetyKey(t *testing.T) {
	raw := json.RawMessage(`{"zone_id": "abc123"}`)
	cfg := ParseSafetyConfig(raw)
	assert.Equal(t, DefaultSafetyConfig(), cfg)
}

func TestParseSafetyConfig_CustomThresholds(t *testing.T) {
	raw := json.RawMessage(`{
		"zone_id": "abc123",
		"safety": {
			"update_threshold_pct": 0.5,
			"delete_threshold_pct": 0.4,
			"min_existing_records": 20,
			"protect_root_ns": false
		}
	}`)
	cfg := ParseSafetyConfig(raw)

	assert.InDelta(t, 0.5, cfg.UpdateThresholdPct, 0.001)
	assert.InDelta(t, 0.4, cfg.DeleteThresholdPct, 0.001)
	assert.Equal(t, 20, cfg.MinExistingRecords)
	assert.False(t, cfg.ProtectRootNS)
}

func TestParseSafetyConfig_PartialOverride(t *testing.T) {
	raw := json.RawMessage(`{
		"safety": {
			"delete_threshold_pct": 0.5
		}
	}`)
	cfg := ParseSafetyConfig(raw)

	// Only delete threshold should change; rest are defaults
	assert.InDelta(t, DefaultUpdateThresholdPct, cfg.UpdateThresholdPct, 0.001)
	assert.InDelta(t, 0.5, cfg.DeleteThresholdPct, 0.001)
	assert.Equal(t, DefaultMinExistingRecords, cfg.MinExistingRecords)
}

func TestParseSafetyConfig_InvalidValues(t *testing.T) {
	// Threshold > 1.0 is invalid — should use default
	raw := json.RawMessage(`{
		"safety": {
			"update_threshold_pct": 1.5,
			"delete_threshold_pct": -0.1,
			"min_existing_records": -5
		}
	}`)
	cfg := ParseSafetyConfig(raw)

	assert.InDelta(t, DefaultUpdateThresholdPct, cfg.UpdateThresholdPct, 0.001)
	assert.InDelta(t, DefaultDeleteThresholdPct, cfg.DeleteThresholdPct, 0.001)
	assert.Equal(t, DefaultMinExistingRecords, cfg.MinExistingRecords)
}

func TestParseSafetyConfig_MalformedJSON(t *testing.T) {
	raw := json.RawMessage(`{not valid json}`)
	cfg := ParseSafetyConfig(raw)
	assert.Equal(t, DefaultSafetyConfig(), cfg)
}

// ── DefaultSafetyConfig test ─────────────────────────────────────────────────

func TestDefaultSafetyConfig(t *testing.T) {
	cfg := DefaultSafetyConfig()

	assert.InDelta(t, 0.3, cfg.UpdateThresholdPct, 0.001)
	assert.InDelta(t, 0.3, cfg.DeleteThresholdPct, 0.001)
	assert.Equal(t, 10, cfg.MinExistingRecords)
	assert.True(t, cfg.ProtectRootNS)
}

// ── race safety ──────────────────────────────────────────────────────────────

// CheckSafety is a pure function — this test exists for -race -count=50 compliance.
func TestCheckSafety_RaceSafe(t *testing.T) {
	existing := makeRecords(20)
	plan := &Plan{Zone: "example.com", Deletes: makeDeletes(5)}
	config := DefaultSafetyConfig()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			r := CheckSafety(plan, existing, config)
			require.True(t, r.Passed)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
