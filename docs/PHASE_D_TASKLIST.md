# PHASE_D_TASKLIST.md — GFW Detection & Auto-Failover Work Order (PD.1 ✅ PD.2 ✅ PD.3 ✅ PD.4 ✅)

> **Created 2026-04-21.** This document is the authoritative work order for
> Phase D (GFW Detection) of the platform restructuring.
>
> **Pre-requisite**: Phase B complete (DNS plan/apply for failover switching),
> Phase C complete (probe infrastructure + alert engine). Both must be stable
> and battle-tested before Phase D begins.
>
> **Status**: Architecture designed. Implementation deferred per ADR-0003 D11.
> Activate when Phase B + C are proven in production.
>
> **Audience**: Claude Code sessions (Opus for all tasks — detection logic
> and auto-failover are both correctness-critical and security-sensitive).

---

## Phase D — Definition of Scope

Phase D adds **censorship detection and automated response**: distributed
probe nodes inside China detect GFW blocking at 4 network layers, compare
with uncensored control nodes, issue verdicts with confidence scoring, and
optionally trigger automatic DNS failover to backup infrastructure.

### What "Phase D done" looks like (acceptance demo)

```
1. Platform has 3 probe nodes inside CN (Beijing, Shanghai, Guangzhou)
   and 2 control nodes outside (Hong Kong, Tokyo)
2. Every 3 minutes, each CN probe checks all "cn-facing" tagged domains
   across 4 layers: DNS → TCP → TLS → HTTP
3. Beijing probe detects: DNS for example.com returns bogon IP (1.2.3.4),
   while HK control resolves to correct CDN IP (104.x.x.x)
4. System classifies: blocking="dns", confidence=0.3 (single observation)
5. 2 more consecutive measurements confirm → confidence rises to 0.9
6. System triggers P1 alert: "example.com BLOCKED in CN (DNS poisoning),
   confirmed by 3 consecutive measurements from 2 CN nodes"
7. Failover policy for example.com says: auto-switch A record to backup CDN
8. System calls Phase B DNS plan/apply → switches A record → audit logged
9. Status page shows: "example.com — CDN failover active (GFW block detected)"
10. 30 minutes later: CN probes detect example.com is accessible again via
    new CDN IP → blocking cleared → system holds for 1 hour (debounce)
11. After 1 hour stable → auto-recovery: switch DNS back to primary
12. Full timeline visible in GFW dashboard: detection → failover → recovery
```

### What is OUT of Phase D (do not implement)

| Feature | Timing | Reason |
|---|---|---|
| Standby domain pool (pre-warmed backup FQDNs) | Future vertical | Complex domain lifecycle (warmup, promotion) |
| CDN provider auto-switching (not just DNS) | Future | Need CDN provider abstraction first |
| Mainland-China reachability from user perspective | Future | Requires real-user monitoring (RUM) |
| IP rotation / prefix-based subdomain generation | Future | Evasion technique; separate strategy |
| Legal compliance reporting per jurisdiction | Future | Policy layer on top of detection data |
| VPN/tunnel-based circumvention | Out of scope | Not platform's responsibility |
| Historical GFW intelligence database | Future | Data science workload |

---

## Dependency Graph

```
    PD.1 (Probe Node Binary — the sensing infrastructure)
       │
       ├──────────────────────────────┐
       ▼                              ▼
    PD.2                           PD.3
  Multi-Layer                   Control vs
  Detection Engine              Measurement
  (4-layer checks)              Comparison
       │                              │
       └──────────────┬───────────────┘
                      ▼
                   PD.4
            Blocking Verdict +
            Confidence Scoring +
            Alert Integration
                      │
                      ▼
                   PD.5
            Auto-Failover
            (DNS Switch)
                      │
                      ▼
                   PD.6
            GFW Dashboard +
            Recovery Logic
```

### Critical path

`PD.1 → PD.2 → PD.3 → PD.4 → PD.5`

### Parallelization rules

- PD.2 and PD.3 can overlap (detection runs checks; comparison needs both
  probe + control results — but the code is separable)
- PD.5 depends on PD.4 (need confirmed blocking verdict before failover)
- PD.6 can start after PD.4 (dashboard shows verdicts) and finishes after PD.5 (shows failover)

---

## Task Cards

---

### PD.1 — Probe Node Binary **(Opus)**

**Owner**: **Opus** — security-sensitive binary deployed to untrusted networks;
must be minimal, hardened, and structurally constrained (same philosophy as
`cmd/agent`)
**Status**: ✅ COMPLETED 2026-04-23
**Depends on**: Phase C (PC.1 probe infrastructure, PC.6 notification hub)
**Reads first**: `docs/analysis/OONI_PROBE_ANALYSIS.md` §1 "Architecture",
`CLAUDE.md` Critical Rule #3 (agent whitelist philosophy applies to probe nodes),
`ARCHITECTURE.md` §3 "Pull Agent Detailed Design" (similar binary design)

**Context**: The platform needs eyes inside China. Probe nodes are lightweight
Go binaries deployed to CN VPS instances. They pull check assignments from
the control plane, execute 4-layer measurements, and report results back.
They NEVER hold credentials for DNS/CDN providers or make operational changes.

**Scope (in)**:

- New binary: `cmd/probe/main.go`:
  - Single static Go binary (cross-compiled for `linux/amd64`)
  - Configuration: `/etc/domain-platform/probe.yaml`
    ```yaml
    control_plane_url: https://platform.example.com
    node_id: cn-beijing-01
    region: cn-north
    role: probe          # "probe" (CN node) or "control" (uncensored node)
    check_interval: 180  # seconds between check cycles
    max_concurrent: 20   # concurrent measurements
    tls:
      ca_cert: /etc/domain-platform/ca.crt
      client_cert: /etc/domain-platform/probe.crt
      client_key: /etc/domain-platform/probe.key
    ```

- Probe node protocol (control plane side, extends `cmd/server`):
  ```
  POST /probe/v1/register          — first contact, register node
  POST /probe/v1/heartbeat         — alive + metrics
  GET  /probe/v1/assignments       — pull domains to check (long-poll)
  POST /probe/v1/measurements      — report measurement results (bulk)
  ```

- Probe node allowed actions (whitelist, same philosophy as agent):
  1. Register with control plane
  2. Heartbeat (report status, load, last error)
  3. Pull domain assignments (which FQDNs to check)
  4. DNS lookup (system resolver + configurable resolver)
  5. TCP connect (SYN to target IP:port)
  6. TLS handshake (with target SNI)
  7. HTTP GET (with Host header)
  8. Report measurement results back to control plane
  9. Self-upgrade (same canary pattern as agent)

- Probe node MUST NOT:
  - Hold DNS/CDN provider credentials
  - Make DNS changes
  - Execute arbitrary commands
  - Access paths outside `/etc/domain-platform/`
  - Connect to anything except control plane + measurement targets

- Control plane tables:
  ```sql
  CREATE TABLE gfw_probe_nodes (
      id              BIGSERIAL PRIMARY KEY,
      uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
      node_id         VARCHAR(64) NOT NULL UNIQUE,  -- "cn-beijing-01"
      region          VARCHAR(64) NOT NULL,          -- "cn-north", "hk", "jp"
      role            VARCHAR(16) NOT NULL,          -- "probe" (CN), "control" (uncensored)
      status          VARCHAR(32) NOT NULL DEFAULT 'registered', -- registered, online, offline, error
      last_seen_at    TIMESTAMPTZ,
      agent_version   VARCHAR(32),
      ip_address      VARCHAR(45),
      metadata        JSONB DEFAULT '{}',           -- load_avg, disk_free, etc.
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE TABLE gfw_check_assignments (
      id              BIGSERIAL PRIMARY KEY,
      domain_id       BIGINT NOT NULL REFERENCES domains(id),
      probe_node_ids  JSONB NOT NULL,               -- ["cn-beijing-01", "cn-shanghai-01"]
      control_node_ids JSONB NOT NULL,              -- ["hk-01", "jp-01"]
      check_interval  INT NOT NULL DEFAULT 180,     -- seconds
      enabled         BOOLEAN NOT NULL DEFAULT true,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```

- Assignment logic:
  - Domains tagged `cn-facing` → assigned to all CN probe nodes + at least 1 control node
  - Control plane computes assignments → probe nodes pull their slice

**Scope (out)**:

- Detection logic (PD.2 — probe just runs raw checks and reports)
- Comparison logic (PD.3 — control plane does analysis)
- Alert/failover (PD.4, PD.5)
- Probe node deployment automation (manual VPS setup)
- Probe node auto-scaling

**Deliverables**:

- `cmd/probe/` binary with register/heartbeat/pull/measure/report lifecycle
- Probe node protocol endpoints on control plane
- `gfw_probe_nodes` + `gfw_check_assignments` tables
- mTLS authentication (reuse CA from agent protocol)
- Assignment computation logic
- `make probe` build target (cross-compile linux/amd64)
- Unit tests for probe execution + protocol

**Acceptance**:

- Probe binary starts, registers with control plane → node appears in DB
- Probe heartbeats every 30s → `last_seen_at` updated
- Probe pulls assignments → receives list of FQDNs to check
- Probe executes DNS+TCP+TLS+HTTP for assigned domains → reports results
- Results received by control plane → stored in measurements table (PD.2)
- Probe binary has NO code path for os/exec (verified by grep gate)
- Probe binary is < 15 MB
- mTLS: invalid cert → rejected; valid cert → accepted
- Probe offline > 90s → status transitions to "offline"
- `go test ./cmd/probe/...` passes
- `make check-probe-safety` grep gate passes (no os/exec, no file writes outside config)

---

### PD.2 — Multi-Layer Detection Engine ✅ (完成)

**Owner**: **Opus** — detection logic determines all downstream decisions;
false negatives miss blocking, false positives trigger unnecessary failovers
**Status**: ✅ COMPLETED 2026-04-26
**Depends on**: PD.1 (probe nodes reporting raw measurement data)
**Reads first**: `docs/analysis/OONI_PROBE_ANALYSIS.md` §5 "Blocking Detection
Logic", §6 "GFW-Specific Detection Patterns"

**Context**: Each probe node runs 4 checks per domain and reports raw results.
This task implements the measurement execution on the probe side and the
result storage on the control plane side.

**Scope (in)**:

- `cmd/probe/checker/` — measurement execution (runs ON the probe node):
  ```go
  type Checker struct {
      resolver    string  // system or custom DNS resolver
      timeout     time.Duration
      logger      *zap.Logger
  }

  func (c *Checker) CheckDNS(ctx, fqdn) (*DNSResult, error)
  func (c *Checker) CheckTCP(ctx, ip string, port int) (*TCPResult, error)
  func (c *Checker) CheckTLS(ctx, ip string, port int, sni string) (*TLSResult, error)
  func (c *Checker) CheckHTTP(ctx, url string) (*HTTPResult, error)
  func (c *Checker) FullCheck(ctx, fqdn) (*Measurement, error)  // all 4 layers
  ```

- Per-layer result types:
  ```go
  type DNSResult struct {
      ResolverIP   string        `json:"resolver_ip"`
      Answers      []string      `json:"answers"`       // resolved IPs
      CNAME        []string      `json:"cname"`         // CNAME chain
      Error        string        `json:"error,omitempty"`
      DurationMS   int64         `json:"duration_ms"`
      Truncated    bool          `json:"truncated"`
  }

  type TCPResult struct {
      IP           string        `json:"ip"`
      Port         int           `json:"port"`
      Success      bool          `json:"success"`
      Error        string        `json:"error,omitempty"`
      DurationMS   int64         `json:"duration_ms"`
  }

  type TLSResult struct {
      IP           string        `json:"ip"`
      SNI          string        `json:"sni"`
      Success      bool          `json:"success"`
      Error        string        `json:"error,omitempty"`  // "connection_reset", "timeout", "cert_error"
      DurationMS   int64         `json:"duration_ms"`
      CertSubject  string        `json:"cert_subject,omitempty"`
      CertIssuer   string        `json:"cert_issuer,omitempty"`
  }

  type HTTPResult struct {
      URL          string        `json:"url"`
      StatusCode   int           `json:"status_code"`
      BodyLength   int64         `json:"body_length"`
      Title        string        `json:"title"`           // extracted <title>
      Headers      map[string]string `json:"headers"`
      Error        string        `json:"error,omitempty"` // "connection_reset", "timeout", "tls_error"
      DurationMS   int64         `json:"duration_ms"`
  }

  type Measurement struct {
      FQDN         string        `json:"fqdn"`
      NodeID       string        `json:"node_id"`
      NodeRole     string        `json:"node_role"`       // "probe" or "control"
      DNS          *DNSResult    `json:"dns"`
      TCP          []*TCPResult  `json:"tcp"`             // one per resolved IP
      TLS          []*TLSResult  `json:"tls"`             // one per resolved IP
      HTTP         *HTTPResult   `json:"http"`
      MeasuredAt   time.Time     `json:"measured_at"`
      TotalMS      int64         `json:"total_ms"`
  }
  ```

- Control plane storage:
  ```sql
  CREATE TABLE gfw_measurements (
      id              BIGSERIAL,
      domain_id       BIGINT NOT NULL,
      node_id         VARCHAR(64) NOT NULL,
      node_role       VARCHAR(16) NOT NULL,       -- "probe", "control"
      region          VARCHAR(64) NOT NULL,
      fqdn            VARCHAR(512) NOT NULL,
      dns_result      JSONB,
      tcp_result      JSONB,
      tls_result      JSONB,
      http_result     JSONB,
      total_ms        INT,
      measured_at     TIMESTAMPTZ NOT NULL,
      PRIMARY KEY (measured_at, id)
  );

  SELECT create_hypertable('gfw_measurements', 'measured_at');
  SELECT add_retention_policy('gfw_measurements', INTERVAL '180 days');
  ```

- GFW-specific detection heuristics (probe-side):
  - DNS: detect known GFW bogon IPs (maintain a list)
  - DNS: detect injection (response arrives < 5ms = likely injected)
  - TCP: detect RST after SYN (GFW sends RST before real server responds)
  - TLS: detect reset during ClientHello (SNI-based blocking)
  - HTTP: detect connection reset after Host header

- Measurement scheduling (control plane → probe node):
  - Group domains by check interval
  - Distribute evenly across CN probe nodes (load balance)
  - Control nodes check the SAME domains at the SAME time (for comparison)
  - Stagger start times to avoid burst

**Scope (out)**:

- Verdict computation (PD.3 — this task only collects raw measurements)
- Alert/failover (PD.4, PD.5)
- Historical analysis (PD.6)

**Deliverables**:

- `cmd/probe/checker/` package with 4-layer check implementation
- Per-layer result structs
- Full measurement execution (DNS → resolve IPs → TCP per IP → TLS per IP → HTTP)
- `gfw_measurements` TimescaleDB hypertable
- Control plane measurement ingestion endpoint
- GFW bogon IP list (configurable, updatable)
- Measurement scheduling logic

**Acceptance**:

- Probe checks `example.com` → produces Measurement with all 4 layers
- DNS check returns resolved IPs (or error + duration)
- TCP check to each resolved IP → success/fail per IP
- TLS check with correct SNI → reports cert info or error type
- HTTP check → reports status code, body length, title
- Results stored in `gfw_measurements` with correct TimescaleDB partitioning
- Known bogon IP detected → flagged in DNS result
- Control node runs same check → results stored with `node_role="control"`
- 180-day retention policy active
- `go test ./cmd/probe/checker/...` passes
- Throughput: 20 concurrent measurements per probe node sustained

---

### PD.3 — Control vs Measurement Comparison ✅ (完成)

