# OONI Probe Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/ooni/probe-cli (master branch, Go)
> **Files analyzed**: `internal/model/measurement.go`, `internal/model/th.go`,
> `internal/experiment/webconnectivitylte/testkeys.go`,
> `internal/experiment/webconnectivitylte/measurer.go`,
> `internal/experiment/webconnectivity/summary.go`
> **Purpose**: Extract GFW censorship detection methodology for future
> monitoring vertical (Phase D, currently parked per ADR-0003).

---

## 1. Architecture: Probe vs Control Comparison

```
┌─────────────────┐          ┌──────────────────────┐
│  Probe (censored │          │  Test Helper (TH)    │
│  vantage point)  │          │  (uncensored)        │
│                  │          │                      │
│  DNS lookup      │◄────────►│  DNS lookup          │
│  TCP connect     │  compare │  TCP connect         │
│  TLS handshake   │  results │  TLS handshake       │
│  HTTP GET        │          │  HTTP GET            │
└─────────────────┘          └──────────────────────┘
         │                              │
         └──────────┬───────────────────┘
                    ▼
           Blocking Analysis
           (dns | tcp_ip | http-failure | http-diff | accessible)
```

The fundamental principle: perform the same operations from two vantage
points. If they diverge, something is blocking.

---

## 2. Measurement Data Model (Exact from Source)

```go
type Measurement struct {
    // Identity
    ID                    string          `json:"id,omitempty"`
    ReportID              string          `json:"report_id"`
    Input                 MeasurementInput `json:"input"`  // target URL

    // Probe metadata
    ProbeASN              string          `json:"probe_asn"`
    ProbeCC               string          `json:"probe_cc"`
    ProbeIP               string          `json:"probe_ip,omitempty"`
    ProbeNetworkName      string          `json:"probe_network_name"`
    ResolverASN           string          `json:"resolver_asn"`
    ResolverIP            string          `json:"resolver_ip"`

    // Test metadata
    TestName              string          `json:"test_name"`
    TestVersion           string          `json:"test_version"`
    TestStartTime         string          `json:"test_start_time"`
    MeasurementStartTime  string          `json:"measurement_start_time"`
    MeasurementRuntime    float64         `json:"test_runtime"`
    SoftwareName          string          `json:"software_name"`
    SoftwareVersion       string          `json:"software_version"`
    DataFormatVersion     string          `json:"data_format_version"`

    // Results
    TestKeys              interface{}     `json:"test_keys"`  // experiment-specific
    TestHelpers           map[string]interface{} `json:"test_helpers,omitempty"`
    Annotations           map[string]string `json:"annotations,omitempty"`
}
```

---

## 3. Web Connectivity TestKeys (Exact from Source)

```go
type TestKeys struct {
    // Raw observations
    Queries       []*ArchivalDNSLookupResult          `json:"queries"`
    TCPConnect    []*ArchivalTCPConnectResult          `json:"tcp_connect"`
    TLSHandshakes []*ArchivalTLSOrQUICHandshakeResult `json:"tls_handshakes"`
    Requests      []*ArchivalHTTPRequestResult        `json:"requests"`
    NetworkEvents []*ArchivalNetworkEvent             `json:"network_events"`

    // Control (test helper) comparison
    Control               *ControlResponse `json:"control"`
    ControlRequest        *ControlRequest  `json:"x_control_request"`
    ControlFailure        *string          `json:"control_failure"`

    // Analysis results
    DNSConsistency        string           `json:"dns_consistency"`  // "consistent" | "inconsistent"
    DNSExperimentFailure  *string          `json:"dns_experiment_failure"`
    HTTPExperimentFailure string           `json:"http_experiment_failure"`
    BodyProportion        float64          `json:"body_proportion"`
    BodyLengthMatch       bool             `json:"body_length_match"`
    HeadersMatch          bool             `json:"headers_match"`
    StatusCodeMatch       bool             `json:"status_code_match"`
    TitleMatch            bool             `json:"title_match"`

    // Final verdict
    Blocking              any              `json:"blocking"`    // "dns"|"tcp_ip"|"http-diff"|"http-failure"|false|nil
    Accessible            bool             `json:"accessible"`
    BlockingFlags         int64            `json:"x_blocking_flags"`
}
```

