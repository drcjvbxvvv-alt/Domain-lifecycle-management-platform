# ARCHITECTURE.md — System Architecture Reference

> This document is a detailed reference for the Domain Lifecycle Management Platform architecture.
> Claude Code should read this when working on cross-cutting concerns, provider integrations, or deployment.

---

## 1. System Overview

The platform manages 12,000+ domains across 10 projects. It automates:
- Domain onboarding (DNS + CDN + nginx conf + deployment + verification)
- Continuous reachability monitoring from CN probe nodes (Phase 1: 3 nodes, one per ISP; Phase 2: 6 nodes, two per ISP with north/south coverage)
- Automated failover when GFW blocks a domain — SLA: **detection < 2 min, failover < 5 min**
- Batch releases with canary/rollback capabilities
- Standby domain pool management with pre-warming

### Core Principle: Prefix Determines Everything

```
Given: main domain = example.com, prefix = "ws"
System auto-derives:
  FQDN:          ws.example.com
  DNS Provider:   Cloudflare (from prefix rule)
  CDN Provider:   Cloudflare (from prefix rule)
  nginx template: ws.conf.tmpl (from prefix rule)
  HTML template:  none (from prefix rule)
```

Prefix rules have two levels: system-wide defaults + project-level overrides.
Project-level ALWAYS wins.

---

## 2. Subsystem Responsibilities

### 2.1 Domain Registry & Configuration (internal/domain, internal/project)

- CRUD for projects, main domains, subdomains, prefix rules
- Automatic subdomain generation from prefix rules when a domain is registered
- Domain state machine enforcement (see CLAUDE.md)
- Audit trail for all state transitions

### 2.2 DNS/CDN Automation (pkg/provider)

- Unified interface abstracting 5 CDN + 4 DNS vendors
- Provider registry with runtime lookup by name
- All provider operations are async (dispatched as asynq tasks)
- Retry with exponential backoff per provider
- Provider-specific quirks handled INSIDE the implementation, NEVER leaked to business logic

**Rule — DNS record TTL:** All CNAME / A records created by the platform default to
**TTL = 60s**. This is a hard requirement for the auto-switch SLA in §2.5; providers that
do not support 60s TTL (e.g. some free-tier plans) must be flagged in their adapter and
rejected at provider registration time.

**Provider implementation priority:**
- P0: Cloudflare (DNS+CDN), Alibaba Cloud (DNS+CDN)
- P1: Tencent Cloud (DNS+CDN), GoDaddy (DNS)
- P2: Huawei Cloud (DNS+CDN), Self-hosted CDN

### 2.3 Release Subsystem (internal/release)

Hierarchy:
```
Release
  └── Shard (200-500 domains per shard)
       └── DomainTask (one per domain: render → deploy → verify)
            └── ServerTask (one per target machine: svn up → nginx reload)
```

**Shard partitioning rules:**
- A Release is always scoped to **one project** (never cross-project). Large cross-project
  operations must be dispatched as multiple independent Releases.
- Within a project, domains are partitioned by `hash(main_domain_id) % shard_count` so that
  retries of the same domain always land in the same shard.
- Shard size: normal shards 200–500 domains. The **first (canary) shard** is always the
  smaller of `30 domains` or `2% of the release`, with a hard minimum of 10. Rationale:
  a blast radius of ≤ 30 domains is small enough that a template regression caught at
  canary costs at most one manual rollback cycle, while still being statistically
  meaningful for the 95% probe-verification threshold.

**Canary strategy:**
- Deploy canary shard → wait for probe verification → success rate ≥ 95% → continue
- Success rate < 95% → auto-pause Release, alert operators
- Any shard can be paused/resumed/rolled back independently

**nginx reload aggregation:**
- Same server, multiple conf changes → buffer 30 seconds OR 50 domains, then single reload
- Emergency (P1 alert failover) → skip buffer, immediate reload
- ALWAYS run `nginx -t` before reload.

**Reload failure handling (explicit):**
- `nginx -t` fails on a server → roll back *only that server's* batch (restore previous
  conf files from snapshot, no reload issued), mark every DomainTask in that server's batch
  as `failed` with reason `nginx_test_failed`.
- The enclosing Release shard is **not** globally aborted; other servers in the same shard
  continue independently. However, if > 20% of servers in a shard fail `nginx -t`, the
  whole shard is paused and escalated to P1 alert.
- DomainTasks marked `failed` do **not** auto-retry via asynq. They return to `deploying`
  only via an explicit operator re-queue, because a template error will just fail again.
- Every reload batch MUST snapshot the full previous conf to DB before writing new conf,
  per CLAUDE.md Critical Rule #6.

