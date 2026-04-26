# PHASE_C_TASKLIST.md — Monitoring & Alerting Work Order (PC.1 ✅ PC.2 ✅ PC.3 ✅ PC.4 ✅ PC.5 ✅ PC.6 ✅)

> **Created 2026-04-21.** This document is the authoritative work order for
> Phase C (Monitoring & Alerting) of the platform restructuring.
>
> **Pre-requisite**: Phase A complete (PA.1–PA.7 minimum: domain asset data,
> SSL cert tracking, expiry alerts). Phase 1-2 release pipeline (for L2
> deployment verification). Phase B is NOT strictly required — C can start
> in parallel with B after Phase A.
>
> **Audience**: Claude Code sessions (Opus for alert engine + probe
> correctness, Sonnet for status page/UI/notification tasks).

---

## Phase C — Definition of Scope

Phase C adds **operational observability**: probes verify that domains are
alive and releases landed, alerts fire on failures, status pages communicate
health to stakeholders, and maintenance windows suppress noise during planned
work.

### What "Phase C done" looks like (acceptance demo)

```
1. System runs L1 probe every 5 minutes: DNS resolves, TCP :443 open,
   HTTP 200 for all 500 active domains
2. After a release completes, L2 probe fires: fetches each domain, checks
   <meta name="release-version"> matches the artifact_id just deployed
3. L3 probe runs every 1 min for 20 "core"-tagged domains: checks specific
   API endpoints return expected responses
4. Domain goes DOWN (HTTP timeout) → state change detected → P2 alert fires
   → Telegram message: "example.com is DOWN (HTTP timeout, 3 retries failed)"
5. Same domain stays DOWN → no duplicate alert for 1 hour (dedup)
6. Domain comes back UP → INFO notification: "example.com recovered after 12min"
7. Public status page at status.company.com shows: 3 groups (Web, API, CDN),
   each with uptime bars (99.9% / 30d) + current status green/red
8. Operator posts incident: "CDN provider degraded" → appears on status page
9. Planned maintenance: operator schedules 02:00–04:00 UTC window for
   host-group "nginx-asia" → during window, probe marks MAINTENANCE (not DOWN),
   alerts suppressed, status page shows "Under Maintenance"
10. Uptime dashboard: charts per domain (response time + uptime %), calendar
    heatmap, "worst 10" table
```

### What is OUT of Phase C (do not implement)

| Feature | Phase | Reason |
|---|---|---|
| GFW censorship detection (probe from CN) | Phase D | Different probe infrastructure |
| Auto-failover on DOWN | Phase D | Needs DNS switch capability (Phase B) + policy |
| Auto-rollback on L2 failure | Future | Complex decision; manual first |
| Distributed probe nodes (multi-region) | Phase D | Phase C runs probes from platform host |
| Real-time WebSocket dashboard updates | Future | Polling (10s) is sufficient |
| SLA reporting with financial penalties | Future | Uptime data enables this later |
| Synthetic transaction monitoring | Future | L3 is HTTP-level only |
| APM / tracing integration | Out of scope | Not our domain |

---

## Dependency Graph

```
              PC.6 (Notification Hub — can start early, independent)
                │
    PC.1 (Probe Engine L1/L2/L3 — core infrastructure)
       │
       ├─────────────────────────────┐
       ▼                             ▼
    PC.2                          PC.5
  Alert Engine                 Uptime Dashboard
  + Dedup                      (needs probe data)
       │
       ├─────────────────────────────┐
       ▼                             ▼
    PC.3                          PC.4
  Status Page                  Maintenance
  (shows alert                 Windows
   status + uptime)            (suppresses alerts)
```

### Critical path

`PC.1 → PC.2 → PC.3`

### Parallelization rules

- PC.6 (Notification Hub) is independent — can start any time
- PC.1 is the foundation; everything else needs probe data
- PC.2 depends on PC.1 (state changes trigger alerts)
- PC.3 and PC.4 can parallel after PC.2 (status page shows status; maintenance suppresses alerts)
- PC.5 depends on PC.1 (needs accumulated probe data for charts)
- PC.4 must integrate with PC.2 (maintenance = suppress alerts)

---

## Task Cards

---

### PC.1 — Probe Engine L1 / L2 / L3 **(Opus)**

**Owner**: **Opus** — probe correctness determines everything downstream
(false positives = alert fatigue, false negatives = missed outages)
**Status**: ✅ COMPLETED 2026-04-22
**Depends on**: Phase A (domain inventory with lifecycle_state), Phase 1-2
(release pipeline for L2 verification)
**Reads first**: `ARCHITECTURE.md` §2.7 "Probe Subsystem",
`docs/analysis/UPTIME_KUMA_ANALYSIS.md` §2 "Monitor Schema" + §6 "Uptime
Calculation", CLAUDE.md Critical Rule #8

**Context**: Phase 1 left probes as stubs. This task implements the actual
probe execution: connect to domains, check health at 3 tiers, store results
in TimescaleDB, detect state changes.

**Scope (in)**:

- `internal/probe/engine.go` — probe execution engine:
  ```go
  type Engine struct {
      store      ProbeResultStore
      domains    DomainStore
      logger     *zap.Logger
  }

  func (e *Engine) RunL1(ctx, domain) (*ProbeResult, error)
  func (e *Engine) RunL2(ctx, domain, expectedArtifactID) (*ProbeResult, error)
  func (e *Engine) RunL3(ctx, domain, healthConfig) (*ProbeResult, error)
  ```

- L1 probe (availability):
  - DNS resolution (system resolver): resolve FQDN → IPs
  - TCP connect to first resolved IP:443 (timeout 5s)
  - HTTP GET to `https://{fqdn}/` (timeout 8s, follow redirects max 5)
  - Check: status code in acceptable range (200-399 default)
  - Measure: DNS time, TCP time, TLS time, total response time
  - Result: UP (all pass), DOWN (any fail), with detail on which step failed

- L2 probe (deployment verification):
  - HTTP GET to `https://{fqdn}/`
  - Parse response body for `<meta name="release-version" content="{id}">`
  - Compare with expected artifact_id (from most recent succeeded release)
  - Result: UP (matches), DOWN (mismatch or missing meta tag)
  - Also checks: HTTP 200, response body non-empty

- L3 probe (business health):
  - Configurable per domain via `probe_policies` table:
    - URL path, expected status code, expected body keyword, timeout
  - HTTP GET to configured path
  - Check: status matches + keyword found in body (if configured)
  - Result: UP / DOWN with detail

- `store/timescale/probe.go` — probe result storage:
  ```go
  type ProbeResult struct {
      ID            int64     `db:"id"`
      DomainID      int64     `db:"domain_id"`
      ProbeType     string    `db:"probe_type"`    // "l1", "l2", "l3"
      Status        string    `db:"status"`        // "up", "down", "maintenance"
      ResponseTimeMS int64    `db:"response_time_ms"`
      DNSTimeMS     int64     `db:"dns_time_ms"`
      TLSTimeMS     int64     `db:"tls_time_ms"`
      StatusCode    int       `db:"status_code"`
      Error         string    `db:"error"`
      Detail        JSONB     `db:"detail"`        // per-step breakdown
      MeasuredAt    time.Time `db:"measured_at"`
  }
  ```

- TimescaleDB hypertable setup:
  ```sql
  CREATE TABLE probe_results (
      id              BIGSERIAL,
      domain_id       BIGINT NOT NULL,
      probe_type      VARCHAR(8) NOT NULL,
      status          VARCHAR(16) NOT NULL,
      response_time_ms INT,
      dns_time_ms     INT,
      tls_time_ms     INT,
      status_code     SMALLINT,
      error           TEXT,
      detail          JSONB,
      measured_at     TIMESTAMPTZ NOT NULL,
      PRIMARY KEY (measured_at, id)
  );

  SELECT create_hypertable('probe_results', 'measured_at');
  SELECT add_retention_policy('probe_results', INTERVAL '90 days');
  ```

- `probe_policies` table (L3 configuration):
  ```sql
  CREATE TABLE probe_policies (
      id              BIGSERIAL PRIMARY KEY,
      domain_id       BIGINT NOT NULL REFERENCES domains(id),
      probe_type      VARCHAR(8) NOT NULL DEFAULT 'l3',
      url_path        VARCHAR(512) NOT NULL DEFAULT '/',
      method          VARCHAR(8) NOT NULL DEFAULT 'GET',
      expected_status INT NOT NULL DEFAULT 200,
      expected_keyword VARCHAR(255),
      timeout_ms      INT NOT NULL DEFAULT 8000,
      interval_seconds INT NOT NULL DEFAULT 300,
      retries         INT NOT NULL DEFAULT 3,
      retry_interval_ms INT NOT NULL DEFAULT 2000,
      enabled         BOOLEAN NOT NULL DEFAULT true,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```

- State change detection:
  ```go
  type StateTracker struct {
      redis *redis.Client
  }

  // Returns true if status changed from previous
  func (t *StateTracker) RecordAndDetect(ctx, domainID, probeType, status) (changed bool, prevStatus string, err error)
  // Uses Redis key: probe:state:{domain_id}:{probe_type} → "up"|"down"
  ```

- asynq task scheduling:
  - `probe:run_l1` — dispatched every 5 minutes for all active domains (batched)
  - `probe:run_l2` — dispatched after each release succeeds (per-release, per-domain)
  - `probe:run_l3` — dispatched per probe_policy interval (1 min for core domains)
  - Scheduler: periodic tasks in `cmd/worker/main.go`

- Retry logic (from Uptime Kuma):
  - L1: 3 retries with 2s interval before marking DOWN
  - Prevents transient network glitches from triggering false alerts
  - Only marks DOWN after all retries exhausted

**Scope (out)**:

- Alert dispatch (PC.2 — state changes are detected here, alerts fire there)
- Uptime calculation / aggregation (PC.5)
- Maintenance suppression (PC.4)
- Multi-region probing (Phase D)
- Custom probe scripts / synthetic transactions

**Deliverables**:

- Probe engine (L1 + L2 + L3 execution)
- TimescaleDB schema + store implementation
- probe_policies table + CRUD
- State change detection (Redis-based)
- asynq task handlers + scheduling
- Retry logic

**Acceptance**:

- L1 probe against a live HTTPS site → result stored with correct timings
- L1 probe against unreachable host → retries 3x → marks DOWN with error detail
- L2 probe with correct artifact_id meta tag → UP
- L2 probe with wrong/missing meta tag → DOWN
- L3 probe with keyword match → UP; without → DOWN
- State change: UP→DOWN detected → `changed=true` returned
- Repeated DOWN → `changed=false` (no re-trigger)
- probe_results in TimescaleDB with correct hypertable partitioning
- Retention policy: data older than 90 days auto-deleted
- `go test -race ./internal/probe/...` passes
- 500 domains × L1 every 5 min = ~100 checks/min throughput verified

---

### PC.2 — Alert Engine + Dedup **(Opus)**

**Owner**: **Opus** — alert correctness is critical (missed alert = outage
undetected; over-alerting = operator fatigue = alerts ignored)
**Status**: ✅ COMPLETED 2026-04-22
**Depends on**: PC.1 (state changes trigger alerts), PC.6 (notification dispatch)
**Reads first**: `CLAUDE.md` Critical Rule #8, `docs/analysis/UPTIME_KUMA_ANALYSIS.md`
§4 "Notification Architecture", `ARCHITECTURE.md` §2.8 "Alert & Notification"

**Context**: When a probe detects a state change (UP→DOWN or DOWN→UP), the
alert engine creates an alert event, applies dedup rules, assigns severity,
and dispatches notifications through the configured channels.

**Scope (in)**:

- `internal/alert/engine.go`:
  ```go
  type Engine struct {
      store    AlertStore
      notify   NotificationDispatcher
      redis    *redis.Client
      logger   *zap.Logger
  }

  func (e *Engine) ProcessStateChange(ctx, StateChange) error
  func (e *Engine) CheckDedup(ctx, target, alertType, severity) (bool, error)
  func (e *Engine) CreateAlert(ctx, AlertInput) (*AlertEvent, error)
  func (e *Engine) AcknowledgeAlert(ctx, alertID, userID, note) error
  func (e *Engine) ResolveAlert(ctx, alertID) error
  ```

- Alert event model:
  ```sql
  CREATE TABLE alert_events (
      id              BIGSERIAL PRIMARY KEY,
      uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
      target_type     VARCHAR(32) NOT NULL,     -- "domain", "agent", "release", "ssl_cert"
      target_id       BIGINT NOT NULL,
      target_name     VARCHAR(255) NOT NULL,    -- FQDN or agent hostname (for display)
      alert_type      VARCHAR(64) NOT NULL,     -- "probe_down", "probe_recovered", "ssl_expiring", "drift_detected"
      severity        VARCHAR(8) NOT NULL,      -- "p1", "p2", "p3", "info"
      status          VARCHAR(32) NOT NULL DEFAULT 'firing', -- firing, acknowledged, resolved
      message         TEXT NOT NULL,
      detail          JSONB,                    -- probe result, error context
      fired_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      acknowledged_at TIMESTAMPTZ,
      acknowledged_by BIGINT REFERENCES users(id),
      resolved_at     TIMESTAMPTZ,
      resolved_by     BIGINT REFERENCES users(id),
      acknowledge_note TEXT,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE INDEX idx_alerts_target ON alert_events(target_type, target_id);
  CREATE INDEX idx_alerts_status ON alert_events(status);
  CREATE INDEX idx_alerts_severity ON alert_events(severity);
  CREATE INDEX idx_alerts_fired ON alert_events(fired_at);
  ```

- Severity assignment rules:
  | Trigger | Severity | Action |
  |---|---|---|
  | Core domain DOWN (tag "core") | P1 | Page immediately |
  | Any domain DOWN | P2 | Notify (Telegram channel) |
  | Domain recovered | INFO | Notify |
  | SSL cert expiring (7d) | P2 | Notify |
  | SSL cert expired | P1 | Page |
  | Multiple hosts in shard failed (>20%) | P1 | Page + pause releases |
  | DNS drift detected | P3 | Log, daily digest |
  | Agent offline > 5 min | P2 | Notify |

