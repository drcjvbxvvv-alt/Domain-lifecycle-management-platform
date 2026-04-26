package gfw

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"domain-platform/pkg/probeprotocol"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func probeM(fqdn string, dns *probeprotocol.DNSResult, tcps []probeprotocol.TCPResult, tlss []probeprotocol.TLSResult, http *probeprotocol.HTTPResult) *probeprotocol.Measurement {
	return &probeprotocol.Measurement{
		DomainID:   1,
		FQDN:       fqdn,
		NodeID:     "cn-beijing-01",
		NodeRole:   probeprotocol.RoleProbe,
		DNS:        dns,
		TCP:        tcps,
		TLS:        tlss,
		HTTP:       http,
		MeasuredAt: time.Now(),
	}
}

func controlM(fqdn string, dns *probeprotocol.DNSResult, tcps []probeprotocol.TCPResult, tlss []probeprotocol.TLSResult, http *probeprotocol.HTTPResult) *probeprotocol.Measurement {
	return &probeprotocol.Measurement{
		DomainID:   1,
		FQDN:       fqdn,
		NodeID:     "hk-01",
		NodeRole:   probeprotocol.RoleControl,
		DNS:        dns,
		TCP:        tcps,
		TLS:        tlss,
		HTTP:       http,
		MeasuredAt: time.Now(),
	}
}

func newAnalyzer() *Analyzer {
	return NewAnalyzer(DefaultASNDatabase())
}

// ── DNS layer tests ───────────────────────────────────────────────────────────

func TestClassify_DNSBogonIP(t *testing.T) {
	probe := probeM("example.com",
		&probeprotocol.DNSResult{
			Answers:  []string{"1.2.3.4"}, // known bogon
			IsBogon:  true,
			DurationMS: 3,
		},
		nil, nil, nil)

	v := newAnalyzer().Classify(probe, nil, 0.3)

	assert.Equal(t, BlockingDNS, v.Blocking)
	assert.False(t, v.Accessible)
	assert.Equal(t, DNSInconsistent, v.DNSConsistency)
	assert.True(t, v.Detail.ProbeIsBogon)
}

func TestClassify_DNSInjected(t *testing.T) {
	probe := probeM("blocked.com",
		&probeprotocol.DNSResult{
			Answers:    []string{"8.7.198.45"},
			IsInjected: true,
			DurationMS: 2,
		},
		nil, nil, nil)

	v := newAnalyzer().Classify(probe, nil, 0.3)

	assert.Equal(t, BlockingDNS, v.Blocking)
	assert.True(t, v.Detail.ProbeIsInjected)
}

func TestClassify_DNSMismatchWithControl(t *testing.T) {
	// Probe resolves to GFW IP, control resolves to real IP — no overlap.
	probe := probeM("google.com",
		&probeprotocol.DNSResult{Answers: []string{"243.185.187.39"}, DurationMS: 50},
		nil, nil, nil)
	ctrl := controlM("google.com",
		&probeprotocol.DNSResult{Answers: []string{"142.250.80.46"}, DurationMS: 30},
		nil, nil, nil)

	v := newAnalyzer().Classify(probe, ctrl, 0.5)

	assert.Equal(t, BlockingDNS, v.Blocking)
	assert.Equal(t, DNSInconsistent, v.DNSConsistency)
}