### 2.4 Probe Monitoring (internal/probe, cmd/scanner)

**Three-tier probing:**

| Tier | Target | Checks | Frequency | Concurrency |
|------|--------|--------|-----------|-------------|
| L1 | All 12K main domains | DNS + TCP :443 | Every 60s | 500 goroutines |
| L2 | L1-passed domains, sample 1-2 subdomains | HTTP 200 + latency | Every 5min | 200 goroutines |
| L3 | Manually tagged core domains | HTTP + keyword + TLS handshake + cert expiry | Every 30s | 50 goroutines |

**Detection & SLA math (L1):**
- Cycle = 60s. Two trigger paths:
  - **Fast path**: a single cycle in which *all* active probe nodes report the same
    non-ok status (dns_poison / tcp_block / http_hijack) → immediate P1.
  - **Confirmation path**: majority of active nodes (Phase 1: ≥ 2 of 3; Phase 2: ≥ 4 of 6)
    report non-ok for **2 consecutive cycles** → P1.
- Worst-case detection latency via fast path ≈ 60s + ~5s aggregation ≈ **65s**.
- Worst-case via confirmation path ≈ 60s + 60s + ~5s ≈ **125s** (edge of SLA, acceptable
  because this path exists only to suppress transient single-node blips).
- Single-node, single-cycle failures become a P3 log, never a P1.
- `alert:dedup` (see Redis keys) ensures the same status does not re-alert within 1h.
- **Capacity note**: 12K domains × 3 probe nodes ÷ 60s cycle ≈ 200 checks/s/node. On the
  1C/1G Phase-1 probe boxes this is achievable only with Go scanner concurrency ≥ 500
  and DNS/TCP timeouts ≤ 3s. If live load testing shows CPU > 80% sustained, fall back
  to L1 = 90s and negotiate the SLA to < 3 min with stakeholders before Phase 1 cutover.

**Block detection logic:**
```
DNS poisoning:   Resolved IPs match known GFW poison IPs (127.0.0.1, 243.185.187.39, etc.)
TCP block:       connect() to :443 times out after 3s
SNI block:       TCP connects but TLS handshake fails
HTTP hijack:     Response contains block keywords OR unexpected redirect
Content tamper:  Response body checksum mismatch
```

**Data flow:**
```
CN Probe Nodes (Go Scanner)
    │
    │ HTTPS POST /api/v1/probe/push (mTLS authenticated)
    ▼
Probe Receiver (境外)
    │
    ├──→ TimescaleDB (raw results, 90-day retention)
    ├──→ Redis (current state per domain, dedup)
    └──→ Alert Engine (on state change only)
```

### 2.5 Alert & Auto-Disposition (internal/alert, internal/switcher)

**Alert severity:**

| Level | Trigger | Auto-action |
|-------|---------|-------------|
| P0 | Standby pool exhausted / entire project unreachable | Pause all releases |
| P1 | Main domain blocked (DNS poison / TCP block / HTTP hijack) | Trigger auto-switch |
| P2 | Partial subdomain anomaly / pool < 5 remaining | Alert for manual intervention |
| P3 | High latency / non-critical anomaly | Log only |
| INFO | Domain recovered / switch completed | Notification |

**Auto-switch flow (P1 trigger):**
```
1. Send alert (Telegram + Webhook)
2. Pause all pending releases for this domain's project
3. Select highest-priority standby domain from pool
4. DNS migration: delete old CNAMEs, create new CNAMEs
5. CDN migration: clone config from old domain to new domain
6. Re-render nginx conf with new main domain (prefixes unchanged)
7. SVN commit + Agent deploy
8. Wait 30-60s for DNS TTL + CDN propagation (this assumes all managed CNAME TTLs are 60s — see §2.2 rule)
9. Probe verification from ≥ majority of active CN probe nodes (Phase 1: ≥ 2 of 3; Phase 2: ≥ 4 of 6)
10. Update DB: old domain → blocked, new domain → active
```

Each step has rollback logic. If step N fails, steps 1..N-1 are reversed.

### 2.6 Standby Pool (internal/pool)

**Lifecycle:** `pending → warming → ready → promoted → blocked → retired`

> Naming is deliberately distinct from the main-domain state machine in CLAUDE.md so that
> `pool.status = ready` is never confused with `main_domain.status = active`, and
> `pool.status = promoted` clearly marks the moment a pooled domain was swapped in to
> replace a blocked main domain.

**Pre-warming (transition `pending → warming → ready`):**
1. `pending → warming` when the warmup worker picks up the row
2. Create DNS CNAME records for all prefixes (TTL = 60)
3. Create CDN configurations for all prefixes
4. Wait for DNS + CDN propagation
5. Verify reachability from ≥ majority of active CN probe nodes (Phase 1: ≥ 2 of 3; Phase 2: ≥ 4 of 6)
6. ALL required checks pass → status = `ready`. ANY fail → status = `pending`, alert, retry with backoff.

