// Package gfw provides the GFW (Great Firewall) blocking detection engine.
// The Analyzer implements an OONI-inspired 4-layer decision tree that compares
// a probe-node measurement (inside mainland China) against a control-node
// measurement (outside GFW) to produce a structured Verdict.
package gfw

import (
	"strings"
	"time"

	"domain-platform/pkg/probeprotocol"
)

// ── Blocking classification constants ────────────────────────────────────────

const (
	// BlockingNone means the domain is reachable from the probe vantage point.
	BlockingNone = ""

	// BlockingDNS means DNS answers from the probe differ from the control and
	// the probe received bogon IPs or a suspiciously fast (injected) response.
	BlockingDNS = "dns"

	// BlockingTCPIP means TCP connections fail at the probe but succeed at the
	// control — IP-level blocking / null-routing.
	BlockingTCPIP = "tcp_ip"

	// BlockingTLSSNI means TCP connects but the TLS handshake is reset, likely
	// because the GFW detected the SNI field.
	BlockingTLSSNI = "tls_sni"

	// BlockingHTTPFailure means the HTTP layer fails at the probe but succeeds
	// at the control.
	BlockingHTTPFailure = "http-failure"

	// BlockingHTTPDiff means HTTP is reachable but the response body / status
	// differs (block page injection or content filtering).
	BlockingHTTPDiff = "http-diff"

	// BlockingIndeterminate means not enough data to determine the cause.
	BlockingIndeterminate = "indeterminate"
)

const (
	// DNSConsistent — probe and control DNS answers overlap (or no DNS data).
	DNSConsistent = "consistent"

	// DNSInconsistent — probe answers don't match control (possible injection).
	DNSInconsistent = "inconsistent"
)

// ── Verdict types ─────────────────────────────────────────────────────────────

// VerdictDetail contains per-layer evidence that supports the Verdict.  It is
// stored as JSONB in gfw_verdicts.detail for debugging / audit.
type VerdictDetail struct {
	// DNS layer
	ProbeAnswers    []string `json:"probe_answers,omitempty"`
	ControlAnswers  []string `json:"control_answers,omitempty"`
	ProbeIsBogon    bool     `json:"probe_is_bogon,omitempty"`
	ProbeIsInjected bool     `json:"probe_is_injected,omitempty"`
	CDNOwner        string   `json:"cdn_owner,omitempty"` // non-empty when CDN suppressed DNS mismatch

	// TCP layer
	ProbeTCPSuccess   bool   `json:"probe_tcp_success,omitempty"`
	ControlTCPSuccess bool   `json:"control_tcp_success,omitempty"`
	TCPError          string `json:"tcp_error,omitempty"`

	// TLS layer
	ProbeTLSSuccess   bool   `json:"probe_tls_success,omitempty"`
	ControlTLSSuccess bool   `json:"control_tls_success,omitempty"`
	TLSError          string `json:"tls_error,omitempty"`

	// HTTP layer
	ProbeHTTPStatus   int    `json:"probe_http_status,omitempty"`
	ControlHTTPStatus int    `json:"control_http_status,omitempty"`
	ProbeHTTPTitle    string `json:"probe_http_title,omitempty"`
	ControlHTTPTitle  string `json:"control_http_title,omitempty"`
	HTTPError         string `json:"http_error,omitempty"`
}

// Verdict is the output of one Analyzer.Classify call.  It describes whether
// the domain is blocked and, if so, at which layer.
type Verdict struct {
	DomainID       int64         `json:"domain_id"`
	FQDN           string        `json:"fqdn"`
	Blocking       string        `json:"blocking"`        // BlockingXxx constant
	Accessible     bool          `json:"accessible"`
	DNSConsistency string        `json:"dns_consistency"` // DNSConsistent | DNSInconsistent | ""
	Confidence     float64       `json:"confidence"`
	ProbeNodeID    string        `json:"probe_node_id"`
	ControlNodeID  string        `json:"control_node_id"`
	Detail         VerdictDetail `json:"detail"`
	MeasuredAt     time.Time     `json:"measured_at"`
}

// ── Analyzer ──────────────────────────────────────────────────────────────────