- Dedup logic (Critical Rule #8):
  - Redis key: `alert:dedup:{target_type}:{target_id}:{alert_type}:{severity}`
  - TTL: 3600 seconds (1 hour)
  - If key exists → suppress (don't create new alert_event, don't notify)
  - On state change back (DOWN→UP) → delete dedup key + create "resolved" alert

- Batch aggregation:
  - If 5+ domains go DOWN within 60 seconds → batch into one notification:
    "5 domains DOWN: a.com, b.com, c.com, d.com, e.com"
  - Implementation: 30-second buffer in Redis list before dispatching
    `alert:batch:{severity}` → RPUSH events → after 30s, process batch

- Alert lifecycle:
  ```
  firing → acknowledged (operator saw it)
       → resolved (issue fixed, manually or auto-detected)
  ```
  - Auto-resolve: when probe state returns to UP → resolve matching firing alert

- API:
  - `GET /api/v1/alerts` — list (filter: status, severity, target_type, date range)
  - `GET /api/v1/alerts/:id` — detail
  - `POST /api/v1/alerts/:id/acknowledge` — `{ "note": "investigating" }`
  - `POST /api/v1/alerts/:id/resolve` — manual resolve
  - `GET /api/v1/alerts/stats` — count by severity × status

**Scope (out)**:

- Escalation policies (P2 → P1 after 30 min unacknowledged) — future
- On-call rotation / PagerDuty-style scheduling — future
- Alert correlation (group related alerts) — future
- Alert suppression rules (beyond maintenance windows) — future

**Deliverables**:

- Alert engine with severity assignment + dedup + batch aggregation
- alert_events table + store
- Auto-resolve on recovery
- API (list, detail, acknowledge, resolve)
- Integration with PC.1 state changes
- Integration with PC.6 notification dispatch

**Acceptance**:

- Probe DOWN → alert_event created with severity P2, status="firing"
- Same domain DOWN again within 1 hour → NO new alert (dedup)
- Domain recovers → alert auto-resolved + INFO notification sent
- 5 domains DOWN within 30s → ONE batched notification
- Core-tagged domain DOWN → P1 severity
- Acknowledge alert → status="acknowledged", acknowledged_by set
- `GET /api/v1/alerts?status=firing&severity=p1` returns correct list
- Dedup Redis key expires after 1 hour → next DOWN creates new alert
- `go test -race ./internal/alert/...` passes

---

### PC.3 — Public Status Page ✅ (完成)

**Owner**: Sonnet
**Status**: ✅ COMPLETED 2026-04-26
**Depends on**: PC.1 (probe data for uptime bars), PC.2 (alert status for
current state display)
**Reads first**: `docs/analysis/UPTIME_KUMA_ANALYSIS.md` §4 "Status Page Model"

**Context**: Public status pages let customers/stakeholders see service health
without needing platform login. Groups organize monitors; uptime bars show
history; incidents communicate ongoing issues.

**Scope (in)**:

- Tables (from ARCHITECTURE_ROADMAP.md):
  ```sql
  CREATE TABLE status_pages (
      id              BIGSERIAL PRIMARY KEY,
      uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
      slug            VARCHAR(128) NOT NULL UNIQUE,
      title           VARCHAR(255) NOT NULL,
      description     TEXT,
      published       BOOLEAN NOT NULL DEFAULT true,
      password_hash   VARCHAR(255),
      custom_domain   VARCHAR(255),
      theme           VARCHAR(32) DEFAULT 'default',
      logo_url        VARCHAR(512),
      footer_text     TEXT,
      custom_css      TEXT,
      auto_refresh_seconds INT NOT NULL DEFAULT 60,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE TABLE status_page_groups (
      id              BIGSERIAL PRIMARY KEY,
      status_page_id  BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
      name            VARCHAR(128) NOT NULL,
      sort_order      INT NOT NULL DEFAULT 0
  );

  CREATE TABLE status_page_monitors (
      id              BIGSERIAL PRIMARY KEY,
      group_id        BIGINT NOT NULL REFERENCES status_page_groups(id) ON DELETE CASCADE,
      domain_id       BIGINT NOT NULL REFERENCES domains(id),
      display_name    VARCHAR(128),         -- override display (hide real FQDN if needed)
      sort_order      INT NOT NULL DEFAULT 0
  );

  CREATE TABLE status_page_incidents (
      id              BIGSERIAL PRIMARY KEY,
      status_page_id  BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
      title           VARCHAR(255) NOT NULL,
      content         TEXT,                 -- Markdown body
      severity        VARCHAR(32) NOT NULL DEFAULT 'info',  -- info, warning, danger
      pinned          BOOLEAN NOT NULL DEFAULT false,
      active          BOOLEAN NOT NULL DEFAULT true,
      created_by      BIGINT REFERENCES users(id),
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```

- `internal/statuspage/service.go`:
  - Status page CRUD
  - Group CRUD (within a page)
  - Monitor assignment (link domain to group)
  - Incident CRUD
  - `GetPublicStatus(ctx, slug)` — compute current status for public display:
    - Per-monitor: current status (from latest probe), uptime % (24h/7d/30d)
    - Per-group: worst status among members
    - Active incidents
    - Active maintenance windows affecting this page's domains

- Public API (no auth required, except password-protected pages):
  - `GET /status/:slug` — public status page data (JSON)
  - `GET /status/:slug/uptime/:domain_id` — uptime history (for mini charts)
  - Password check: `POST /status/:slug/auth` → returns session token

- Admin API (requires auth):
  - `POST /api/v1/status-pages` — create
  - `GET /api/v1/status-pages` — list
  - `PUT /api/v1/status-pages/:id` — update
  - `DELETE /api/v1/status-pages/:id` — delete
  - `POST /api/v1/status-pages/:id/groups` — add group
  - `PUT /api/v1/status-pages/:id/groups/:gid` — update group
  - `POST /api/v1/status-pages/:id/groups/:gid/monitors` — add monitor
  - `POST /api/v1/status-pages/:id/incidents` — create incident
  - `PUT /api/v1/status-pages/:id/incidents/:iid` — update incident

- Frontend (admin side):
  - `web/src/views/status-pages/StatusPageList.vue` — manage pages
  - `web/src/views/status-pages/StatusPageEditor.vue` — configure groups + monitors + settings
  - `web/src/views/status-pages/IncidentEditor.vue` — write incidents

- Frontend (public side — separate route, minimal layout):
  - `web/src/views/public/StatusPagePublic.vue`:
    - Header: logo, title, description
    - Overall status indicator (all operational / partial outage / major outage)
    - Groups with monitors: status dot + name + uptime bar (90 days, colored cells)
    - Active incidents section (pinned + recent)
    - Auto-refresh every N seconds
    - Responsive (mobile-friendly)

**Scope (out)**:

- Custom domain SSL (handled by Caddy config externally)
- RSS/Atom feed for incidents
- Email subscription for status updates
- Embedded status badge (image URL)
- Multi-language status pages

**Deliverables**:

- Status page tables + service
- Public API (no-auth) + admin API
- Admin UI: page editor + incident management
- Public UI: status page with uptime bars + incidents
- Password protection support
- `npm run build` + `go test ./...` passes

**Acceptance**:

- Create status page with slug "main" → accessible at `/status/main`
- Add 2 groups ("Web", "API") with 3 monitors each → displays correctly
- Monitor goes DOWN → status page shows red dot within 60s refresh
- All monitors UP → "All Systems Operational" banner
- Post incident with severity "danger" → appears on public page
- Password-protected page → shows auth form, correct password → access granted
- Uptime bars show 90-day history (green/red/gray cells)
- Mobile responsive (viewport 375px width works)
- `npm run build` clean

---

### PC.4 — Maintenance Windows ✅ (完成)

**Owner**: Sonnet
**Status**: ✅ COMPLETED 2026-04-26
**Depends on**: PC.2 (alert engine — maintenance suppresses alerts)
**Reads first**: `docs/analysis/UPTIME_KUMA_ANALYSIS.md` §7 "Maintenance Model"

**Context**: During planned maintenance (server migrations, upgrades), probes
will detect "DOWN" but operators shouldn't be paged. Maintenance windows
suppress alerts and show correct status on public pages.

**Scope (in)**:

- Tables:
  ```sql
  CREATE TABLE maintenance_windows (
      id              BIGSERIAL PRIMARY KEY,
      uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
      title           VARCHAR(150) NOT NULL,
      description     TEXT,
      strategy        VARCHAR(32) NOT NULL DEFAULT 'single',
                      -- "single" (one-time), "recurring_weekly", "recurring_monthly", "cron"
      start_at        TIMESTAMPTZ,          -- for single: exact start
      end_at          TIMESTAMPTZ,          -- for single: exact end
      recurrence      JSONB,                -- for recurring: {"weekdays":[1,5],"start_time":"02:00","duration_minutes":120,"timezone":"UTC"}
      active          BOOLEAN NOT NULL DEFAULT true,
      created_by      BIGINT REFERENCES users(id),
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE TABLE maintenance_window_targets (
      id              BIGSERIAL PRIMARY KEY,
      maintenance_id  BIGINT NOT NULL REFERENCES maintenance_windows(id) ON DELETE CASCADE,
      target_type     VARCHAR(32) NOT NULL,  -- "domain", "host_group", "project"
      target_id       BIGINT NOT NULL,
      UNIQUE(maintenance_id, target_type, target_id)
  );
  ```

- `internal/maintenance/service.go`:
  - CRUD for maintenance windows + targets
  - `IsInMaintenance(ctx, domainID, now) (bool, *MaintenanceWindow, error)`:
    - Check single windows: `start_at <= now <= end_at`
    - Check recurring: compute next occurrence, check if now falls within
    - Check via targets: domain directly, OR domain's host_group, OR domain's project
  - `GetActiveWindows(ctx) ([]MaintenanceWindow, error)` — currently active

- Integration with probe engine (PC.1):
  - Before recording probe result: check `IsInMaintenance(domain_id)`
  - If in maintenance → record result with `status = "maintenance"` (not "down")
  - Maintenance status does NOT count against uptime percentage

- Integration with alert engine (PC.2):
  - In `ProcessStateChange()`: if domain is in maintenance → suppress alert
  - Do NOT create alert_event for maintenance-period failures
  - When maintenance ends and domain is still DOWN → NOW create alert

- Integration with status page (PC.3):
  - `GetPublicStatus()`: domains in maintenance show "Under Maintenance" badge
    (distinct from "DOWN")

- API:
  - `POST /api/v1/maintenance` — create window
  - `GET /api/v1/maintenance` — list (active/upcoming/past)
  - `GET /api/v1/maintenance/:id` — detail with targets
  - `PUT /api/v1/maintenance/:id` — update
  - `DELETE /api/v1/maintenance/:id` — delete
  - `POST /api/v1/maintenance/:id/targets` — add targets
  - `DELETE /api/v1/maintenance/:id/targets/:tid` — remove target

- Frontend:
  - `web/src/views/maintenance/MaintenanceList.vue`:
    - List windows: title, strategy, next occurrence, targets, active toggle
    - Create form: title, strategy picker, schedule config, target selector
  - Domain detail: "Maintenance" badge when domain is in active window
  - Status page: maintenance indicator per affected monitor

**Scope (out)**:

- Auto-create maintenance from release schedules
- Maintenance window approval workflow
- Nested/inherited maintenance (project → all domains)
- Maintenance notification to subscribers ("maintenance starting in 1 hour")

**Deliverables**:

- Maintenance tables + service
- `IsInMaintenance()` check (single + recurring)
- Integration: probe records "maintenance", alerts suppressed, status page shows badge
- API + frontend
- `go test ./internal/maintenance/...` passes

**Acceptance**:

- Create single maintenance window (02:00–04:00 today) for domain X
- During window: L1 probe records status="maintenance" (not "down")
- During window: no alert fires for domain X
- After window: if domain still down → alert fires immediately
- Status page shows "Under Maintenance" for domain X during window
- Recurring weekly (Mon+Fri 02:00–04:00 UTC) → correctly computed
- Uptime calculation: maintenance period excluded (doesn't lower uptime %)
- Delete maintenance window → domain's next DOWN triggers alert normally
- `go test ./internal/maintenance/...` passes

---

### PC.5 — Uptime Dashboard ✅ (完成)

**Owner**: Sonnet
**Status**: ✅ COMPLETED 2026-04-26
**Depends on**: PC.1 (probe data must be accumulating)
**Reads first**: `docs/analysis/UPTIME_KUMA_ANALYSIS.md` §6 "Uptime Calculation"

**Context**: Operators need visual insight into domain health over time:
uptime percentage, response time trends, worst performers, and historical
patterns. TimescaleDB continuous aggregates provide efficient queries.

**Scope (in)**:

- TimescaleDB continuous aggregates:
  ```sql
  CREATE MATERIALIZED VIEW probe_stats_hourly
  WITH (timescaledb.continuous) AS
  SELECT
      domain_id,
      probe_type,
      time_bucket('1 hour', measured_at) AS bucket,
      COUNT(*) FILTER (WHERE status = 'up') AS up_count,
      COUNT(*) FILTER (WHERE status = 'down') AS down_count,
      COUNT(*) FILTER (WHERE status = 'maintenance') AS maintenance_count,
      AVG(response_time_ms) AS avg_response_ms,
      MIN(response_time_ms) AS min_response_ms,
      MAX(response_time_ms) AS max_response_ms,
      PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY response_time_ms) AS p95_response_ms
  FROM probe_results
  GROUP BY domain_id, probe_type, bucket;

  CREATE MATERIALIZED VIEW probe_stats_daily
  WITH (timescaledb.continuous) AS
  SELECT
      domain_id,
      probe_type,
      time_bucket('1 day', bucket) AS bucket,
      SUM(up_count) AS up_count,
      SUM(down_count) AS down_count,
      SUM(maintenance_count) AS maintenance_count,
      AVG(avg_response_ms) AS avg_response_ms,
      MIN(min_response_ms) AS min_response_ms,
      MAX(max_response_ms) AS max_response_ms
  FROM probe_stats_hourly
  GROUP BY domain_id, probe_type, bucket;

  -- Refresh policies
  SELECT add_continuous_aggregate_policy('probe_stats_hourly', '2 hours', '1 hour', '1 hour');
  SELECT add_continuous_aggregate_policy('probe_stats_daily', '2 days', '1 day', '1 day');
  ```

- `internal/probe/analytics.go`:
  - `GetUptime(ctx, domainID, probeType, duration) (float64, error)` — percentage
  - `GetResponseTimeSeries(ctx, domainID, probeType, from, to, granularity) ([]DataPoint, error)`
  - `GetWorstPerformers(ctx, probeType, duration, limit) ([]DomainUptime, error)`
  - `GetUptimeCalendar(ctx, domainID, year, month) ([]DayStatus, error)` — per-day status

- API:
  - `GET /api/v1/probes/uptime/:domain_id?duration=30d&probe_type=l1`
  - `GET /api/v1/probes/response-time/:domain_id?from=...&to=...&granularity=1h`
  - `GET /api/v1/probes/worst?duration=7d&limit=10`
  - `GET /api/v1/probes/calendar/:domain_id?year=2026&month=4`
  - `GET /api/v1/probes/overview` — all domains, current status + 24h uptime

- Frontend:
  - `web/src/views/dashboard/UptimeDashboard.vue`:
    - Overview cards: total monitored, currently up, currently down, avg uptime
    - Worst performers table (domain, uptime %, current status, avg response)
    - Filter: probe type, project, time range
  - `web/src/views/domains/DomainDetail.vue` — "Monitoring" tab:
    - Uptime percentage badges: 24h / 7d / 30d
    - Response time line chart (ECharts or Chart.js)
    - Calendar heatmap (green=100%, yellow=<99.9%, red=<99%, gray=no data)
    - Recent probe results table (last 20)
  - `web/src/components/UptimeBar.vue` — reusable uptime bar component
    (90 cells, color per day, tooltip with details)

**Scope (out)**:

- Real-time response time (current value is in probe result, not aggregated)
- Comparison across time periods ("this week vs last week")
- Anomaly detection (statistical outlier alerting)
- SLA compliance reporting with thresholds
- Export to PDF/CSV

**Deliverables**:

- TimescaleDB continuous aggregates (hourly + daily)
- Analytics service (uptime, response time series, worst performers, calendar)
- API endpoints
- Frontend: uptime dashboard + domain monitoring tab + UptimeBar component
- `npm run build` + `go test ./...` passes

**Acceptance**:

- Domain with 100 checks, 98 UP → uptime = 98.0%
- Maintenance checks excluded from calculation (not counted as DOWN)
- Response time chart shows hourly data points for last 7 days
- Calendar heatmap: day with 0 DOWN = green, day with any DOWN = yellow/red
- Worst performers: correct ordering by uptime % ascending
- `GET /api/v1/probes/uptime/:id?duration=30d` returns correct percentage
- Continuous aggregates auto-refresh (hourly policy working)
- `npm run build` clean

---

### PC.6 — Notification Hub ✅ (完成)

**Owner**: Sonnet
**Status**: ✅ COMPLETED 2026-04-25
**Depends on**: Phase A (basic `pkg/notify` exists with Telegram + Webhook)
**Reads first**: `docs/analysis/UPTIME_KUMA_ANALYSIS.md` §3 "Notification
Architecture"

**Context**: Phase A has basic notification (Telegram + Webhook for expiry
alerts). Phase C needs a full notification system: reusable configs, multiple
channels, many-to-many linking, test capability, history, and escalation.

**Scope (in)**:

- Tables:
  ```sql
  CREATE TABLE notification_channels (
      id              BIGSERIAL PRIMARY KEY,
      uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
      name            VARCHAR(128) NOT NULL,
      channel_type    VARCHAR(32) NOT NULL,   -- "telegram", "slack", "webhook", "email", "pagerduty"
      config          JSONB NOT NULL,          -- type-specific config (encrypted secrets)
      is_default      BOOLEAN NOT NULL DEFAULT false,
      enabled         BOOLEAN NOT NULL DEFAULT true,
      created_by      BIGINT REFERENCES users(id),
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  -- Many-to-many: which channels get which alert types
  CREATE TABLE notification_rules (
      id              BIGSERIAL PRIMARY KEY,
      channel_id      BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
      alert_type      VARCHAR(64),            -- NULL = all types
      min_severity    VARCHAR(8) NOT NULL DEFAULT 'p3',  -- only fire for this severity or higher
      target_type     VARCHAR(32),            -- NULL = all targets; "project", "domain"
      target_id       BIGINT,                 -- specific project/domain, NULL = global
      enabled         BOOLEAN NOT NULL DEFAULT true,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );

  CREATE TABLE notification_history (
      id              BIGSERIAL PRIMARY KEY,
      channel_id      BIGINT NOT NULL REFERENCES notification_channels(id),
      alert_event_id  BIGINT REFERENCES alert_events(id),
      status          VARCHAR(32) NOT NULL,   -- "sent", "failed", "suppressed"
      message         TEXT,
      error           TEXT,
      sent_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```

- `internal/alert/dispatcher.go` — notification dispatch:
  ```go
  type Dispatcher struct {
      channels ChannelStore
      rules    RuleStore
      history  HistoryStore
      senders  map[string]Sender  // "telegram" → TelegramSender, etc.
  }

  func (d *Dispatcher) Dispatch(ctx, alert *AlertEvent) error
  // 1. Find matching rules (by alert_type, severity, target)
  // 2. For each matched rule → get channel → send via appropriate sender
  // 3. Record in notification_history
  // 4. On failure: log error, record failed status, retry once
  ```

- `pkg/notify/` — extend with new senders:
  - `pkg/notify/telegram.go` — exists, refactor to implement Sender interface
  - `pkg/notify/webhook.go` — exists, refactor
  - `pkg/notify/slack.go` — NEW: Slack incoming webhook
  - `pkg/notify/email.go` — NEW: SMTP email
  - Common interface:
    ```go
    type Sender interface {
        Send(ctx context.Context, config json.RawMessage, message Message) error
        Test(ctx context.Context, config json.RawMessage) error
    }
    ```

- Escalation (simple):
  - P1 alerts: dispatch immediately to all P1-configured channels
  - P2 alerts: buffer for 30 seconds (batch), then dispatch
  - P3/INFO: buffer for 5 minutes (daily digest option)

- API:
  - `POST /api/v1/notifications/channels` — create channel
  - `GET /api/v1/notifications/channels` — list
  - `PUT /api/v1/notifications/channels/:id` — update
  - `DELETE /api/v1/notifications/channels/:id` — delete
  - `POST /api/v1/notifications/channels/:id/test` — send test message
  - `POST /api/v1/notifications/rules` — create rule
  - `GET /api/v1/notifications/rules` — list rules
  - `DELETE /api/v1/notifications/rules/:id` — delete rule
  - `GET /api/v1/notifications/history` — recent notifications (paginated)

- Frontend:
  - `web/src/views/settings/NotificationChannelList.vue`:
    - List channels with type icon, name, enabled toggle, "Test" button
    - Create/edit form: type selector → type-specific config fields
  - `web/src/views/settings/NotificationRuleList.vue`:
    - Rules table: channel, alert type filter, severity filter, target filter
    - Create rule: select channel + conditions
  - `web/src/views/settings/NotificationHistory.vue`:
    - Recent sends with status (sent/failed), timestamp, message preview

**Scope (out)**:

- PagerDuty / Opsgenie integration (add later via same Sender interface)
- SMS notifications
- Phone call escalation
- On-call rotation scheduling
- Notification templates (message formatting customization)

**Deliverables**:

- notification_channels + notification_rules + notification_history tables
- Dispatcher service (rule matching + send + history)
- Sender interface + Slack + Email implementations (+ refactored Telegram/Webhook)
- Test endpoint (send test message to channel)
- Escalation logic (immediate P1 / buffered P2 / digest P3)
- API + frontend
- `npm run build` + `go test ./...` passes

**Acceptance**:

- Create Telegram channel with bot_token + chat_id → saved
- "Test" button → test message arrives in Telegram
- Create Slack channel → test → message arrives in Slack
- Create rule: "P1 alerts for project X → Telegram channel" → saved
- P1 alert fires for project X domain → Telegram notification sent
- P2 alert fires → buffered 30s → sent (possibly batched with others)
- Notification history shows sent/failed status for each dispatch
- Failed send → recorded with error detail → visible in history
- Channel disabled → no notifications dispatched through it
- `go test ./internal/alert/... ./pkg/notify/...` passes

---

## Phase C Effort Estimate

| # | Task | Owner | Lo | Hi | Risk | Notes |
|---|---|---|---|---|---|---|
| PC.1 | Probe Engine L1/L2/L3 | **Opus** | 2.5 | 4.0 | 🔴 | Core infrastructure; TimescaleDB setup; retry logic; throughput |
| PC.2 | Alert Engine + Dedup | **Opus** | 2.0 | 3.5 | 🔴 | Dedup correctness; batch aggregation; auto-resolve |
| PC.3 | Public Status Page | Sonnet | 2.0 | 3.0 | 🟡 | Public route + auth + uptime bars + incidents |
| PC.4 | Maintenance Windows | Sonnet | 1.5 | 2.5 | 🟡 | Recurring schedule computation; integration points |
| PC.5 | Uptime Dashboard | Sonnet | 1.5 | 2.5 | 🟢 | Continuous aggregates + charts (data is ready from PC.1) |
| PC.6 | Notification Hub | Sonnet | 2.0 | 3.0 | 🟡 | Multi-channel; rule matching; Slack/Email impl |

**Task sum**: Lo = 11.5 days / Hi = 18.5 days

**Integration friction**: +3–5 days (probe performance tuning, alert
dedup edge cases, status page real-time accuracy, maintenance ↔ alert
interaction testing)

| | Work days | Calendar weeks |
|---|---|---|
| **Optimistic** | 14.5 days | ~3 weeks |
| **Mid-range** | 20 days | ~4.5 weeks |
| **Pessimistic** | 23.5 days | ~5 weeks |

### Risk hotspots

1. **PC.1 Probe Engine** 🔴 — Must handle 500+ domains × 5-min interval
   reliably. Network timeouts, DNS failures, and TLS errors must not crash
   the worker. Retry logic must not cascade into overload.
   Mitigation: start with 50 domains, scale up gradually. Use semaphore for
   concurrent outbound connections.

2. **PC.2 Alert Dedup + Batch** 🔴 — The 30-second batch buffer + dedup
   interaction is subtle. Must not: miss alerts, duplicate alerts, or delay
   P1 alerts. Mitigation: P1 bypasses batching entirely (immediate dispatch).
   Extensive integration tests with simulated state changes.

### Recommended work order

```
Week 1:  PC.6 (notification hub — independent, enables everything else) +
         PC.1 start (probe engine L1)
Week 2:  PC.1 finish (L2 + L3 + state detection + TimescaleDB)
Week 3:  PC.2 (alert engine) + PC.4 (maintenance, parallel)
Week 4:  PC.3 (status page) + PC.5 (uptime dashboard)
Week 5:  Integration testing + polish + edge case fixes
```

---

## Scope Creep Warnings

| Temptation | Truth |
|---|---|
| "PC.1 should probe from multiple regions" | Single-location probing is fine. Multi-region is Phase D. |
| "PC.2 should have ML-based anomaly detection" | State-change detection (UP/DOWN) is sufficient. ML is overkill. |
| "PC.3 should support custom HTML templates for status pages" | Fixed theme + custom CSS is enough. Custom HTML is a rabbit hole. |
| "PC.3 should have email/RSS subscriptions" | Out of scope. Add later on top of incidents. |
| "PC.4 should auto-create maintenance from release schedules" | Manual creation only. Auto-link is future. |
| "PC.5 should have real-time WebSocket updates" | Polling (10s) is fine per Phase 2 decision. |
| "PC.6 should support 90+ notification providers like Uptime Kuma" | 4 channels (Telegram, Slack, Webhook, Email) is plenty. Interface makes adding more trivial. |
| "PC.6 should have on-call rotation" | Use external PagerDuty for that. We dispatch TO it, not replicate it. |

---

## References

- `docs/ARCHITECTURE_ROADMAP.md` §6 — Phase C overview
- `docs/analysis/UPTIME_KUMA_ANALYSIS.md` — Monitor model, heartbeat, status page, maintenance, uptime calc
- `ARCHITECTURE.md` §2.7 "Probe Subsystem", §2.8 "Alert & Notification"
- `CLAUDE.md` Critical Rule #8 (alert dedup), §"Task Queue Patterns"
- `docs/FRONTEND_GUIDE.md` — Vue 3 conventions