---

## 4. Control (Test Helper) Protocol

### Request (Probe → TH)

```go
type THRequest struct {
    HTTPRequest        string              `json:"http_request"`       // target URL
    HTTPRequestHeaders map[string][]string `json:"http_request_headers"`
    TCPConnect         []string            `json:"tcp_connect"`        // endpoints to test
}
```

### Response (TH → Probe)

```go
type THResponse struct {
    DNS           THDNSResult                       `json:"dns"`
    TCPConnect    map[string]THTCPConnectResult     `json:"tcp_connect"`
    TLSHandshake  map[string]THTLSHandshakeResult  `json:"tls_handshake"`
    HTTPRequest   THHTTPRequestResult               `json:"http_request"`
    IPInfo        map[string]*THIPInfo              `json:"ip_info"`
}

type THDNSResult struct {
    Failure *string  `json:"failure"`
    Addrs   []string `json:"addrs"`
    ASNs    []int64  `json:"-"`
}

type THHTTPRequestResult struct {
    BodyLength  int64             `json:"body_length"`
    Failure     *string           `json:"failure"`
    Title       string            `json:"title"`
    Headers     map[string]string `json:"headers"`
    StatusCode  int64             `json:"status_code"`
}
```

---

## 5. Blocking Detection Logic (Exact Decision Tree)

### Layer 1: DNS Analysis

```
DNS Consistency = "consistent" if ANY of:
  - Input is an IP address (not hostname)
  - Both probe and control fail with same error
  - Resolved IPs share at least one ASN
  - Resolved IPs share at least one address

DNS Consistency = "inconsistent" otherwise
  (different ASNs, probe got bogons, etc.)
```

### Layer 2: Final Verdict (Priority Order)

```
1. HTTPS success (valid TLS over port 443)
   → accessible = true (no MITM possible without compromised CA)

2. Control unreachable
   → blocking = nil (can't determine)

3. Both probe AND control got NXDOMAIN
   → accessible = true (site is genuinely down)

4. DNS inconsistent + probe got NXDOMAIN
   → blocking = "dns"

5. All TCP connects failed:
   - DNS consistent → blocking = "tcp_ip"
   - DNS inconsistent → blocking = "dns"

6. Control HTTP failed
   → blocking = nil (can't compare)

7. Probe HTTP failed:
   - connection_refused/reset/EOF/timeout → blocking = "http-failure"
   - NXDOMAIN during redirect → blocking = "dns"
   - TLS error → blocking = "http-failure"
   - If only 1 request + DNS inconsistent → blocking = "dns"

8. Both HTTP succeeded — compare responses:
   - Status code matches + (body OR headers OR title match)
     → accessible = true
   - Otherwise → blocking = "http-diff"
   - (If DNS inconsistent, override to blocking = "dns")
```

### Blocking Flags (v0.5 Bit Flags)

```go
const (
    AnalysisBlockingFlagDNSBlocking   = 1 << 0  // DNS-level
    AnalysisBlockingFlagTCPIPBlocking  = 1 << 1  // TCP/IP-level
    AnalysisBlockingFlagTLSBlocking    = 1 << 2  // TLS-level
    AnalysisBlockingFlagHTTPBlocking   = 1 << 3  // HTTP failure
    AnalysisBlockingFlagHTTPDiff       = 1 << 4  // HTTP content differs
    AnalysisBlockingFlagSuccess        = 1 << 5  // No blocking
)
```

---

## 6. GFW-Specific Detection Patterns

| GFW Technique | OONI Detection Method |
|---|---|
| **DNS poisoning** | Compare probe DNS answers with TH answers; check for known bogon ranges; injected responses arrive faster |
| **TCP RST injection** | TCP connect fails with RST after SYN; TH succeeds |
| **SNI-based TLS blocking** | TLS handshake reset during ClientHello; same IP with different SNI succeeds |
| **HTTP keyword blocking** | Connection reset after Host header sent; compare with TH success |
| **IP blackholing** | TCP timeout; TH connects to same IP successfully |

---

