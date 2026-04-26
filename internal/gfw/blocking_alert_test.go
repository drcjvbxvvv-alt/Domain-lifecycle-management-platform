package gfw

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── classifyBlocking ──────────────────────────────────────────────────────────

func TestClassifyBlocking_Blocked(t *testing.T) {
	status, severity := classifyBlocking(0.90, BlockingDNS)
	assert.Equal(t, "blocked", status)
	assert.Equal(t, "P1", severity)
}

func TestClassifyBlocking_BlockedHighConfidence(t *testing.T) {
	status, severity := classifyBlocking(1.0, BlockingTCPIP)
	assert.Equal(t, "blocked", status)
	assert.Equal(t, "P1", severity)
}

func TestClassifyBlocking_PossiblyBlocked_Lower(t *testing.T) {
	status, severity := classifyBlocking(0.70, BlockingTLSSNI)
	assert.Equal(t, "possibly_blocked", status)
	assert.Equal(t, "P2", severity)
}

func TestClassifyBlocking_PossiblyBlocked_Upper(t *testing.T) {
	status, severity := classifyBlocking(0.89, BlockingHTTPDiff)
	assert.Equal(t, "possibly_blocked", status)
	assert.Equal(t, "P2", severity)
}

func TestClassifyBlocking_Borderline_P3(t *testing.T) {
	// 0.30–0.69 → no persistent blocking state, but fire P3 alert.
	status, severity := classifyBlocking(0.50, BlockingDNS)
	assert.Equal(t, "", status)
	assert.Equal(t, "P3", severity)
}

func TestClassifyBlocking_BelowThreshold(t *testing.T) {
	status, severity := classifyBlocking(0.20, BlockingDNS)
	assert.Equal(t, "", status)
	assert.Equal(t, "", severity)
}

func TestClassifyBlocking_ZeroConfidence(t *testing.T) {
	status, severity := classifyBlocking(0.0, BlockingDNS)
	assert.Equal(t, "", status)
	assert.Equal(t, "", severity)
}

func TestClassifyBlocking_NoBlockingType(t *testing.T) {
	// No blocking type → always None regardless of confidence.
	status, severity := classifyBlocking(0.99, BlockingNone)
	assert.Equal(t, "", status)
	assert.Equal(t, "", severity)
}

func TestClassifyBlocking_EmptyBlockingType(t *testing.T) {
	status, severity := classifyBlocking(0.95, "")
	assert.Equal(t, "", status)
	assert.Equal(t, "", severity)
}

// ── buildTitle ────────────────────────────────────────────────────────────────

func TestBuildTitle_DNS(t *testing.T) {
	title := buildTitle("example.com", BlockingDNS, 0.90)
	assert.Contains(t, title, "DNS")
	assert.Contains(t, title, "example.com")
	assert.Contains(t, title, "90%")
}

func TestBuildTitle_TCPIP(t *testing.T) {
	title := buildTitle("test.org", BlockingTCPIP, 0.75)
	assert.Contains(t, title, "TCP/IP")
	assert.Contains(t, title, "75%")
}

func TestBuildTitle_TLSSNI(t *testing.T) {
	title := buildTitle("foo.bar", BlockingTLSSNI, 0.50)
	assert.Contains(t, title, "TLS SNI")
}

func TestBuildTitle_HTTPFailure(t *testing.T) {
	title := buildTitle("foo.bar", BlockingHTTPFailure, 0.80)
	assert.Contains(t, title, "HTTP failure")
}

func TestBuildTitle_HTTPDiff(t *testing.T) {
	title := buildTitle("foo.bar", BlockingHTTPDiff, 0.70)
	assert.Contains(t, title, "HTTP content difference")
}

func TestBuildTitle_Unknown(t *testing.T) {
	title := buildTitle("x.com", "indeterminate", 0.60)
	assert.Contains(t, title, "blocking detected")
	assert.Contains(t, title, "x.com")
}

// ── EvaluateAndAlert (integration-ish, uses stubs) ───────────────────────────

// stubBlockingStore records calls to UpdateDomainBlockingStatus.
type stubBlockingStore struct {
	lastStatus     string
	lastType       string
	lastConfidence float64
	updateCalled   bool
}

func (s *stubBlockingStore) UpdateDomainBlockingStatus(
	_ interface{}, // context.Context
	_ int64,
	status, blockingType string,
	_ interface{}, // *time.Time
	confidence float64,
) error {
	s.lastStatus = status
	s.lastType = blockingType
	s.lastConfidence = confidence
	s.updateCalled = true
	return nil
}

// Verify confidence boundary at 0.90.
func TestClassifyBlocking_BoundaryAt90(t *testing.T) {
	// 0.90 is "blocked" (≥ 0.90).
	status, sev := classifyBlocking(0.90, BlockingDNS)
	assert.Equal(t, "blocked", status)
	assert.Equal(t, "P1", sev)

	// 0.8999... is "possibly_blocked" (< 0.90).
	status2, sev2 := classifyBlocking(0.8999, BlockingDNS)
	assert.Equal(t, "possibly_blocked", status2)
	assert.Equal(t, "P2", sev2)
}

// Verify confidence boundary at 0.70.
func TestClassifyBlocking_BoundaryAt70(t *testing.T) {
	status, sev := classifyBlocking(0.70, BlockingTCPIP)
	assert.Equal(t, "possibly_blocked", status)
	assert.Equal(t, "P2", sev)

	status2, sev2 := classifyBlocking(0.6999, BlockingTCPIP)
	assert.Equal(t, "", status2)
	assert.Equal(t, "P3", sev2)
}

// Verify confidence boundary at 0.30.
func TestClassifyBlocking_BoundaryAt30(t *testing.T) {
	// 0.30 → P3 (no persistent state).
	status, sev := classifyBlocking(0.30, BlockingDNS)
	assert.Equal(t, "", status)
	assert.Equal(t, "P3", sev)

	// 0.2999 → below threshold, no alert.
	status2, sev2 := classifyBlocking(0.2999, BlockingDNS)
	assert.Equal(t, "", status2)
	assert.Equal(t, "", sev2)
}