**Owner**: **Opus** — the analysis logic that determines "blocked" vs
"accessible" — must handle CDN variation, transient failures, and real blocking
**Status**: ✅ COMPLETED 2026-04-26
**Depends on**: PD.2 (measurements from both probe + control nodes)
**Reads first**: `docs/analysis/OONI_PROBE_ANALYSIS.md` §5 "Blocking Detection
Logic" (exact decision tree)

**Context**: Raw measurements alone don't tell you if a site is blocked. You
need to COMPARE probe results (from CN) with control results (from HK/JP).
If they diverge in specific patterns, the site is blocked. This task
implements the OONI-style comparison engine.

**Scope (in)**:

- `internal/gfw/analyzer.go`:
  ```go
  type Analyzer struct {
      store      MeasurementStore
      asnDB      ASNDatabase       // IP → ASN lookup
      bogonList  BogonList         // known GFW injected IPs
      logger     *zap.Logger
  }

  func (a *Analyzer) Analyze(ctx, domainID, probeMeasurement, controlMeasurement) (*Verdict, error)
  func (a *Analyzer) CheckDNSConsistency(probe, control *DNSResult) string  // "consistent" | "inconsistent"
  func (a *Analyzer) ClassifyBlocking(probe *Measurement, control *Measurement) *Verdict
  ```

- DNS Consistency check (from OONI, ASN-based):
  ```go
  func CheckDNSConsistency(probe, control *DNSResult, asnDB ASNDatabase) string {
      // "consistent" if ANY of:
      //   - Both failed with same error
      //   - Resolved IPs share at least one ASN (CDN = same AS, different IPs is OK)
      //   - Resolved IPs share at least one address
      //   - Probe resolved to known CDN IP range
      // "inconsistent" otherwise:
      //   - Probe got bogon IPs while control got real IPs
      //   - Probe got IPs in completely different ASN with no CDN overlap
      //   - Probe got NXDOMAIN while control resolved successfully
  }
  ```

- Blocking classification (exact OONI decision tree, adapted):
  ```go
  func ClassifyBlocking(probe, control *Measurement) *Verdict {
      // Priority order:
      // 1. HTTPS success (probe TLS+HTTP succeeded) → accessible
      // 2. Control unreachable → indeterminate (can't compare)
      // 3. Both NXDOMAIN → site is down (not blocked)
      // 4. DNS inconsistent + probe NXDOMAIN → blocking="dns"
      // 5. All TCP failed:
      //    - DNS consistent → blocking="tcp_ip"
      //    - DNS inconsistent → blocking="dns" (root cause)
      // 6. TLS reset with correct SNI, but succeeds with random SNI to same IP
      //    → blocking="tls_sni"
      // 7. Control HTTP failed → indeterminate
      // 8. Probe HTTP failed (reset/timeout/EOF):
      //    → blocking="http-failure"
      // 9. Both HTTP succeeded, compare:
      //    - Status + (body length OR title match) → accessible
      //    - Otherwise → blocking="http-diff"
      // At every step: if DNS inconsistent, prefer blocking="dns"
  }
  ```

- Verdict struct:
  ```go
  type Verdict struct {
      DomainID       int64     `json:"domain_id"`
      FQDN           string    `json:"fqdn"`
      Blocking       string    `json:"blocking"`       // "", "dns", "tcp_ip", "tls_sni", "http-failure", "http-diff"
      Accessible     bool      `json:"accessible"`
      DNSConsistency string    `json:"dns_consistency"` // "consistent", "inconsistent"
      Confidence     float64   `json:"confidence"`      // 0.0 - 1.0
      ProbeNodeID    string    `json:"probe_node_id"`
      ControlNodeID  string    `json:"control_node_id"`
      Detail         VerdictDetail `json:"detail"`      // per-layer breakdown
      MeasuredAt     time.Time `json:"measured_at"`
  }
  ```

- Confidence scoring:
  ```go
  // Confidence increases with consecutive confirmations:
  // 1 observation: 0.3
  // 2 consecutive from same node: 0.5
  // 2+ nodes confirm: 0.7
  // 3+ consecutive from 2+ nodes: 0.9
  // Confidence decays if next check shows accessible: reset to 0
  ```
  Redis key: `gfw:confidence:{domain_id}` → `{count, nodes, last_blocking_type}`

- CDN/Geo variation allowlist:
  - Known CDN ASNs (Cloudflare, Fastly, Akamai, etc.) → IP differences within
    same CDN are NOT blocking
  - Configurable allowlist in platform settings

- `gfw_verdicts` table (one row per analysis):
  ```sql
  CREATE TABLE gfw_verdicts (
      id              BIGSERIAL PRIMARY KEY,
      domain_id       BIGINT NOT NULL REFERENCES domains(id),
      blocking        VARCHAR(32) NOT NULL,       -- "", "dns", "tcp_ip", "tls_sni", "http-failure", "http-diff"
      accessible      BOOLEAN NOT NULL,
      dns_consistency VARCHAR(16),
      confidence      DECIMAL(3,2) NOT NULL,
      probe_node_id   VARCHAR(64) NOT NULL,
      control_node_id VARCHAR(64) NOT NULL,
      detail          JSONB,
      measured_at     TIMESTAMPTZ NOT NULL,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE INDEX idx_verdicts_domain ON gfw_verdicts(domain_id, measured_at DESC);
  CREATE INDEX idx_verdicts_blocking ON gfw_verdicts(blocking) WHERE blocking != '';
  ```