**Pool thresholds (counted over `status = ready` only):**
- Normal projects: alert at < 2 remaining
- Core projects: alert at < 5 remaining
- Any project's `ready` pool = 0: P0 alert (critical)

---

## 3. Data Architecture

### PostgreSQL Tables

All tables follow these conventions:
- `id BIGSERIAL PRIMARY KEY` — internal use only, never exposed in API
- `uuid UUID NOT NULL DEFAULT gen_random_uuid()` — external identifier
- `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
- `deleted_at TIMESTAMPTZ` — soft delete

Key relationships:
```
projects 1──N main_domains
main_domains 1──N subdomains
main_domains 1──N domain_state_history
projects 1──N main_domain_pool
projects 1──N releases
releases 1──N release_shards
release_shards 1──N domain_tasks
main_domains 1──N switch_history
```

### TimescaleDB (probe_results hypertable)

```sql
CREATE TABLE probe_results (
    probe_node      VARCHAR(32)  NOT NULL,
    isp             VARCHAR(16)  NOT NULL,
    domain          VARCHAR(253) NOT NULL,
    tier            SMALLINT     NOT NULL,      -- 1=L1, 2=L2, 3=L3
    status          VARCHAR(16)  NOT NULL,      -- ok / dns_poison / tcp_block / sni_block / http_hijack / content_tamper / timeout
    block_reason    VARCHAR(64),                -- free-form detail, NULL when ok
    dns_ok          BOOLEAN      NOT NULL,
    dns_ips         TEXT[],
    tcp_latency_ms  FLOAT,
    tls_handshake_ok BOOLEAN,                   -- L2+ only
    tls_sni_ok      BOOLEAN,                    -- L2+ only
    tls_cert_expiry TIMESTAMPTZ,                -- L3 only
    http_code       SMALLINT,
    http_hijacked   BOOLEAN,
    content_hash    BYTEA,                      -- L3 only, for content_tamper detection
    checked_at      TIMESTAMPTZ  NOT NULL
);

SELECT create_hypertable('probe_results', 'checked_at');

-- Retention policy: 90 days raw, aggregated summaries permanent
SELECT add_retention_policy('probe_results', INTERVAL '90 days');

-- Continuous aggregate for dashboard
CREATE MATERIALIZED VIEW probe_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', checked_at) AS bucket,
    domain,
    probe_node,
    COUNT(*) AS total_checks,
    COUNT(*) FILTER (WHERE status = 'ok') AS ok_count,
    AVG(tcp_latency_ms) AS avg_latency
FROM probe_results
GROUP BY bucket, domain, probe_node;
```

### Redis Key Design

```
# Domain current status (for alert dedup)
domain:status:{probe_node}:{domain} → "ok" | "dns_poison" | "tcp_block" | ...
TTL: 3600s