## 7. Applicability to Our Platform (Phase D — GFW Vertical)

### Proposed Measurement Model for Our Platform

```go
// internal/probe/measurement.go (future Phase D)
type GFWMeasurement struct {
    ID              string    `json:"id" db:"id"`
    DomainID        int64     `json:"domain_id" db:"domain_id"`
    FQDN            string    `json:"fqdn" db:"fqdn"`
    ProbeNodeID     string    `json:"probe_node_id" db:"probe_node_id"`
    ProbeRegion     string    `json:"probe_region" db:"probe_region"`  // "cn-beijing", "cn-shanghai"
    ProbeASN        string    `json:"probe_asn" db:"probe_asn"`

    // Per-layer results
    DNSResult       *DNSCheckResult  `json:"dns_result" db:"dns_result"`       // JSONB
    TCPResult       *TCPCheckResult  `json:"tcp_result" db:"tcp_result"`       // JSONB
    TLSResult       *TLSCheckResult  `json:"tls_result" db:"tls_result"`       // JSONB
    HTTPResult      *HTTPCheckResult `json:"http_result" db:"http_result"`     // JSONB

    // Control comparison
    ControlNodeID   string    `json:"control_node_id" db:"control_node_id"`
    ControlRegion   string    `json:"control_region" db:"control_region"`  // "hk", "jp", "us"
    ControlResult   *ControlCheckResult `json:"control_result" db:"control_result"` // JSONB

    // Verdict
    Blocking        string    `json:"blocking" db:"blocking"`       // "", "dns", "tcp_ip", "tls_sni", "http-failure", "http-diff"
    Accessible      bool      `json:"accessible" db:"accessible"`
    Confidence      float64   `json:"confidence" db:"confidence"`   // 0.0 - 1.0

    // Timing
    MeasuredAt      time.Time `json:"measured_at" db:"measured_at"`
    DurationMS      int64     `json:"duration_ms" db:"duration_ms"`
}

type DNSCheckResult struct {
    ResolverIP   string   `json:"resolver_ip"`
    Answers      []string `json:"answers"`        // resolved IPs
    AnswerASNs   []int64  `json:"answer_asns"`
    Failure      string   `json:"failure,omitempty"`
    Consistency  string   `json:"consistency"`    // "consistent", "inconsistent"
    DurationMS   int64    `json:"duration_ms"`
}
```

### Proposed Architecture (Phase D)

```
┌─────────────────────────────────────────────────┐
│  Control Plane (existing)                        │
│  + internal/gfw/ (new)                          │
│    ├── scheduler.go   (which domains to check)  │
│    ├── analyzer.go    (compare results)         │
│    └── alerter.go     (blocking detected → alert)│
└──────────────────────┬──────────────────────────┘
                       │ dispatch via asynq
         ┌─────────────┼─────────────────┐
         ▼             ▼                 ▼
  ┌────────────┐ ┌────────────┐  ┌────────────┐
  │ CN Probe   │ │ CN Probe   │  │ Control    │
  │ (Beijing)  │ │ (Shanghai) │  │ (Hong Kong)│
  │ cmd/probe  │ │ cmd/probe  │  │ cmd/probe  │
  └────────────┘ └────────────┘  └────────────┘
```

Probe nodes are separate lightweight binaries (similar to our existing
`cmd/agent` but for monitoring, not deployment). They run the 4-layer
check and report results back.

---

## 8. Summary

OONI Probe provides the gold standard for censorship detection methodology.
Key principles for our future GFW vertical:

1. **Always compare probe vs control** — never conclude "blocked" from a
   single vantage point alone
2. **Layer-by-layer diagnosis** — identify WHERE blocking occurs (DNS vs
   TCP vs TLS vs HTTP)
3. **ASN-based DNS validation** — different IPs are OK if same ASN (CDN)
4. **HTTPS as ground truth** — successful TLS = accessible (unless CA
   compromise)
5. **Confidence scoring** — transient failures need repeated observation
6. **Structured measurement data** — store raw per-layer results for
   retrospective analysis

This is Phase D work. The foundation we build now (domain asset layer,
probe infrastructure in Phase 3) will support this future vertical.
