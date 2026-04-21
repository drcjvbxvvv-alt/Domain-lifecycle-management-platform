# Architecture Roadmap — Domain Lifecycle & Deployment Platform

> **Created**: 2026-04-21
> **Status**: DRAFT — Pending review
> **Scope**: Full platform evolution from current state (Phase 1-2 complete)
> through enterprise-grade domain management, DNS operations, monitoring,
> and GFW detection.
>
> **Reference**: 7 open-source project analyses in `docs/analysis/`

---

## 1. Vision

Transform the platform from a **release deployment tool** into a **full
domain lifecycle operations platform** that covers:

```
Domain Asset Management → DNS Operations → Deployment → Monitoring → GFW Detection
       (Phase A)            (Phase B)      (Phase 1-2)   (Phase C)    (Phase D)
```

The deployment pipeline (Phase 1-2, already built) becomes one vertical on
top of a broader domain operations platform.

---

## 2. Phase Overview

| Phase | Name | Core Value | Reference Projects |
|-------|------|-----------|-------------------|
| **Phase 1-2** | Release Platform | Build + deploy HTML/nginx to agents | *(completed)* |
| **Phase A** | Domain Asset Layer | Know what you own, where, when it expires | DomainMOD, Nomulus |
| **Phase B** | DNS Operations | Manage DNS records with plan/apply safety | DNSControl, OctoDNS, PowerDNS-Admin |
| **Phase C** | Monitoring & Alerting | Verify deployments, track uptime, alert on issues | Uptime Kuma |
| **Phase D** | GFW Detection | Detect and respond to censorship blocking | OONI Probe |

---

## 3. Dependency Graph

```
                    ┌─────────────────────────────────┐
                    │     Phase 1-2 (COMPLETED)        │
                    │  Release + Deploy + Agent Fleet  │
                    └────────────────┬────────────────┘
                                     │
                    ┌────────────────┴────────────────┐
                    │         Phase A                   │
                    │    Domain Asset Layer             │
                    │                                   │
                    │  A1: Schema + Models              │
                    │  A2: Registrar + DNS Provider CRUD│
                    │  A3: Domain Asset Extension       │
                    │  A4: SSL Cert Tracking            │
                    │  A5: Cost + Fee Schedule          │
                    │  A6: Tags + Bulk Ops              │
                    │  A7: Expiry Dashboard + Alerts    │
                    │  A8: Import Queue                 │
                    └───────┬───────────────┬──────────┘
                            │               │
               ┌────────────┴───┐    ┌──────┴──────────────┐
               │    Phase B     │    │      Phase C         │
               │ DNS Operations │    │  Monitoring & Alert  │
               │                │    │                      │
               │ B1: DNS Record │    │ C1: Probe L1/L2/L3  │
               │     Model      │    │ C2: Alert Engine     │
               │ B2: Provider   │    │ C3: Status Page      │
               │     Sync Engine│    │ C4: Maintenance Win  │
               │ B3: Plan/Apply │    │ C5: Uptime Dashboard │
               │     Workflow   │    │ C6: Notification Hub │
               │ B4: Safety     │    │                      │
               │     Thresholds │    │                      │
               │ B5: DNS UI     │    │                      │
               │ B6: Zone RBAC  │    │                      │
               └───────┬────────┘    └──────────┬──────────┘
                       │                        │
                       └────────────┬───────────┘
                                    │
                       ┌────────────┴────────────┐
                       │        Phase D           │
                       │    GFW Detection         │
                       │                          │
                       │ D1: Probe Node Binary    │
                       │ D2: Multi-Layer Detection│
                       │ D3: Control Comparison   │
                       │ D4: Blocking Verdicts    │
                       │ D5: Auto-Failover        │
                       └──────────────────────────┘
```

### Critical Dependencies

| Downstream | Depends On | Why |
|---|---|---|
| Phase B | Phase A (A2, A3) | DNS operations need `dns_providers` table and domain-provider binding |
| Phase C | Phase A (A3, A4) | Monitoring needs domain inventory + SSL cert expiry data |
| Phase C | Phase 1-2 (P2.1) | L2 probe verifies release deployments |
| Phase D | Phase C (C1, C2) | GFW detection extends probe infrastructure + alert engine |
| Phase D | Phase B (B2) | Failover needs DNS provider sync to switch records |

### Parallelization