**Scope (out)**:

- Alert dispatch (PD.4)
- Failover execution (PD.5)
- ASN database maintenance (use a static MaxMind / ip2asn file, update monthly)

**Deliverables**:

- Analyzer with DNS consistency check + blocking classification
- Confidence scoring (Redis-based consecutive tracking)
- CDN ASN allowlist
- `gfw_verdicts` table + store
- Unit tests covering all decision tree branches
- Integration test: simulated probe + control → correct verdict

**Acceptance**:

- Probe returns bogon IP, control returns Cloudflare IP → blocking="dns", confidence=0.3
- 3 consecutive dns-blocked measurements from 2 nodes → confidence=0.9
- Probe TCP RST, control TCP success → blocking="tcp_ip"
- Probe TLS reset on target SNI, success with random SNI → blocking="tls_sni"
- Both in same CDN ASN but different IPs → accessible (not false positive)
- Both NXDOMAIN → accessible (site down, not blocked)
- Control unreachable → verdict.Blocking="" + confidence=0 (indeterminate)
- After blocking confirmed, next check accessible → confidence resets to 0
- `go test -race ./internal/gfw/...` passes with all decision tree branches covered

---

### PD.4 — Blocking Alert + Dashboard **(Opus)**

**Owner**: **Opus** — alerting on blocking events triggers operational response;
must not false-alarm but must not miss real blocks
**Status**: ✅ COMPLETED 2026-04-26
**Depends on**: PD.3 (verdicts with confidence scoring), Phase C PC.2 (alert engine)
**Reads first**: `CLAUDE.md` Critical Rule #8

**Context**: When confidence crosses threshold (0.7 = likely, 0.9 = confirmed),
the system issues alerts. The alert integrates with Phase C's alert engine
(dedup, severity, notification channels). A dashboard shows blocking status
across all CN-facing domains.

**Scope (in)**:

- Alert integration:
  - When `confidence >= 0.7` → create alert_event:
    - `target_type = "domain"`, `alert_type = "gfw_blocked"`
    - `severity = "p1"` (blocking = operational impact)
    - `message`: "example.com BLOCKED in CN (type: dns, confidence: 0.9,
      detected by: cn-beijing-01 + cn-shanghai-01)"
    - `detail` JSONB: verdict + measurements + timeline
  - When blocking clears (confidence drops to 0 after N accessible checks):
    - Auto-resolve the alert
    - INFO notification: "example.com no longer blocked in CN"
  - Dedup: same domain + "gfw_blocked" → 1 alert per hour max (reuse PC.2 engine)

- Blocking state tracking (on domain):
  ```sql
  ALTER TABLE domains ADD COLUMN blocking_status VARCHAR(32); -- null, "blocked", "possibly_blocked"
  ALTER TABLE domains ADD COLUMN blocking_type VARCHAR(32);   -- "dns", "tcp_ip", "tls_sni", etc.
  ALTER TABLE domains ADD COLUMN blocking_since TIMESTAMPTZ;
  ALTER TABLE domains ADD COLUMN blocking_confidence DECIMAL(3,2);
  ```
  - Updated by verdict processor:
    - confidence >= 0.9 → blocking_status = "blocked"
    - 0.7 <= confidence < 0.9 → blocking_status = "possibly_blocked"
    - confidence < 0.3 → blocking_status = null (cleared)

- API:
  - `GET /api/v1/gfw/blocked-domains` — list currently blocked domains
    (filter: blocking_type, confidence, region)
  - `GET /api/v1/gfw/verdicts/:domain_id` — verdict history for domain
  - `GET /api/v1/gfw/stats` — aggregate: total monitored, blocked count, by type
  - `GET /api/v1/gfw/timeline/:domain_id` — blocking/accessible timeline

- Frontend:
  - `web/src/views/gfw/GFWDashboard.vue`:
    - Summary cards: total CN-monitored, currently blocked, possibly blocked
    - Blocked domains table: FQDN, blocking type, confidence, since, probe nodes
    - Blocking type distribution (pie: dns / tcp / tls / http)
  - `web/src/views/gfw/GFWTimeline.vue`:
    - Per-domain timeline: horizontal bar chart showing blocked/accessible periods
    - Hover: show measurement details for that time point
  - Domain detail: "GFW" tab (when domain is cn-facing tagged):
    - Current status: blocked/accessible + confidence
    - Last 24h measurement results (mini timeline)
    - Blocking history table

**Scope (out)**:

- Failover execution (PD.5)
- Recovery logic (PD.6)
- Geo-map visualization (which cities are blocked)

**Deliverables**:

- Alert integration (gfw_blocked alert type in PC.2 engine)
- Domain blocking_status fields + update logic
- Blocking state API endpoints
- GFW dashboard + timeline pages
- `go test ./internal/gfw/...` + `npm run build` passes

**Acceptance**:

- Confidence reaches 0.9 → alert_event created with severity P1
- Alert dispatched via Phase C notification channels (Telegram + Slack)
- Domain detail shows blocking_status = "blocked"
- `GET /api/v1/gfw/blocked-domains` returns currently blocked domains
- Blocking clears → alert auto-resolved + INFO notification
- Dashboard shows correct counts and distribution
- Timeline shows blocking/accessible periods for last 7 days
- Duplicate blocking detection within 1 hour → no new alert (dedup working)
- `go test ./internal/gfw/...` passes