func TestClassify_DNSConsistentOverlapWithControl(t *testing.T) {
	// Both probe and control resolve to the same IP — no DNS blocking.
	probe := probeM("example.com",
		&probeprotocol.DNSResult{Answers: []string{"93.184.216.34"}, DurationMS: 50},
		[]probeprotocol.TCPResult{{IP: "93.184.216.34", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "93.184.216.34", SNI: "example.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 1200, Title: "Example Domain"},
	)
	ctrl := controlM("example.com",
		&probeprotocol.DNSResult{Answers: []string{"93.184.216.34"}, DurationMS: 30},
		[]probeprotocol.TCPResult{{IP: "93.184.216.34", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "93.184.216.34", SNI: "example.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 1200, Title: "Example Domain"},
	)

	v := newAnalyzer().Classify(probe, ctrl, 0.9)

	assert.Equal(t, BlockingNone, v.Blocking)
	assert.True(t, v.Accessible)
	assert.Equal(t, DNSConsistent, v.DNSConsistency)
}

func TestClassify_DNSCDNDifferentIPsNotBlocking(t *testing.T) {
	// Probe and control get different Cloudflare IPs — this is geo-routing, not blocking.
	probe := probeM("cdn-site.com",
		&probeprotocol.DNSResult{Answers: []string{"104.16.0.1"}, DurationMS: 40}, // Cloudflare
		[]probeprotocol.TCPResult{{IP: "104.16.0.1", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "104.16.0.1", SNI: "cdn-site.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 5000, Title: "Welcome"},
	)
	ctrl := controlM("cdn-site.com",
		&probeprotocol.DNSResult{Answers: []string{"104.24.0.1"}, DurationMS: 35}, // different Cloudflare IP
		[]probeprotocol.TCPResult{{IP: "104.24.0.1", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "104.24.0.1", SNI: "cdn-site.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 5000, Title: "Welcome"},
	)

	v := newAnalyzer().Classify(probe, ctrl, 0.7)

	assert.Equal(t, BlockingNone, v.Blocking)
	assert.True(t, v.Accessible)
	assert.Equal(t, "cloudflare", v.Detail.CDNOwner)
}

func TestClassify_DNSErrorAtProbeNotControl(t *testing.T) {
	probe := probeM("blocked-nxdomain.com",
		&probeprotocol.DNSResult{Error: "NXDOMAIN", DurationMS: 60},
		nil, nil, nil)
	ctrl := controlM("blocked-nxdomain.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		nil, nil, nil)

	v := newAnalyzer().Classify(probe, ctrl, 0.5)

	assert.Equal(t, BlockingDNS, v.Blocking)
}

// ── TCP layer tests ───────────────────────────────────────────────────────────

func TestClassify_TCPBlockedProbeFailsControlSucceeds(t *testing.T) {
	probe := probeM("tcp-blocked.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: false, Error: "connection_refused"}},
		nil, nil)
	ctrl := controlM("tcp-blocked.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		nil, nil)

	v := newAnalyzer().Classify(probe, ctrl, 0.7)

	assert.Equal(t, BlockingTCPIP, v.Blocking)
	assert.False(t, v.Accessible)
	assert.False(t, v.Detail.ProbeTCPSuccess)
	assert.True(t, v.Detail.ControlTCPSuccess)
}

func TestClassify_TCPBothFail_NotBlocking(t *testing.T) {
	// If both probe and control fail TCP, the origin is down — not GFW blocking.
	probe := probeM("origin-down.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: false, Error: "connection_refused"}},
		nil, nil)
	ctrl := controlM("origin-down.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: false, Error: "connection_refused"}},
		nil, nil)

	v := newAnalyzer().Classify(probe, ctrl, 0.3)

	// Neither TCP blocking verdict should fire because control also fails.
	assert.NotEqual(t, BlockingTCPIP, v.Blocking)
}

// ── TLS/SNI layer tests ───────────────────────────────────────────────────────

func TestClassify_TLSSNIBlocking(t *testing.T) {
	probe := probeM("tls-blocked.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "tls-blocked.com", Success: false, Error: "connection_reset"}},
		nil)
	ctrl := controlM("tls-blocked.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "tls-blocked.com", Success: true}},
		nil)

	v := newAnalyzer().Classify(probe, ctrl, 0.7)

	assert.Equal(t, BlockingTLSSNI, v.Blocking)
	assert.False(t, v.Detail.ProbeTLSSuccess)
	assert.True(t, v.Detail.ControlTLSSuccess)
}

func TestClassify_TLSCertError_NotSNIBlocking(t *testing.T) {
	// cert_error is a misconfiguration, NOT a GFW SNI signal.
	probe := probeM("self-signed.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "self-signed.com", Success: false, Error: "cert_error"}},
		nil)
	ctrl := controlM("self-signed.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "self-signed.com", Success: true}},
		nil)

	v := newAnalyzer().Classify(probe, ctrl, 0.3)

	// cert_error is not an SNI blocking signal — should NOT produce tls_sni verdict.
	assert.NotEqual(t, BlockingTLSSNI, v.Blocking)
}

// ── HTTP layer tests ──────────────────────────────────────────────────────────

func TestClassify_HTTPFailure(t *testing.T) {
	probe := probeM("http-fail.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "http-fail.com", Success: true}},
		&probeprotocol.HTTPResult{Error: "connection_reset"},
	)
	ctrl := controlM("http-fail.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "http-fail.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 3000},
	)

	v := newAnalyzer().Classify(probe, ctrl, 0.7)

	assert.Equal(t, BlockingHTTPFailure, v.Blocking)
}

func TestClassify_HTTPBlockPage(t *testing.T) {
	// GFW injects a block page — short body, different title.
	probe := probeM("blocked-http.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "blocked-http.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 150, Title: "访问受限"},
	)
	ctrl := controlM("blocked-http.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "blocked-http.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 50000, Title: "Real Site Title"},
	)

	v := newAnalyzer().Classify(probe, ctrl, 0.9)

	assert.Equal(t, BlockingHTTPDiff, v.Blocking)
	assert.Equal(t, "访问受限", v.Detail.ProbeHTTPTitle)
}

func TestClassify_HTTP451Blocked(t *testing.T) {
	// HTTP 451 Unavailable for Legal Reasons — explicit block signal.
	probe := probeM("legal-block.com",
		&probeprotocol.DNSResult{Answers: []string{"1.2.3.10"}, DurationMS: 40},
		[]probeprotocol.TCPResult{{IP: "1.2.3.10", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "1.2.3.10", SNI: "legal-block.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 451},
	)

	v := newAnalyzer().Classify(probe, nil, 0.3)

	assert.Equal(t, BlockingHTTPFailure, v.Blocking)
}

// ── No blocking (happy path) ──────────────────────────────────────────────────

func TestClassify_FullyAccessible(t *testing.T) {
	probe := probeM("accessible.com",
		&probeprotocol.DNSResult{Answers: []string{"93.184.216.34"}, DurationMS: 45},
		[]probeprotocol.TCPResult{{IP: "93.184.216.34", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "93.184.216.34", SNI: "accessible.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 2000, Title: "My Site"},
	)
	ctrl := controlM("accessible.com",
		&probeprotocol.DNSResult{Answers: []string{"93.184.216.34"}, DurationMS: 30},
		[]probeprotocol.TCPResult{{IP: "93.184.216.34", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "93.184.216.34", SNI: "accessible.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 2000, Title: "My Site"},
	)

	v := newAnalyzer().Classify(probe, ctrl, 0.9)

	assert.Equal(t, BlockingNone, v.Blocking)
	assert.True(t, v.Accessible)
	assert.Equal(t, DNSConsistent, v.DNSConsistency)
}

func TestClassify_NilControl_HeuristicOnly(t *testing.T) {
	// Without a control measurement, only heuristic flags (bogon/injected) apply.
	// A clean probe with no bogon flags and no errors → accessible.
	probe := probeM("clean.com",
		&probeprotocol.DNSResult{Answers: []string{"93.184.216.34"}, DurationMS: 45},
		[]probeprotocol.TCPResult{{IP: "93.184.216.34", Port: 443, Success: true}},
		[]probeprotocol.TLSResult{{IP: "93.184.216.34", SNI: "clean.com", Success: true}},
		&probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 2000, Title: "Clean Site"},
	)

	v := newAnalyzer().Classify(probe, nil, 0.3)

	assert.Equal(t, BlockingNone, v.Blocking)
	assert.True(t, v.Accessible)
}

// ── Utility function tests ────────────────────────────────────────────────────

func TestIPSetOverlap(t *testing.T) {
	assert.True(t, ipSetOverlap([]string{"1.2.3.4", "5.6.7.8"}, []string{"5.6.7.8"}))
	assert.False(t, ipSetOverlap([]string{"1.2.3.4"}, []string{"5.6.7.8"}))
	assert.False(t, ipSetOverlap(nil, []string{"5.6.7.8"}))
	assert.False(t, ipSetOverlap([]string{"1.2.3.4"}, nil))
}

func TestIsHTTPDiff(t *testing.T) {
	// Body ratio < 0.3 signals diff
	probe := &probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 100, Title: "Block Page"}
	ctrl := &probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 50000, Title: "Real Site"}
	assert.True(t, isHTTPDiff(probe, ctrl))

	// Same content — no diff
	same := &probeprotocol.HTTPResult{StatusCode: 200, BodyLength: 5000, Title: "My Site"}
	assert.False(t, isHTTPDiff(same, same))
}