- **Phase B and Phase C can run in parallel** after Phase A completes
- Phase B tasks B1-B4 (backend) can parallel with C1-C2 (probe backend)
- Phase D must wait for both B and C

---

## 4. Phase A — Domain Asset Layer

> **Goal**: The platform knows every domain you own, where it's registered,
> when it expires, what DNS provider hosts it, and how much it costs.
>
> **Reference**: DomainMOD (data model), Nomulus (lifecycle + status flags)

### A1: Schema + Models + Store Layer

**Scope**:
- New tables: `registrars`, `registrar_accounts`, `dns_providers`,
  `ssl_certificates`, `domain_costs`, `domain_fee_schedules`,
  `tags`, `domain_tags`, `domain_import_jobs`
- Extend `domains` table: 20+ new columns (asset fields, transfer tracking,
  expiry status, status flags)
- Go structs + sqlx store implementations
- Migration (in-place edit, pre-launch window)

**Deliverables**: Migration SQL, Go models, repository interfaces + postgres impl

### A2: Registrar + DNS Provider CRUD

**Scope**:
- `internal/registrar/` package (service + model + repository)
- `internal/dnsprovider/` package (service + model + repository)
- API: CRUD for registrars, registrar_accounts, dns_providers
- Frontend: Registrar management page, DNS provider management page
- Wire `pkg/provider/dns/` registry to `dns_providers` table

**Deliverables**: Full CRUD API + UI for registrars and DNS providers

### A3: Domain Asset Extension

**Scope**:
- Extend domain creation/edit forms: registrar, DNS provider, expiry, cost
- Domain list: new columns (registrar, provider, expiry, cost, tags)
- Domain detail: full asset information panel
- `domains.tld` auto-extraction on create/update
- `domains.annual_cost` auto-calculation from fee schedule

**Deliverables**: Extended domain CRUD with full asset data

### A4: SSL Certificate Tracking

**Scope**:
- `internal/ssl/` package
- Periodic TLS checker (asynq task: `ssl:check_expiry`)
  - Connect to domain:443, extract cert expiry
  - Update `ssl_certificates` table
  - Alert on expiring certs (30d, 7d thresholds)
- API: cert CRUD + expiring endpoint
- Frontend: cert status in domain detail + expiry dashboard widget

**Deliverables**: SSL tracking + automated expiry checking

### A5: Cost + Fee Schedule

**Scope**:
- `domain_fee_schedules` CRUD (per registrar × TLD)
- `domain_costs` history (per renewal event)
- Auto-calculate `domains.annual_cost` from schedule (unless `fee_fixed`)
- API: cost summary endpoints (by registrar, TLD, project, period)
- Frontend: cost dashboard, cost history per domain

**Deliverables**: Financial tracking + reporting

### A6: Tags + Bulk Operations

**Scope**:
- Tags CRUD + domain-tag assignment
- Bulk domain operations: assign registrar, assign provider, add tags,
  update expiry dates
- Saved filters (optional, frontend-side Pinia state)
- Domain export (CSV)

**Deliverables**: Tag system + bulk operations UI

### A7: Expiry Dashboard + Notifications

**Scope**:
- Periodic worker: `domain:expiry_check` (daily)
  - Compute `expiry_status` for all domains
  - Alert on 30d/7d/expired thresholds
- Dashboard widgets: expiring domains (30/60/90 bands), expiring certs
- Calendar view (domain + cert expirations)
- Notification: Telegram + Webhook on expiry state changes

**Deliverables**: Expiry monitoring + dashboard + notifications

### A8: Import Queue

**Scope**:
- `domain_import_jobs` table + asynq task: `domain:import`
- Manual bulk import: CSV upload → parse → validate → dedup → insert
- API import: connect to registrar API → pull domain list → sync
- Import job status tracking + error reporting
- Frontend: import wizard (upload CSV or trigger API sync)

**Deliverables**: Bulk domain onboarding pipeline

---

## 5. Phase B — DNS Operations

> **Goal**: Manage DNS records for all domains with a safe plan/apply workflow.
> The platform becomes the source of truth for "what records SHOULD exist".
>
> **Reference**: DNSControl (provider interface), OctoDNS (plan/apply + safety),
> PowerDNS-Admin (UI patterns)

### B1: DNS Record Data Model