// Analyzer classifies GFW blocking by comparing probe vs control measurements.
// It implements the OONI-inspired 4-layer decision tree:
//
//  1. DNS consistency check
//  2. TCP/IP reachability check
//  3. TLS/SNI handshake check
//  4. HTTP response check
type Analyzer struct {
	asn ASNDatabase // CDN IP lookup — suppresses false-positive DNS verdicts
}

// NewAnalyzer creates an Analyzer using the provided CDN ASN database.
// Pass DefaultASNDatabase() for the built-in CDN CIDR list.
func NewAnalyzer(asn ASNDatabase) *Analyzer {
	return &Analyzer{asn: asn}
}

// Classify produces a Verdict from a pair of measurements.
//
// probe must be a measurement from a node with role "probe" (inside GFW).
// control may be nil; if so, the Analyzer falls back to heuristic-only
// analysis (bogon/injection flags) and returns at most Confidence=0.30.
func (a *Analyzer) Classify(probe, control *probeprotocol.Measurement, confidence float64) Verdict {
	v := Verdict{
		DomainID:    probe.DomainID,
		FQDN:        probe.FQDN,
		ProbeNodeID: probe.NodeID,
		MeasuredAt:  probe.MeasuredAt,
		Confidence:  confidence,
	}
	if control != nil {
		v.ControlNodeID = control.NodeID
	}

	// ── Layer 1: DNS ──────────────────────────────────────────────────────────
	dnsConsistency, dnsDetail := a.checkDNS(probe, control)
	v.DNSConsistency = dnsConsistency
	v.Detail.ProbeAnswers = dnsDetail.ProbeAnswers
	v.Detail.ControlAnswers = dnsDetail.ControlAnswers
	v.Detail.ProbeIsBogon = dnsDetail.ProbeIsBogon
	v.Detail.ProbeIsInjected = dnsDetail.ProbeIsInjected
	v.Detail.CDNOwner = dnsDetail.CDNOwner

	if dnsConsistency == DNSInconsistent {
		v.Blocking = BlockingDNS
		v.Accessible = false
		return v
	}

	// ── Layer 2: TCP ──────────────────────────────────────────────────────────
	probeAllTCPFail, controlAnyTCPOK, tcpErr := checkTCP(probe, control)
	v.Detail.ProbeTCPSuccess = !probeAllTCPFail
	v.Detail.ControlTCPSuccess = controlAnyTCPOK
	v.Detail.TCPError = tcpErr

	if probeAllTCPFail && controlAnyTCPOK {
		v.Blocking = BlockingTCPIP
		v.Accessible = false
		return v
	}

	// ── Layer 3: TLS ──────────────────────────────────────────────────────────
	probeTLSFail, controlTLSOK, tlsErr := checkTLS(probe, control)
	v.Detail.ProbeTLSSuccess = !probeTLSFail
	v.Detail.ControlTLSSuccess = controlTLSOK
	v.Detail.TLSError = tlsErr

	if probeTLSFail && controlTLSOK {
		// TLS reset/handshake failure while control handshake succeeds →
		// SNI-based blocking.
		v.Blocking = BlockingTLSSNI
		v.Accessible = false
		return v
	}

	// ── Layer 4: HTTP ─────────────────────────────────────────────────────────
	probeHTTPOK, httpBlocking, httpDetail := checkHTTP(probe, control)
	v.Detail.ProbeHTTPStatus = httpDetail.ProbeHTTPStatus
	v.Detail.ControlHTTPStatus = httpDetail.ControlHTTPStatus
	v.Detail.ProbeHTTPTitle = httpDetail.ProbeHTTPTitle
	v.Detail.ControlHTTPTitle = httpDetail.ControlHTTPTitle
	v.Detail.HTTPError = httpDetail.HTTPError

	if !probeHTTPOK {
		v.Blocking = httpBlocking
		v.Accessible = false
		return v
	}

	// ── No blocking detected ─────────────────────────────────────────────────
	v.Blocking = BlockingNone
	v.Accessible = true
	v.DNSConsistency = coalesce(v.DNSConsistency, DNSConsistent)
	return v
}

// ── Layer helpers ─────────────────────────────────────────────────────────────

