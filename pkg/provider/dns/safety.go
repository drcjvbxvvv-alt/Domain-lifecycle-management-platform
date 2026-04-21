package dns

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ── Default thresholds (adopted from OctoDNS) ────────────────────────────────

const (
	DefaultUpdateThresholdPct = 0.3  // 30% of existing records
	DefaultDeleteThresholdPct = 0.3  // 30% of existing records
	DefaultMinExistingRecords = 10   // safety only kicks in above this count
	DefaultProtectRootNS      = true // NS @ changes always require force
)

// ── Plan types ───────────────────────────────────────────────────────────────

// ChangeAction describes what kind of change a PlanChange represents.
type ChangeAction string

const (
	ActionCreate ChangeAction = "create"
	ActionUpdate ChangeAction = "update"
	ActionDelete ChangeAction = "delete"
)

// PlanChange is a single DNS record change within a Plan.
type PlanChange struct {
	Action  ChangeAction `json:"action"`
	Before  *Record      `json:"before,omitempty"`  // nil for creates
	After   *Record      `json:"after,omitempty"`   // nil for deletes
	Summary string       `json:"summary,omitempty"` // human-readable diff line
}

// Plan is the result of comparing desired DNS state against existing records.
// It contains the full list of changes that would be applied.
type Plan struct {
	Zone     string       `json:"zone"`
	Creates  []PlanChange `json:"creates"`
	Updates  []PlanChange `json:"updates"`
	Deletes  []PlanChange `json:"deletes"`
	Checksum string       `json:"checksum,omitempty"` // SHA-256 of plan content (for stale-plan detection)
}

// TotalChanges returns the total number of changes in the plan.
func (p *Plan) TotalChanges() int {
	return len(p.Creates) + len(p.Updates) + len(p.Deletes)
}

// IsEmpty returns true if the plan has no changes.
func (p *Plan) IsEmpty() bool {
	return p.TotalChanges() == 0
}

// HasRootNSChange returns true if the plan contains any change
// to an NS record at the zone root ("@" or the zone name itself).
func (p *Plan) HasRootNSChange(zone string) bool {
	isRootNS := func(c PlanChange) bool {
		rec := c.After
		if rec == nil {
			rec = c.Before
		}
		if rec == nil {
			return false
		}
		if !strings.EqualFold(rec.Type, "NS") {
			return false
		}
		name := strings.TrimSuffix(strings.ToLower(rec.Name), ".")
		zoneName := strings.TrimSuffix(strings.ToLower(zone), ".")
		return name == "@" || name == "" || name == zoneName
	}

	for _, c := range p.Creates {
		if isRootNS(c) {
			return true
		}
	}
	for _, c := range p.Updates {
		if isRootNS(c) {
			return true
		}
	}
	for _, c := range p.Deletes {
		if isRootNS(c) {
			return true
		}
	}
	return false
}

// ── Safety config ────────────────────────────────────────────────────────────

// SafetyConfig holds thresholds for the safety check.
// All fields have sensible defaults — use DefaultSafetyConfig() to get them.
type SafetyConfig struct {
	UpdateThresholdPct float64 `json:"update_threshold_pct"` // e.g. 0.3 = 30%
	DeleteThresholdPct float64 `json:"delete_threshold_pct"` // e.g. 0.3 = 30%
	MinExistingRecords int     `json:"min_existing_records"` // safety skipped below this count
	ProtectRootNS      bool    `json:"protect_root_ns"`      // block root NS changes unless forced
}

// DefaultSafetyConfig returns the OctoDNS-based default safety configuration.
func DefaultSafetyConfig() SafetyConfig {
	return SafetyConfig{
		UpdateThresholdPct: DefaultUpdateThresholdPct,
		DeleteThresholdPct: DefaultDeleteThresholdPct,
		MinExistingRecords: DefaultMinExistingRecords,
		ProtectRootNS:      DefaultProtectRootNS,
	}
}