**Scope**:
- New table: `dns_records` (desired state stored in DB)
  ```sql
  dns_records (
      id, domain_id, name, type, content, ttl,
      extra JSONB,           -- type-specific (MX priority, SRV weight, etc.)
      managed BOOLEAN,       -- platform-managed or external
      provider_record_id,    -- provider's ID for this record
      created_at, updated_at
  )
  ```
- New table: `dns_sync_history` (audit of plan/apply actions)
- Go model: `pkg/provider/dns.Record` (from DNSControl analysis)
- Domain-level settings: `purge_unmanaged_records`, `sync_config` JSONB

**Deliverables**: Record schema, models, store layer

### B2: Provider Sync Engine

**Scope**:
- Implement `pkg/provider/dns.Provider` interface (from DNSControl analysis):
  - `GetZoneRecords()` — fetch actual state from provider
  - `PlanChanges()` — compute desired vs actual diff → corrections
  - `ApplyCorrections()` — execute changes
- Implement Cloudflare provider (extend existing P2.7 work)
- Provider registry: `dns.GetProvider(name)` returns initialized provider

**Deliverables**: DNS sync engine with Cloudflare implementation

### B3: Plan/Apply Workflow

**Scope**:
- API: `POST /api/v1/domains/:id/dns/plan` → returns proposed changes
- API: `POST /api/v1/domains/:id/dns/apply` → executes with checksum verification
- Store plan hash in Redis (TTL 1 hour, OctoDNS checksum pattern)
- Audit: record all DNS changes in `dns_sync_history`
- asynq task: `dns:sync` for scheduled syncs (detect drift)

**Deliverables**: Plan/apply API with checksum verification

### B4: Safety Thresholds

**Scope** (from OctoDNS analysis):
- Configurable per provider: `update_threshold_pct`, `delete_threshold_pct`
- `MIN_EXISTING_RECORDS = 10` — skip safety on small zones
- Root NS protection — always require explicit confirmation
- `force` parameter for override
- Dry-run by default in API

**Deliverables**: Safety mechanisms preventing accidental mass changes

### B5: DNS Management UI

**Scope** (from PowerDNS-Admin analysis):
- Zone record list view (table: name, type, content, TTL, managed)
- Inline editing with type-specific validation
- **Staged-edit + batch-apply** pattern (edit in memory → submit once)
- Plan preview (shows creates/updates/deletes before execution)
- Diff view for pending changes
- DNS record templates (pre-populate standard records on new domain)

**Deliverables**: DNS record management UI with plan preview

### B6: Zone-Level RBAC

**Scope** (from PowerDNS-Admin analysis):
- `domain_permissions` table (user × domain × permission level)
- Two-path access: project membership OR direct domain permission
- Permission levels: viewer, editor, admin
- DNS write operations require `editor` or above

**Deliverables**: Fine-grained domain-level access control

---

## 6. Phase C — Monitoring & Alerting

> **Goal**: Verify deployments landed, track uptime, alert on failures,
> provide public status pages.
>
> **Reference**: Uptime Kuma (data model, status pages, maintenance windows)

### C1: Probe L1 / L2 / L3

**Scope** (from ARCHITECTURE.md §2.7, enhanced with Uptime Kuma patterns):
- L1: DNS + TCP + HTTP status for all active domains (every 5 min)
- L2: Content verification — check `<meta release-version>` matches (post-deploy)
- L3: Business endpoint health for `core`-tagged domains (every 1 min)
- Probe result → TimescaleDB `probe_results` hypertable
- State-change detection (UP→DOWN, DOWN→UP) → trigger alert
- Keyword matching (Uptime Kuma pattern) for L2 verification

**Deliverables**: 3-tier probe system with TimescaleDB storage

### C2: Alert Engine + Dedup