// dnsCheckDetail carries intermediate DNS comparison results.
type dnsCheckDetail struct {
	ProbeAnswers    []string
	ControlAnswers  []string
	ProbeIsBogon    bool
	ProbeIsInjected bool
	CDNOwner        string
}

// checkDNS returns the DNS consistency verdict and intermediate detail.
func (a *Analyzer) checkDNS(probe, control *probeprotocol.Measurement) (string, dnsCheckDetail) {
	detail := dnsCheckDetail{}
	if probe.DNS == nil {
		// No DNS data — can't assess at this layer.
		return "", detail
	}

	detail.ProbeAnswers = probe.DNS.Answers
	detail.ProbeIsBogon = probe.DNS.IsBogon
	detail.ProbeIsInjected = probe.DNS.IsInjected

	if control != nil && control.DNS != nil {
		detail.ControlAnswers = control.DNS.Answers
	}

	// Heuristic 1: bogon IP or injection flag → immediately inconsistent.
	if probe.DNS.IsBogon || probe.DNS.IsInjected {
		return DNSInconsistent, detail
	}

	// Heuristic 2: NXDOMAIN / empty answers with error → DNS blocking.
	if probe.DNS.Error != "" && (control == nil || control.DNS == nil || control.DNS.Error == "") {
		// Probe got an error; control did not — likely DNS blocking.
		return DNSInconsistent, detail
	}

	// Heuristic 3: IP set comparison against control (when available).
	if control != nil && control.DNS != nil && len(control.DNS.Answers) > 0 {
		overlap := ipSetOverlap(probe.DNS.Answers, control.DNS.Answers)
		if !overlap {
			// Check whether all probe IPs belong to the same CDN as the control
			// IPs — CDNs use geo-distributed IPs that legitimately differ by region.
			cdnOwner := a.cdnMatchAll(probe.DNS.Answers, control.DNS.Answers)
			if cdnOwner != "" {
				// CDN routing — not a blocking signal.
				detail.CDNOwner = cdnOwner
				return DNSConsistent, detail
			}
			// True mismatch — probe got different IPs with no CDN explanation.
			return DNSInconsistent, detail
		}
	}

	return DNSConsistent, detail
}

// cdnMatchAll returns the CDN name if ALL probe answers AND all control answers
// map to the same CDN (geo-distributed CDN routing is expected).
// Returns "" if any IP doesn't belong to a known CDN.
func (a *Analyzer) cdnMatchAll(probeIPs, controlIPs []string) string {
	if a.asn == nil || len(probeIPs) == 0 || len(controlIPs) == 0 {
		return ""
	}
	// Determine CDN of the first control IP.
	cdn := a.asn.Lookup(controlIPs[0])
	if cdn == "" {
		return ""
	}
	for _, ip := range append(probeIPs, controlIPs...) {
		if a.asn.Lookup(ip) != cdn {
			return ""
		}
	}
	return cdn
}

// checkTCP returns:
//   - probeAllFail: true when every probe TCP attempt failed
//   - controlAnyOK: true when at least one control TCP attempt succeeded
//   - errStr: error string from the first failed probe attempt
func checkTCP(probe, control *probeprotocol.Measurement) (probeAllFail bool, controlAnyOK bool, errStr string) {
	if len(probe.TCP) == 0 {
		// No TCP data — cannot assess this layer.
		return false, false, ""
	}

	probeAllFail = true
	for _, r := range probe.TCP {
		if r.Success {
			probeAllFail = false
		} else if errStr == "" {
			errStr = r.Error
		}
	}

	if control != nil {
		for _, r := range control.TCP {
			if r.Success {
				controlAnyOK = true
				break
			}
		}
	}

	return probeAllFail, controlAnyOK, errStr
}

// checkTLS returns:
//   - probeFail: true when all probe TLS handshakes failed
//   - controlOK: true when at least one control TLS handshake succeeded
//   - errStr: first probe TLS error
func checkTLS(probe, control *probeprotocol.Measurement) (probeFail bool, controlOK bool, errStr string) {
	if len(probe.TLS) == 0 {
		return false, false, ""
	}

	probeFail = true
	for _, r := range probe.TLS {
		if r.Success {
			probeFail = false
		} else if errStr == "" {
			errStr = r.Error
		}
	}

	// Only flag as TLS blocking if the error indicates SNI interference
	// (connection_reset or handshake failure) rather than a cert error, which
	// could be a legitimate misconfiguration.
	if probeFail && !isTLSSNIError(errStr) {
		probeFail = false // not an SNI-blocking signal
	}

	if control != nil {
		for _, r := range control.TLS {
			if r.Success {
				controlOK = true
				break
			}
		}
	}

	return probeFail, controlOK, errStr
}