# Alert dedup
alert:dedup:{probe_node}:{domain}:{status} → "1"
TTL: 3600s (same status won't re-alert within 1 hour)

# nginx reload buffer
reload:buffer:{server_id} → SET of domain_task_ids
TTL: 60s (auto-flush if not manually triggered)

# Auto-switch distributed lock — prevents two workers from switching the same domain
switch:lock:{main_domain_id} → worker_id
TTL: 600s (hard ceiling for a full switch cycle)

# Release pause flag — set by P0/P1 auto-pause, read by release scheduler
release:pause:{project_id} → reason_code
TTL: none (manually cleared by operator)

# Probe cross-cycle consecutive-failure counter (see §2.4 confirmation path)
probe:fail_streak:{probe_node}:{domain} → integer
TTL: 300s (resets if no updates)

# asynq internal keys — reserved namespace, do not reuse
asynq:* → managed by hibiken/asynq, do not write manually
```

---

## 4. Communication Security

### Probe ↔ Controller: mTLS

```
CN Probe Node                    Probe Controller
┌──────────┐    TLS 1.3 mTLS    ┌──────────────┐
│ Client   │ ──────────────────→ │ Server       │
│ Cert     │                     │ Cert         │
│ (unique) │                     │ (controller) │
└──────────┘                     └──────────────┘
     │                                  │
     └──── Both signed by ─────────────┘
           Internal CA
```

- Each probe gets a unique client certificate signed by internal CA
- Controller validates client cert against CA root — rejects unknown probes
- Certificate rotation: every 90 days, 7-day overlap grace period
- Failed auth: reject immediately, do NOT return error details

### Management Console: JWT + HTTPS

- Caddy handles HTTPS (auto Let's Encrypt)
- JWT tokens: 24h expiry, HS256 signed
- Password storage: bcrypt, cost factor 12
- Rate limiting: login endpoint 10 req/min
- **Login identifier = `username`, NOT email.** This is an internal operator console; user
  accounts are provisioned manually and do not carry email. Any future password-reset or
  notification feature MUST use an out-of-band channel (Telegram), not email.

---

## 5. Deployment Topology

### Phase 1 (5 machines)

```
┌─── Taiwan ─────────────────────────────────────┐
│                                                 │
│  Main Node (8C/32G SSD)                        │
│  ┌─────────────────────────────────────┐       │
│  │ Caddy (reverse proxy + static)      │       │
│  │ domain-platform (Gin API :8080)     │       │
│  │ domain-worker (asynq worker)        │       │
│  │ PostgreSQL 16 + TimescaleDB (:5432) │       │
│  │ Redis 7 (:6379)                     │       │
│  └─────────────────────────────────────┘       │
│                                                 │
│  Probe Controller (2C/4G)                      │
│  ┌─────────────────────────────────────┐       │
│  │ domain-platform probe-receiver      │       │
│  │ Alert Engine + Telegram Bot         │       │
│  │ Auto-Switch Engine                  │       │
│  └─────────────────────────────────────┘       │
│                                                 │
└─────────────────────────────────────────────────┘

┌─── Mainland China ─────────────────────────────┐
│  cn-probe-ct (Telecom, 1C/1G)  → Go Scanner   │
│  cn-probe-cu (Unicom,  1C/1G)  → Go Scanner   │
│  cn-probe-cm (Mobile,  1C/1G)  → Go Scanner   │
└─────────────────────────────────────────────────┘
```

### Phase 2 Expansion

- Separate Deploy Worker (8C/16G) for batch release CPU-intensive work
- CN probes: 3 → 6 (2 per ISP, north + south coverage)
- Consider ClickHouse migration if TimescaleDB queries degrade

### Backup & Disaster Recovery (applies from Phase 1)

- **PostgreSQL**: daily `pg_dump` at 04:00 local, retained 14 days on the main node; WAL
  archiving to an off-site object store with 7-day retention; weekly full base backup
  shipped off-site, retained 8 weeks.
- **TimescaleDB hypertable** (`probe_results`): not included in nightly dump (90-day
  retention policy already acts as bounded state). Continuous aggregates (`probe_hourly`)
  ARE dumped.
- **Redis**: AOF `everysec` on the main node. Redis holds only ephemeral state (alert
  dedup, reload buffers, locks) — loss is tolerable but causes brief re-alert storms.
- **Config & secrets**: `configs/providers.yaml` and JWT/CA material are versioned in a
  private repo, NOT in Postgres. Recovery requires both the DB backup and the config repo.
- **RPO / RTO targets (Phase 1)**: RPO ≤ 24h (daily dump) / ≤ 5min (WAL); RTO ≤ 2h for
  API+worker; probe nodes are stateless and can be re-provisioned from the scanner binary
  and a fresh client cert.

---

## 6. Build & Deploy

### Build Artifacts

```bash
# API + Web server
GOOS=linux GOARCH=amd64 go build -o bin/domain-platform ./cmd/server

# Task worker
GOOS=linux GOARCH=amd64 go build -o bin/domain-worker ./cmd/worker

# Probe scanner (for CN nodes)
GOOS=linux GOARCH=amd64 go build -o bin/domain-scanner ./cmd/scanner

# DB migration tool
GOOS=linux GOARCH=amd64 go build -o bin/domain-migrate ./cmd/migrate

# Vue frontend
cd web && npm run build  # outputs to web/dist/
# Vite emits hashed chunks (e.g. assets/index.[hash].js), so JS/CSS are cache-safe.
# index.html MUST be served with `Cache-Control: no-cache` by Caddy, otherwise SPA users
# will keep loading stale chunk manifests after a deploy.
```

### Deployment Process

```bash
# 1. Build
make build && make web

# 2. Upload
scp bin/domain-platform bin/domain-worker user@main-node:/opt/domain-platform/
scp -r web/dist/ user@main-node:/opt/domain-platform/web/dist/
scp bin/domain-scanner user@cn-probe-ct:/opt/domain-scanner/

# 3. Migrate
ssh main-node "/opt/domain-platform/domain-migrate up"

# 4. Restart
ssh main-node "sudo systemctl restart domain-platform domain-worker"
ssh cn-probe-ct "sudo systemctl restart domain-scanner"

# 5. Verify
curl https://platform.example.com/health
```