**Scope** (Critical Rule #8 + Uptime Kuma state-change model):
- Fire alerts on state TRANSITIONS only (not every check)
- Severity levels: P1 (page), P2 (notify), P3 (log), INFO
- Dedup: same (target, type, severity) → 1 alert/hour max
- Batch multi-target alerts into one message
- Alert → Notification dispatch (many-to-many, reusable configs)

**Deliverables**: Alert engine with dedup + notification dispatch

### C3: Public Status Page

**Scope** (from Uptime Kuma status page model):
- `status_pages` table: slug, title, description, theme, password
- Groups: organize monitors into named sections
- Per-domain uptime bars (24h/7d/30d)
- Incident management: manual post (title + Markdown + severity)
- Custom domain support (CNAME)
- Auto-refresh (polling or SSE)

**Deliverables**: Public-facing status pages with incidents

### C4: Maintenance Windows

**Scope** (from Uptime Kuma maintenance model):
- `maintenance_windows` table: schedule (single/recurring/cron)
- Link to targets (domains, host_groups, projects)
- During maintenance:
  - Suppress alert notifications
  - Status page shows "Under Maintenance" (not "Down")
  - Heartbeats recorded as MAINTENANCE status
  - Does NOT count against uptime percentage

**Deliverables**: Scheduled maintenance with alert suppression

### C5: Uptime Dashboard

**Scope**:
- TimescaleDB continuous aggregates (hourly + daily)
- Uptime percentage per domain: 24h, 7d, 30d, 1y
- Response time charts (line graph)
- Uptime calendar heatmap
- Top-N worst performers
- Historical incident timeline

**Deliverables**: Uptime analytics dashboard

### C6: Notification Hub

**Scope** (extend existing `pkg/notify`):
- Notification as reusable objects (many-to-many with monitors/alerts)
- Channels: Telegram (existing), Slack, Webhook (existing), Email, PagerDuty
- Per-channel configuration + test button
- Notification history/log
- Escalation: P1 → page immediately; P2 → batch every 5 min

**Deliverables**: Multi-channel notification system

---

## 7. Phase D — GFW Detection (Parked, Future Vertical)

> **Goal**: Detect when domains are blocked by the Great Firewall and
> automatically trigger DNS failover.
>
> **Reference**: OONI Probe (detection methodology)
>
> **Status**: Architecture designed, implementation deferred per ADR-0003 D11.
> Will be activated when Phase B + C are stable.

### D1: Probe Node Binary

**Scope**:
- New binary: `cmd/probe` (lightweight Go binary for distributed checking)
- Deployed to CN vantage points (Beijing, Shanghai, Guangzhou)
- Deployed to control vantage points (HK, JP, US)
- Communicates with control plane via HTTPS (similar to agent protocol)
- Runs 4-layer checks on assigned domains

**Deliverables**: Distributed probe node binary

### D2: Multi-Layer Detection

**Scope** (from OONI analysis):
- DNS check: query local resolver, compare with control
- TCP check: SYN → measure RST injection
- TLS check: ClientHello with target SNI, detect reset
- HTTP check: full GET, compare body/status with control
- Per-layer result storage: `gfw_measurements` table

**Deliverables**: 4-layer censorship detection engine

### D3: Control vs Measurement Comparison

**Scope** (from OONI decision tree):
- DNS consistency: ASN-based comparison (not IP equality)
- Blocking verdict logic:
  1. HTTPS success → accessible
  2. DNS inconsistent + probe NXDOMAIN → `blocking = "dns"`
  3. TCP all failed → `blocking = "tcp_ip"`
  4. TLS reset on SNI → `blocking = "tls_sni"`
  5. HTTP content differs → `blocking = "http-diff"`
- Confidence scoring: require N consecutive detections before confirming
- False positive handling: CDN/geo variation allowlists

**Deliverables**: Blocking analysis engine with confidence scoring

### D4: Blocking Verdict + Alert

**Scope**:
- Blocking detected → P1 alert to operators
- Dashboard: blocked domains, blocking type, timeline
- Integration with Phase C alert engine (dedup, batching)
- Historical blocking data for trend analysis

**Deliverables**: GFW blocking alerting + dashboard

### D5: Auto-Failover (DNS Switch)

**Scope** (requires Phase B DNS operations):
- On confirmed blocking:
  - Auto-switch DNS to backup IP/CDN (via Phase B sync engine)
  - Or switch to standby domain (from domain pool)
- Failover policy configuration per domain/project
- Automatic recovery when block clears
- Full audit trail of failover actions

**Deliverables**: Automated DNS failover on GFW blocking

---

## 8. Cross-Phase Infrastructure

These capabilities span multiple phases and are built incrementally:

### 8.1 Provider Plugin Architecture

```
Phase A: pkg/provider/registrar/ (domain info sync)
Phase B: pkg/provider/dns/ (DNS record management) — extends existing
Phase D: pkg/provider/cdn/ (CDN failover, future)
```

All follow DNSControl's pattern:
- Interface defined where consumed
- Registry pattern for lookup
- Correction-based plan/apply

### 8.2 Notification System

```
Phase A:  Basic (expiry alerts → Telegram/Webhook)
Phase C:  Full (probe alerts → multi-channel with dedup)
Phase D:  Extended (GFW alerts → escalation + auto-action)
```

### 8.3 Audit Trail

```
Phase 1-2: audit_logs + per-entity history tables (existing)
Phase A:   Domain asset changes audited
Phase B:   DNS record changes with before/after diff (PowerDNS-Admin pattern)
Phase C:   Alert acknowledgement + incident lifecycle
Phase D:   Failover actions audited
```

### 8.4 Dashboard Evolution

```
Phase 1-2: Release progress, agent fleet
Phase A:   Expiry calendar, cost summary, domain inventory
Phase B:   DNS record overview, sync status, drift detection
Phase C:   Uptime charts, response time, status page admin
Phase D:   Blocking map, failover history, CN reachability
```

---

## 9. Data Model Evolution

### New Tables Per Phase

| Phase | New Tables | Extended Tables |
|-------|-----------|----------------|
| **A** | `registrars`, `registrar_accounts`, `dns_providers`, `ssl_certificates`, `domain_costs`, `domain_fee_schedules`, `tags`, `domain_tags`, `domain_import_jobs` | `domains` (+20 columns) |
| **B** | `dns_records`, `dns_sync_history`, `dns_record_templates`, `domain_permissions` | `dns_providers` (+sync_config) |
| **C** | `maintenance_windows`, `maintenance_window_targets`, `status_pages`, `status_page_groups`, `status_page_monitors`, `status_page_incidents`, `notification_configs` | `probe_results` (continuous aggregates) |
| **D** | `gfw_measurements`, `gfw_probe_nodes`, `failover_policies`, `failover_history` | `domains` (+blocking_status) |

### Total Table Count Projection

```
Phase 1-2 (current):  ~25 tables
Phase A:              +9 tables = ~34
Phase B:              +4 tables = ~38
Phase C:              +7 tables = ~45
Phase D:              +4 tables = ~49
```

---

## 10. Go Package Structure (Full Platform)

```
internal/
├── project/          # Phase 1 — Project CRUD
├── lifecycle/        # Phase 1 — Domain lifecycle state machine
├── template/         # Phase 1 — Template + versions
├── artifact/         # Phase 1 — Artifact build pipeline
├── release/          # Phase 1-2 — Release + shards + rollback + dry-run
├── agent/            # Phase 1-2 — Agent management (control-plane side)
├── tasks/            # Phase 1 — asynq task type constants
├── auth/             # Phase 1 — JWT + RBAC
├── audit/            # Phase 1 — Audit log writes
│
├── domain/           # Phase A — Domain asset CRUD + enrichment
├── registrar/        # Phase A — Registrar + account management
├── dnsprovider/      # Phase A — DNS provider account management
├── ssl/              # Phase A — SSL cert tracking + expiry check
├── cost/             # Phase A — Fee schedules + cost tracking
├── tag/              # Phase A — Tag system
├── importer/         # Phase A — Bulk domain import
│
├── dnsrecord/        # Phase B — DNS record desired state + CRUD
├── dnssync/          # Phase B — Sync engine (plan/apply orchestration)
│
├── probe/            # Phase C — Probe orchestration (L1/L2/L3)
├── alert/            # Phase C — Alert engine + dedup + dispatch
├── statuspage/       # Phase C — Public status page management
├── maintenance/      # Phase C — Maintenance windows
│
├── gfw/              # Phase D — GFW detection + analysis
└── failover/         # Phase D — Auto-failover orchestration

pkg/
├── provider/
│   ├── dns/          # DNS provider interface + implementations
│   ├── registrar/    # Registrar provider interface + implementations
│   └── cdn/          # Phase D — CDN provider interface (future)
├── storage/          # MinIO/S3 (existing)
├── notify/           # Notification channels (Telegram, Slack, Webhook, Email)
├── agentprotocol/    # Agent wire protocol (existing)
└── template/         # Template rendering helpers (existing)

cmd/
├── server/           # Control plane API (existing)
├── worker/           # asynq task worker (existing)
├── agent/            # Pull agent on nginx hosts (existing)
├── migrate/          # DB migration tool (existing)
├── probe/            # Phase D — Distributed probe node binary
└── scanner/          # PARKED → becomes cmd/probe in Phase D
```

---

## 11. API Route Evolution

### Phase A Routes (New)

```
/api/v1/registrars[/:id]                    # Registrar CRUD
/api/v1/registrars/:id/accounts[/:id]       # Account CRUD
/api/v1/dns-providers[/:id]                 # DNS Provider CRUD
/api/v1/domains/:id/ssl-certs[/:id]         # SSL cert tracking
/api/v1/domains/:id/costs[/:id]             # Cost history
/api/v1/domains/:id/transfer                # Transfer tracking
/api/v1/domains/import                      # Bulk import
/api/v1/domains/expiring                    # Expiring domains
/api/v1/domains/stats                       # Aggregate statistics
/api/v1/tags[/:id]                          # Tag CRUD
/api/v1/fee-schedules[/:id]                 # Fee schedule CRUD
/api/v1/costs/summary                       # Cost reporting
```

### Phase B Routes (New)

```
/api/v1/domains/:id/dns/records[/:id]       # DNS record CRUD (desired state)
/api/v1/domains/:id/dns/plan                # Compute plan (dry-run)
/api/v1/domains/:id/dns/apply               # Execute plan (with checksum)
/api/v1/domains/:id/dns/sync-status         # Current sync status
/api/v1/dns-templates[/:id]                 # DNS record templates
```

### Phase C Routes (New)

```
/api/v1/probes/results                      # Probe result query
/api/v1/probes/uptime/:domain_id            # Uptime percentage
/api/v1/alerts[/:id]                        # Alert management
/api/v1/alerts/:id/acknowledge              # Acknowledge alert
/api/v1/status-pages[/:id]                  # Status page CRUD
/api/v1/status-pages/:id/incidents[/:id]    # Incident management
/api/v1/maintenance[/:id]                   # Maintenance window CRUD
/api/v1/notifications[/:id]                 # Notification config CRUD
```

### Phase D Routes (New)

```
/api/v1/gfw/measurements                    # GFW measurement query
/api/v1/gfw/blocked-domains                 # Currently blocked domains
/api/v1/gfw/failover-policies[/:id]         # Failover policy CRUD
/api/v1/gfw/failover-history                # Failover action history
/probe/v1/...                               # Probe node protocol (like agent/v1)
```

---

## 12. Frontend Page Evolution

### Phase A Pages (New)

| Page | Path | Purpose |
|---|---|---|
| Registrar List | `/registrars` | Manage registrar vendors |
| Registrar Detail | `/registrars/:id` | Accounts + domains at this registrar |
| DNS Provider List | `/dns-providers` | Manage DNS hosting providers |
| Domain Asset Panel | `/domains/:id` (extended) | Full asset info tab |
| Expiry Dashboard | `/dashboard/expiry` | 30/60/90 day bands + calendar |
| Cost Dashboard | `/dashboard/costs` | By registrar, TLD, project |
| Import Wizard | `/domains/import` | CSV upload / API sync |
| Tag Manager | `/settings/tags` | Tag CRUD with colors |

### Phase B Pages (New)

| Page | Path | Purpose |
|---|---|---|
| DNS Records | `/domains/:id/dns` | Record list + inline edit |
| DNS Plan Preview | `/domains/:id/dns/plan` | Changes to apply (diff view) |
| DNS Templates | `/settings/dns-templates` | Reusable record sets |
| DNS Sync Status | `/dns/sync-status` | All domains, last sync, drift |

### Phase C Pages (New)

| Page | Path | Purpose |
|---|---|---|
| Uptime Dashboard | `/dashboard/uptime` | Charts, worst performers |
| Alert List | `/alerts` | Active alerts, history |
| Status Page Admin | `/status-pages` | Manage public pages |
| Status Page Public | `/:slug` (public route) | Customer-facing |
| Maintenance Windows | `/maintenance` | Schedule maintenance |
| Notification Config | `/settings/notifications` | Channel management |

### Phase D Pages (New)

| Page | Path | Purpose |
|---|---|---|
| GFW Dashboard | `/dashboard/gfw` | Blocked domains map/list |
| Blocking Timeline | `/gfw/timeline` | Historical blocking data |
| Failover Policies | `/gfw/failover` | Auto-switch configuration |
| Probe Nodes | `/gfw/nodes` | CN/control node status |

---

## 13. Effort Estimates (Rough)

> Same caveat as Phase 1-2 estimates: planning tools, not commitments.

| Phase | Tasks | Estimated Range | Key Risk |
|-------|-------|----------------|----------|
| **A** | A1–A8 | 3–5 weeks | Scope creep in asset fields; import queue complexity |
| **B** | B1–B6 | 4–7 weeks | Provider API differences; safety threshold tuning |
| **C** | C1–C6 | 5–8 weeks | Distributed probe reliability; TimescaleDB tuning |
| **D** | D1–D5 | 6–10 weeks | CN node deployment; false positive handling; legal |

**Total**: 18–30 weeks (4.5–7.5 months) for full platform.

### Recommended Sequencing

```
Month 1-2:    Phase A (foundation, blocks everything else)
Month 2-3:    Phase B (DNS ops) + Phase C C1-C2 (probe backend) in parallel
Month 3-4:    Phase B finish + Phase C C3-C6 (status page, maintenance, dashboard)
Month 5-6:    Phase D (when B+C are stable, and CN infrastructure is ready)
```

---

## 14. Quality Gates Between Phases

### Phase A → Phase B Gate

- [ ] All `registrars`, `registrar_accounts`, `dns_providers` CRUD working
- [ ] Domains have `dns_provider_id` properly linked
- [ ] `pkg/provider/dns/` interface finalized and Cloudflare impl working
- [ ] Domain asset data visible in UI (registrar, provider, expiry)

### Phase A → Phase C Gate

- [ ] Domains have complete asset data (FQDN, project, lifecycle_state)
- [ ] `ssl_certificates` tracking operational
- [ ] Expiry alert worker running (`domain:expiry_check`)
- [ ] `pkg/notify/` channels working (Telegram + Webhook minimum)

### Phase B + C → Phase D Gate

- [ ] DNS plan/apply working end-to-end (can switch A records programmatically)
- [ ] Probe L1 running for all active domains
- [ ] Alert engine with dedup operational
- [ ] Status page feature live
- [ ] CN probe node infrastructure provisioned and reachable

---

## 15. Key Architecture Principles (Carried from Analyses)

| # | Principle | Source |
|---|---|---|
| 1 | Registrar and DNS Provider are separate roles | DNSControl |
| 2 | Plan before Apply; dry-run by default | OctoDNS |
| 3 | Safety thresholds prevent accidental mass changes | OctoDNS (30% rule) |
| 4 | Status flags are orthogonal to lifecycle state | Nomulus |
| 5 | State-change alerting (fire on transitions) | Uptime Kuma + Critical Rule #8 |
| 6 | Probe vs Control comparison for blocking detection | OONI Probe |
| 7 | Two-path access control (project + domain-level) | PowerDNS-Admin |
| 8 | Fee schedule is per (Registrar × TLD), not per domain | DomainMOD |
| 9 | Don't duplicate provider state locally; proxy | PowerDNS-Admin |
| 10 | Correction = description + executable function | DNSControl |
| 11 | Checksum verification prevents stale plan execution | OctoDNS |
| 12 | Maintenance windows suppress alerts, not monitoring | Uptime Kuma |
| 13 | 3-level time aggregation for uptime (minute/hour/day) | Uptime Kuma → TimescaleDB |
| 14 | ASN-based DNS comparison, not IP equality | OONI Probe |

---

## References

- `docs/analysis/DNSCONTROL_ANALYSIS.md`
- `docs/analysis/DOMAINMOD_ANALYSIS.md`
- `docs/analysis/NOMULUS_ANALYSIS.md`
- `docs/analysis/OCTODNS_ANALYSIS.md`
- `docs/analysis/POWERDNS_ADMIN_ANALYSIS.md`
- `docs/analysis/OONI_PROBE_ANALYSIS.md`
- `docs/analysis/UPTIME_KUMA_ANALYSIS.md`
- `docs/DOMAIN_ASSET_LAYER_DESIGN.md` (Phase A detailed design)
- `docs/ARCHITECTURE.md` (current system architecture)
- `docs/PHASE2_TASKLIST.md` (Phase 1-2 completed work)
- `CLAUDE.md` (tech stack, coding standards, critical rules)