---

### PD.5 — Auto-Failover (DNS Switch) **(Opus)**

**Owner**: **Opus** — auto-failover changes production DNS; this is the highest
risk automated action in the entire platform. Must be bulletproof.
**Status**: 🔲 NOT STARTED
**Depends on**: PD.4 (confirmed blocking verdict), Phase B PB.2+PB.3 (DNS
plan/apply engine)
**Reads first**: `docs/analysis/DNSCONTROL_ANALYSIS.md` §5 (how we apply DNS
changes), `docs/analysis/OCTODNS_ANALYSIS.md` §3 (safety thresholds)

**Context**: When a domain is confirmed blocked, the platform can automatically
switch its DNS records to a backup CDN or IP that is accessible from China.
This is the "response" half of the detect-and-respond cycle.

**Scope (in)**:

- Failover policy configuration:
  ```sql
  CREATE TABLE failover_policies (
      id              BIGSERIAL PRIMARY KEY,
      uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
      domain_id       BIGINT NOT NULL REFERENCES domains(id) UNIQUE,
      enabled         BOOLEAN NOT NULL DEFAULT false,
      auto_failover   BOOLEAN NOT NULL DEFAULT false,  -- false = manual only
      failover_type   VARCHAR(32) NOT NULL DEFAULT 'dns_switch',
      primary_config  JSONB NOT NULL,      -- {"a_records": ["104.21.x.x"], "cname": "cdn.primary.com"}
      backup_config   JSONB NOT NULL,      -- {"a_records": ["8.8.x.x"], "cname": "cdn.backup.com"}
      min_confidence  DECIMAL(3,2) NOT NULL DEFAULT 0.9,  -- trigger threshold
      cooldown_minutes INT NOT NULL DEFAULT 60,  -- min time between failovers
      auto_recovery   BOOLEAN NOT NULL DEFAULT true,
      recovery_stable_minutes INT NOT NULL DEFAULT 60,  -- accessible for this long before recovery
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE TABLE failover_history (
      id              BIGSERIAL PRIMARY KEY,
      domain_id       BIGINT NOT NULL REFERENCES domains(id),
      policy_id       BIGINT NOT NULL REFERENCES failover_policies(id),
      action          VARCHAR(32) NOT NULL,   -- "failover", "recovery", "manual_failover", "manual_recovery"
      trigger         VARCHAR(64) NOT NULL,   -- "auto_blocking_confirmed", "manual_operator", "auto_recovery"
      from_config     JSONB NOT NULL,         -- DNS state before change
      to_config       JSONB NOT NULL,         -- DNS state after change
      dns_plan_hash   VARCHAR(64),            -- Phase B plan hash used
      executed_by     BIGINT REFERENCES users(id),  -- null for auto
      executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      success         BOOLEAN NOT NULL,
      error           TEXT
  );
  ```

- `internal/gfw/failover.go`:
  ```go
  type FailoverService struct {
      policies   FailoverPolicyStore
      history    FailoverHistoryStore
      dnsSvc     *dnssync.Service      // Phase B DNS plan/apply
      alertSvc   *alert.Engine         // Phase C alerts
      redis      *redis.Client
      logger     *zap.Logger
  }

  func (f *FailoverService) EvaluateAndExecute(ctx, domainID int64, verdict *Verdict) error
  func (f *FailoverService) ManualFailover(ctx, domainID int64, userID int64) error
  func (f *FailoverService) ManualRecovery(ctx, domainID int64, userID int64) error
  func (f *FailoverService) CheckRecovery(ctx, domainID int64) error
  ```

- Auto-failover logic:
  ```
  On new verdict where blocking=true AND confidence >= policy.min_confidence:
    1. Check policy.enabled AND policy.auto_failover → must both be true
    2. Check cooldown: last failover for this domain was > cooldown_minutes ago
    3. Check not already in failover state (domain.blocking_status already = "blocked" AND already switched)
    4. Compute DNS plan: change A/CNAME records from primary_config → backup_config
    5. Safety check: this is a KNOWN safe change (only changes pre-defined records)
       → skip percentage thresholds (but still audit)
    6. Execute DNS apply via Phase B plan/apply (with force=true, audit note="auto-failover")
    7. Record in failover_history
    8. P1 alert: "Auto-failover executed for example.com: switched to backup CDN"
  ```

- Auto-recovery logic:
  ```
  Periodic check (every 5 minutes for domains in failover state):
    1. If last N verdicts (where N = recovery_stable_minutes / check_interval) all accessible:
    2. Check policy.auto_recovery = true
    3. Compute DNS plan: change from backup_config → primary_config
    4. Execute via Phase B plan/apply
    5. Record in failover_history (action="recovery")
    6. INFO alert: "Auto-recovery for example.com: switched back to primary"
    7. Clear domain.blocking_status
  ```

- Safeguards:
  - Cooldown: minimum 60 minutes between failovers (prevent flapping)
  - Max failovers per day: 3 (configurable, prevent runaway loops)
  - Failover only changes SPECIFIED records (primary→backup), never touches other records
  - Recovery debounce: must be stable for full `recovery_stable_minutes` period
  - All failover actions go through Phase B audit trail
  - Every auto action → P1 alert to operators (they should know)