// ParseSafetyConfig extracts SafetyConfig from a dns_providers.config JSONB
// blob. Falls back to defaults for any missing or invalid fields.
//
// Expected JSONB structure:
//
//	{
//	    "zone_id": "...",
//	    "safety": {
//	        "update_threshold_pct": 0.3,
//	        "delete_threshold_pct": 0.3,
//	        "min_existing_records": 10,
//	        "protect_root_ns": true
//	    }
//	}
func ParseSafetyConfig(config json.RawMessage) SafetyConfig {
	cfg := DefaultSafetyConfig()

	if len(config) == 0 {
		return cfg
	}

	var wrapper struct {
		Safety *SafetyConfig `json:"safety"`
	}
	if err := json.Unmarshal(config, &wrapper); err != nil || wrapper.Safety == nil {
		return cfg
	}

	s := wrapper.Safety

	// Only override if the parsed value is valid (> 0 for percentages, > 0 for min records)
	if s.UpdateThresholdPct > 0 && s.UpdateThresholdPct <= 1.0 {
		cfg.UpdateThresholdPct = s.UpdateThresholdPct
	}
	if s.DeleteThresholdPct > 0 && s.DeleteThresholdPct <= 1.0 {
		cfg.DeleteThresholdPct = s.DeleteThresholdPct
	}
	if s.MinExistingRecords > 0 {
		cfg.MinExistingRecords = s.MinExistingRecords
	}
	// ProtectRootNS: always use the parsed value (it's a bool, both true/false are valid)
	cfg.ProtectRootNS = s.ProtectRootNS

	return cfg
}

// ── Safety result ────────────────────────────────────────────────────────────

// SafetyResult is returned by CheckSafety.
type SafetyResult struct {
	Passed          bool    `json:"passed"`
	Reason          string  `json:"reason,omitempty"`     // empty if passed
	UpdatePct       float64 `json:"update_pct"`           // actual update percentage
	DeletePct       float64 `json:"delete_pct"`           // actual delete percentage
	UpdateThreshold float64 `json:"update_threshold"`     // configured threshold
	DeleteThreshold float64 `json:"delete_threshold"`     // configured threshold
	ExistingCount   int     `json:"existing_count"`       // number of existing records
	RootNSChanged   bool    `json:"root_ns_changed"`      // plan touches root NS
	RequiresForce   bool    `json:"requires_force"`       // true when safety failed
}

// ── CheckSafety (pure function) ──────────────────────────────────────────────

// CheckSafety evaluates a Plan against existing records and a SafetyConfig.
// This is a pure function with no I/O — it only inspects the data passed in.
//
// Logic (from OctoDNS §3):
//  1. If len(existing) < MinExistingRecords → PASS (small zone)
//  2. updatePct = len(plan.Updates) / len(existing)
//     → if > UpdateThresholdPct → FAIL
//  3. deletePct = len(plan.Deletes) / len(existing)
//     → if > DeleteThresholdPct → FAIL
//  4. If ProtectRootNS && plan touches NS at "@" → FAIL
//  5. Otherwise → PASS
func CheckSafety(plan *Plan, existing []Record, config SafetyConfig) *SafetyResult {
	result := &SafetyResult{
		Passed:          true,
		UpdateThreshold: config.UpdateThresholdPct,
		DeleteThreshold: config.DeleteThresholdPct,
		ExistingCount:   len(existing),
	}

	// Check root NS change first (this flag is always reported regardless of zone size)
	rootNSChanged := plan.HasRootNSChange(plan.Zone)
	result.RootNSChanged = rootNSChanged

	existingCount := len(existing)

	// Calculate percentages (even for small zones, for informational purposes)
	if existingCount > 0 {
		result.UpdatePct = float64(len(plan.Updates)) / float64(existingCount)
		result.DeletePct = float64(len(plan.Deletes)) / float64(existingCount)
	}

	// Step 1: small zone bypass — safety not needed
	if existingCount < config.MinExistingRecords {
		// Still check root NS even for small zones
		if config.ProtectRootNS && rootNSChanged {
			result.Passed = false
			result.RequiresForce = true
			result.Reason = "root NS change requires force"
			return result
		}
		return result
	}

	// Step 2: update threshold
	if result.UpdatePct > config.UpdateThresholdPct {
		result.Passed = false
		result.RequiresForce = true
		result.Reason = fmt.Sprintf(
			"would update %.0f%% of records (threshold: %.0f%%)",
			result.UpdatePct*100, config.UpdateThresholdPct*100,
		)
		return result
	}

	// Step 3: delete threshold
	if result.DeletePct > config.DeleteThresholdPct {
		result.Passed = false
		result.RequiresForce = true
		result.Reason = fmt.Sprintf(
			"would delete %.0f%% of records (threshold: %.0f%%)",
			result.DeletePct*100, config.DeleteThresholdPct*100,
		)
		return result
	}

	// Step 4: root NS protection
	if config.ProtectRootNS && rootNSChanged {
		result.Passed = false
		result.RequiresForce = true
		result.Reason = "root NS change requires force"
		return result
	}

	return result
}