// isTLSSNIError returns true for TLS errors that indicate SNI-based censorship.
func isTLSSNIError(errStr string) bool {
	lower := strings.ToLower(errStr)
	return containsAnyStr(lower, "connection_reset", "tls_handshake_failure", "alert", "handshake")
}

// httpCheckDetail carries intermediate HTTP comparison results.
type httpCheckDetail struct {
	ProbeHTTPStatus   int
	ControlHTTPStatus int
	ProbeHTTPTitle    string
	ControlHTTPTitle  string
	HTTPError         string
}

// checkHTTP returns:
//   - probeOK: true when HTTP is reachable and response looks genuine
//   - blocking: BlockingHTTPFailure | BlockingHTTPDiff | BlockingNone
//   - detail: per-layer detail for VerdictDetail
func checkHTTP(probe, control *probeprotocol.Measurement) (probeOK bool, blocking string, detail httpCheckDetail) {
	if probe.HTTP == nil {
		// No HTTP data — cannot assess.
		return true, BlockingNone, detail
	}

	detail.ProbeHTTPStatus = probe.HTTP.StatusCode
	detail.ProbeHTTPTitle = probe.HTTP.Title
	detail.HTTPError = probe.HTTP.Error

	if control != nil && control.HTTP != nil {
		detail.ControlHTTPStatus = control.HTTP.StatusCode
		detail.ControlHTTPTitle = control.HTTP.Title
	}

	// HTTP failure at probe level.
	if probe.HTTP.Error != "" {
		return false, BlockingHTTPFailure, detail
	}

	// Unexpected status codes (block pages often use 200 but sometimes 403/503).
	if isHTTPBlockStatus(probe.HTTP.StatusCode) {
		return false, BlockingHTTPFailure, detail
	}

	// Content difference: title changed significantly compared to control.
	if control != nil && control.HTTP != nil && control.HTTP.Error == "" {
		if isHTTPDiff(probe.HTTP, control.HTTP) {
			return false, BlockingHTTPDiff, detail
		}
	}

	return true, BlockingNone, detail
}

// isHTTPBlockStatus returns true for HTTP status codes that indicate blocking.
func isHTTPBlockStatus(code int) bool {
	// 0 = no response at all; treat 451 (legally blocked) as a block signal.
	return code == 0 || code == 451
}

// isHTTPDiff returns true when the probe and control HTTP responses differ in
// a way that suggests a block page was injected.
func isHTTPDiff(probe, control *probeprotocol.HTTPResult) bool {
	// Status code mismatch (e.g. probe gets 200 block page, control gets 200 real).
	// Only flag when statuses are meaningfully different.
	if probe.StatusCode != control.StatusCode && control.StatusCode >= 200 && probe.StatusCode >= 200 {
		return true
	}

	// Title mismatch — block pages usually have different (or empty) titles.
	if probe.Title != control.Title && control.Title != "" && probe.Title != "" {
		return true
	}

	// Body length ratio: block pages are typically much shorter.
	if control.BodyLength > 0 && probe.BodyLength > 0 {
		ratio := float64(probe.BodyLength) / float64(control.BodyLength)
		if ratio < 0.3 || ratio > 3.0 {
			return true
		}
	}

	return false
}

// ── Utility helpers ───────────────────────────────────────────────────────────

// ipSetOverlap returns true if any IP in a appears in b.
func ipSetOverlap(a, b []string) bool {
	set := make(map[string]struct{}, len(b))
	for _, ip := range b {
		set[ip] = struct{}{}
	}
	for _, ip := range a {
		if _, ok := set[ip]; ok {
			return true
		}
	}
	return false
}

// containsAnyStr returns true if s contains any of the substrings (already
// lowercased).  The input s must already be lowercase.
func containsAnyStr(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// coalesce returns the first non-empty string.
func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