- API:
  - `POST /api/v1/gfw/failover-policies` — create/update policy
  - `GET /api/v1/gfw/failover-policies/:domain_id` — get policy for domain
  - `PUT /api/v1/gfw/failover-policies/:id` — update
  - `POST /api/v1/gfw/failover/:domain_id/execute` — manual failover
  - `POST /api/v1/gfw/failover/:domain_id/recover` — manual recovery
  - `GET /api/v1/gfw/failover-history` — list failover actions (filter by domain, date)

- Frontend:
  - Domain detail "GFW" tab → "Failover Policy" section:
    - Enable/disable toggle
    - Auto-failover toggle (with warning text)
    - Primary config (current DNS records)
    - Backup config (fallback DNS records)
    - Confidence threshold slider
    - Cooldown + recovery settings
  - Manual failover/recovery buttons (with confirmation modal)
  - Failover history table in domain detail

**Scope (out)**:

- CDN provider switching (only DNS records change, not CDN config)
- Standby domain activation (swap to entirely different FQDN)
- Traffic splitting / weighted failover
- Automatic backup infrastructure provisioning
- Cost optimization (cheapest accessible CDN selection)

**Deliverables**:

- failover_policies + failover_history tables
- FailoverService with auto-failover + auto-recovery logic
- Safeguards (cooldown, max per day, debounce)
- Integration with Phase B DNS plan/apply
- Manual failover/recovery API
- Frontend: policy config + manual controls + history
- Full audit trail

**Acceptance**:

- Domain blocked (confidence=0.9) + policy enabled + auto_failover=true
  → DNS automatically switched to backup config within 1 check cycle
- failover_history row created with correct from/to config
- P1 alert fires: "auto-failover executed"
- Cooldown: second blocking within 60 min → no second failover
- Domain accessible for 60 stable minutes → auto-recovery executes
- Recovery → DNS switched back to primary + failover_history recorded
- Manual failover via API → works regardless of confidence
- Max 3 failovers per day exceeded → stops, alerts operator
- All DNS changes visible in Phase B dns_sync_history (full audit)
- `go test -race ./internal/gfw/...` passes

---

### PD.6 — GFW Dashboard + Recovery Monitoring

**Owner**: Sonnet
**Status**: 🔲 NOT STARTED
**Depends on**: PD.4 (blocking verdicts), PD.5 (failover actions)
**Reads first**: None specific (builds on PD.4 dashboard + PD.5 history)

**Context**: The final task ties everything together: a comprehensive GFW
operations dashboard showing blocking status, failover state, recovery
progress, historical trends, and probe node health.

**Scope (in)**:

- Enhanced GFW dashboard (extends PD.4's basic dashboard):
  - **Operations panel**: domains currently in failover state, time in failover,
    pending recovery (how long until auto-recovery triggers)
  - **Probe node health**: status of each CN + control node (online/offline,
    last seen, measurement count today)
  - **Blocking trends**: line chart — # blocked domains over last 30 days
  - **Blocking events**: timeline of all blocking/unblocking events (across
    all domains)
  - **Failover success rate**: % of failovers that restored accessibility
  - **Recovery time**: average time from detection to failover execution

- Probe node management UI:
  - `web/src/views/gfw/ProbeNodeList.vue`:
    - List nodes: ID, region, role, status, last seen, IP, version
    - Status badges (online=green, offline=red)
    - Assignment count per node

- Historical analysis page:
  - `web/src/views/gfw/GFWHistory.vue`:
    - Date range selector
    - Blocking events table: domain, type, duration, action taken
    - Export to CSV
    - Per-domain blocking frequency (how often is this domain blocked?)
    - Blocking type trends (is DNS poisoning increasing vs SNI blocking?)

- Recovery monitoring:
  - Periodic worker checks domains in failover state
  - Dashboard shows: "example.com in failover since 2h ago, recovery check:
    5/12 consecutive accessible (need 12 for auto-recovery at 60-min stable)"
  - Visual progress bar toward recovery threshold

- API:
  - `GET /api/v1/gfw/nodes` — probe node status list
  - `GET /api/v1/gfw/trends?days=30` — blocking count time series
  - `GET /api/v1/gfw/events?from=...&to=...` — blocking/failover events
  - `GET /api/v1/gfw/recovery-status` — domains in failover + recovery progress

**Scope (out)**:

- Geo-map visualization (requires map library integration)
- Predictive blocking (ML-based "this domain will be blocked soon")
- Comparative analysis across ISPs/regions
- Public-facing GFW status page (internal operations only)

**Deliverables**:

- Enhanced dashboard (operations panel, trends, failover metrics)
- Probe node management page
- Historical analysis page with export
- Recovery progress monitoring
- API endpoints for all dashboard data
- `npm run build` clean

**Acceptance**:

- Dashboard shows: 3 blocked, 1 in failover, 2 probe nodes online
- Probe node list shows correct status for each node
- Trends chart shows blocking count over 30 days
- Domain in failover → recovery progress bar shows N/M accessible checks
- Failover success rate computed correctly
- Historical export (CSV) includes all blocking events for date range
- Blocking type distribution matches stored verdict data
- `npm run build` succeeds with zero errors

---

## Phase D Effort Estimate

| # | Task | Owner | Lo | Hi | Risk | Notes |
|---|---|---|---|---|---|---|
| PD.1 | Probe Node Binary | **Opus** | 2.5 | 4.0 | 🔴 | New binary; mTLS; deployment to CN VPS |
| PD.2 | Multi-Layer Detection | **Opus** | 2.0 | 3.5 | 🔴 | 4-layer checks; GFW-specific heuristics |
| PD.3 | Control vs Measurement | **Opus** | 2.5 | 4.0 | 🔴 | Decision tree complexity; false positive tuning |
| PD.4 | Blocking Alert + Dashboard | **Opus** | 1.5 | 2.5 | 🟡 | Alert integration; confidence state tracking |
| PD.5 | Auto-Failover | **Opus** | 2.5 | 4.0 | 🔴 | Highest risk: auto DNS changes in production |
| PD.6 | Dashboard + Recovery | Sonnet | 1.5 | 2.5 | 🟢 | Frontend; data is ready from PD.1–PD.5 |

**Task sum**: Lo = 12.5 days / Hi = 20.5 days

**Integration friction**: +4–6 days (CN VPS deployment, real GFW behavior
testing, false positive tuning with production traffic, failover/recovery
cycle testing)

| | Work days | Calendar weeks |
|---|---|---|
| **Optimistic** | 16.5 days | ~3.5 weeks |
| **Mid-range** | 23 days | ~5 weeks |
| **Pessimistic** | 26.5 days | ~6 weeks |

### Risk hotspots

1. **PD.3 False positive handling** 🔴 — CDN geo-variation looks like DNS
   inconsistency. Must maintain CDN ASN allowlist and test against real
   Chinese network conditions. Mitigation: start with conservative thresholds
   (require 0.9 confidence = 3 consecutive from 2+ nodes).

2. **PD.5 Auto-failover safety** 🔴 — Incorrect failover = all CN traffic
   goes to wrong IP. Safeguards (cooldown, max per day, operator notification)
   are critical. Mitigation: ship with `auto_failover=false` default; operators
   opt-in per domain after manual testing.

3. **PD.1 CN VPS reliability** 🔴 — Probe nodes in China may themselves be
   unstable (ISP issues, government actions). Must distinguish "probe node
   down" from "domain blocked". Mitigation: require 2+ CN nodes to agree
   before confirming blocking.

### Recommended work order

```
Week 1:  PD.1 (probe node binary — needs early testing from CN)
Week 2:  PD.2 (detection engine) + deploy probe nodes to CN/HK/JP
Week 3:  PD.3 (comparison logic + confidence scoring)
Week 4:  PD.4 (alert integration) + PD.5 start (failover policies)
Week 5:  PD.5 finish (auto-failover + recovery) + PD.6 (dashboard)
Week 6:  Integration testing with real GFW + false positive tuning
```

---

## Scope Creep Warnings

| Temptation | Truth |
|---|---|
| "PD.1 should support probe nodes in 10 Chinese cities" | 3 cities (Beijing, Shanghai, Guangzhou) is enough for V1. Coverage expansion is future. |
| "PD.2 should detect all known GFW techniques" | DNS + TCP + TLS + HTTP covers 95% of blocking. QUIC/UDP blocking is future. |
| "PD.3 should use ML for verdict classification" | Decision tree is proven (OONI uses it). ML adds complexity without clear benefit at our scale. |
| "PD.4 should show a geo-map of blocked regions" | Table + timeline is sufficient. Map requires geo-library integration. |
| "PD.5 should support CDN provider switching (not just DNS)" | DNS A/CNAME switch is the minimal viable failover. CDN API integration is future. |
| "PD.5 should auto-provision backup infrastructure" | Backup IP/CDN must be pre-configured by operator. Auto-provisioning is out of scope. |
| "PD.6 should predict future blocking events" | We detect, not predict. Prediction requires ML + historical analysis. |

---

## Operational Prerequisites (Before Starting Phase D)

These are NOT code tasks but infrastructure/operational requirements:

1. **CN VPS provisioned** — 3 VPS instances in China (Beijing/Shanghai/Guangzhou)
   with public IP, able to reach platform control plane via HTTPS
2. **Control VPS provisioned** — 2 instances outside GFW (Hong Kong, Tokyo)
3. **Network path verified** — probe nodes can reach control plane; control
   plane can receive measurement reports
4. **mTLS CA ready** — same CA as agent protocol, issue certs for probe nodes
5. **Monitoring for probe nodes** — Phase C probes should monitor the probe
   nodes themselves (meta-monitoring)
6. **Legal review** — confirm running network probes from CN VPS is compliant
   with local regulations
7. **Backup CDN/IP ready** — operators must have pre-configured backup
   infrastructure before enabling auto-failover

---

## References

- `docs/ARCHITECTURE_ROADMAP.md` §7 — Phase D overview
- `docs/analysis/OONI_PROBE_ANALYSIS.md` — Detection methodology, decision tree, measurement model
- `docs/analysis/DNSCONTROL_ANALYSIS.md` — Provider interface (used by failover for DNS changes)
- `docs/analysis/OCTODNS_ANALYSIS.md` — Safety (failover uses plan/apply)
- `ARCHITECTURE.md` §3 "Pull Agent Design" — binary hardening pattern reused for probe
- `CLAUDE.md` Critical Rule #3 (whitelist-only binary), Critical Rule #8 (alert dedup)
- ADR-0003 D11 — GFW vertical parked, will return as Phase D
