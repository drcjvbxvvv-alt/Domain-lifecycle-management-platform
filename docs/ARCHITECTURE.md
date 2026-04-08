# ARCHITECTURE.md — System Architecture Reference

> **Aligned with PRD + ADR-0003 (2026-04-09).** This document is the detailed
> reference for the Domain Lifecycle & Deployment Platform. Read this when
> working on cross-cutting concerns, agent protocol, artifact pipeline, or
> deployment topology. CLAUDE.md is the higher-level entry point; this
> document fills in the details.

---

## 1. System Overview

The platform is an enterprise-grade HTML + Nginx release operations system
that manages 10+ projects and 1万+ domains. It builds **immutable artifacts**
in the control plane, distributes them through **Pull Agents** (Go binaries
on each Nginx server), verifies deployments with multi-tier probing, and
maintains a complete audit trail.

```
┌─────────────────────────────────────────────────────────────────────┐
│                       CONTROL PLANE                                  │
│  ┌──────────────┐    ┌────────────┐    ┌──────────────────┐         │
│  │ Vue 3 SPA    │◄──►│  REST API  │◄──►│  Auth / RBAC /   │         │
│  │ (Naive UI)   │    │  (Gin)     │    │  Approval flow   │         │
│  └──────────────┘    └─────┬──────┘    └──────────────────┘         │
│                            │                                          │
│  ┌─────────────────────────┴────────────────────────┐                │
│  │ Project / Lifecycle / Template / Artifact /      │                │
│  │ Release / Deploy / Agent / Probe / Alert         │                │
│  │              (internal/* services)               │                │
│  └─────────────────────────┬────────────────────────┘                │
└────────────────────────────┼─────────────────────────────────────────┘
                             │
        ┌────────────────────┼─────────────────────────┐
        ▼                    ▼                         ▼
┌──────────────┐   ┌──────────────────┐   ┌────────────────────┐
│ TASK & DATA  │   │  ARTIFACT STORE  │   │  EXECUTION PLANE   │
├──────────────┤   ├──────────────────┤   ├────────────────────┤
│ PostgreSQL   │   │  MinIO / S3      │   │  asynq workers     │
│ + Timescale  │   │  (immutable)     │   │  (cmd/worker)      │
│              │   │                  │   │                    │
│ Redis        │   │  manifest.json   │   │  release executor  │
│ (asynq + KV) │   │  + checksum      │   │  artifact builder  │
│              │   │  + signature     │   │  probe dispatcher  │
└──────────────┘   └────────┬─────────┘   │  notify dispatcher │
                            │              └─────────┬──────────┘
                            │ (download)             │
                            ▼                        │ (dispatch)
                  ┌──────────────────────────────────┴──┐
                  │       PULL AGENTS (cmd/agent)        │
                  │   ───────────────────────────────    │
                  │   Go binary, mTLS, whitelist-only    │
                  │                                       │
                  │   register / heartbeat / pull /       │
                  │   download / verify / write /         │
                  │   nginx -t / reload / report          │
                  │                                       │
                  │   one per Nginx host (host_group N)   │
                  └───────────────────────────────────────┘
```

The platform automates the full release lifecycle:

```
Domain Request → Approval → DNS Provision → Domain Active
     ↓
Template Author → Template Version Publish
     ↓
Release Create → Artifact Build → Sign → Plan Shards → Dispatch
     ↓
Agent Pull → Verify → Write → nginx -t → Reload → Report
     ↓
Probe Verify (L1 + L2 + L3) → Continue / Pause / Rollback → Audit
```

---

## 2. Subsystem Responsibilities

### 2.1 Project Management (`internal/project`)

- CRUD for `projects` table
- Project membership (Phase 4)
- Project-level settings (default approver, notify channel, default
  release policy)
- Soft delete; hard delete only after all releases archived

### 2.2 Domain Lifecycle (`internal/lifecycle`)

The Domain Lifecycle module manages domain identity and provisioning. It is
**prerequisite to** the deployment system — only `active` domains can be the
target of a release.

State machine (CLAUDE.md §"Domain Lifecycle State Machine"):

```
requested → approved → provisioned → active → disabled → active
                                       │              │
                                       ▼              ▼
                                    retired        retired
```

**Single write path (ADR-0003 D9, methodology from ADR-0002 D2):**

All `domains.lifecycle_state` mutations go through
`internal/lifecycle.Service.Transition(ctx, id, from, to, reason, triggeredBy)`.
This method runs entirely inside one Postgres transaction:

1. `SELECT lifecycle_state FROM domains WHERE id = $1 FOR UPDATE`
2. Assert `current == from` → otherwise `ErrLifecycleRaceCondition`
3. Assert `CanTransition(from, to)` → otherwise `ErrInvalidLifecycleState`
4. `UPDATE domains SET lifecycle_state, updated_at = NOW()`
5. `INSERT domain_lifecycle_history (from_state, to_state, reason, triggered_by)`
6. `COMMIT`

No other package may write `domains.lifecycle_state` directly. Enforced by
`make check-lifecycle-writes` CI grep gate.

**Provisioning flow (`approved → provisioned`)**: an asynq task
`lifecycle:provision` picks up the row, calls the configured DNS provider
(`pkg/provider/dns`) to create A/CNAME records, on success transitions to
`provisioned`. Failures retry with exponential backoff (1min, 5min, 15min,
then operator intervention required).

**Approval (`requested → approved`)**: in Phase 1, an "auto-approve" code path
exists for development. Phase 4 wires this to the approval module
(`internal/approval`) which requires a Release Manager or Admin to grant.

### 2.3 Template Management (`internal/template`)

Templates define the structure of HTML and nginx files for a project. Each
template has many `template_versions`; **versions are immutable once published**.

```
templates (1) ──────── (N) template_versions
                              │
                              ├── content_html (Go text/template source)
                              ├── content_nginx (Go text/template source)
                              ├── default_variables (JSONB)
                              ├── published_at (NULL = draft)
                              └── checksum
```

Releases pin to a specific `template_version_id`, never to a `template_id`.
Editing a "live" template means publishing a new version and creating a new
release; old releases continue to reference the old version forever.

**Variables**: each domain has its own `domain_variables` JSONB blob. At
artifact build time, the template is rendered against
`merge(template.default_variables, domain.variables)`.

### 2.4 Artifact Build Pipeline (`internal/artifact`)

An artifact is **the immutable product of rendering a template version against
a set of domains**. PRD §8.1 defines the structure:

```
artifacts/
  {project_slug}/
    {release_id}/
      manifest.json       # see below
      checksums.txt       # SHA-256 of every file
      signature           # cosign / GPG / HMAC, depending on ADR-0004
      domains/
        {fqdn}/
          index.html
          assets/...
      nginx/
        {host_group}/
          {fqdn}.conf
```

**`manifest.json` schema** (see `pkg/agentprotocol/manifest.go`):

```json
{
  "artifact_id": "art_01HXYZ...",
  "release_id":  "rel_01HXYZ...",
  "project":     "project-a",
  "release_type": "html|nginx|full",
  "template_version_id": "tv_01HXYZ...",
  "template_version":    "v23",
  "created_at":   "2026-04-09T10:00:00Z",
  "created_by":   "user_01HXYZ...",
  "source": {
    "template_checksum": "sha256:...",
    "variables_hash":    "sha256:..."
  },
  "domains": [
    {
      "fqdn": "a.example.com",
      "files": [
        { "path": "domains/a.example.com/index.html", "size": 1234, "sha256": "..." }
      ]
    }
  ],
  "nginx": [
    {
      "host_group": "host-group-01",
      "files": [
        { "path": "nginx/host-group-01/a.example.com.conf", "size": 567, "sha256": "..." }
      ]
    }
  ],
  "checksum": "sha256:...",
  "signature": "..."
}
```

**Build steps**:

1. **Plan**: enumerate domains in scope × template version; compute output paths.
2. **Render**: for each domain, render `template.content_html` and
   `template.content_nginx` against the merged variables. Each renderer is a
   pure function: same input → same bytes.
3. **Hash**: SHA-256 every output file. Aggregate into `checksums.txt`.
4. **Manifest**: write `manifest.json` with the file list + per-file hashes.
5. **Compute artifact checksum**: `sha256(manifest.json || checksums.txt)`.
6. **Sign**: append signature per ADR-0004 scheme.
7. **Upload**: `pkg/storage` writes the entire tree to
   `s3://{bucket}/{project}/{release_id}/`.
8. **Mark `artifacts.signed_at = NOW()`** — after this point the row is immutable.

**Idempotency contract**: an artifact build with the same `(template_version_id,
domain_set, variables_hash)` MUST produce byte-identical output. This means:

- No timestamps in rendered content (use the `created_at` in the manifest, not in HTML)
- No random seeds
- Variable iteration order is sorted
- File ordering in `checksums.txt` is sorted

Violations are caught by a reproducibility test in `internal/artifact/build_test.go`
that runs the same build twice and asserts byte equality.

### 2.5 Release Subsystem (`internal/release`)

A Release is one orchestrated rollout of one artifact across a defined scope.

**Hierarchy** (PRD §12):

```
Release
  └── Release Scope (which domains, which host_groups, which release_type)
       └── Release Shard (200-500 domains per shard, sized for canary first)
            └── Domain Task (one per domain × per host)
                 └── Agent Task (the actual instruction sent to one agent)
```

**State machine** (CLAUDE.md §"Release State Machine"):

```
pending → planning → ready → executing → succeeded
                                │
                                ├→ paused → executing
                                │
                                └→ failed → rolling_back → rolled_back
```

**Single write path**: all `releases.status` mutations go through
`internal/release.Service.TransitionRelease()`. Same pattern as Domain
Lifecycle. CI gate: `make check-release-writes`.

**Shard sizing** (PRD §12):

- Normal shards: 200–500 domains
- **Canary shard (shard 0)**: smaller of `min(30, 5%)` of total domains, hard
  minimum 10. Rationale: blast radius small enough that a rendering bug or
  bad nginx conf caught at canary costs at most one fast rollback.
- Shards within a release are partitioned by `hash(domain_id) % shard_count`
  so that retries of the same domain land in the same shard.
- A Release is always scoped to **one project**.

**Canary policy** (PRD §13):

- Deploy canary shard → wait for L2 probe verification → success rate ≥ 95% → continue
- Success rate < 95% → auto-pause Release, alert operators
- Per-host failure: `> N consecutive failures on the same host` → mark host
  in error and skip it for the rest of the release
- Operator can pause / resume / rollback any shard independently

**Rollback** (PRD §14):

- Rollback re-deploys the **previous successful artifact** for the same scope
- Rollback runs through the same shard pipeline (no special path)
- Rollback also undergoes probe verification
- Per-domain / per-shard / per-release granularity supported
- Rollback decisions are recorded in `rollback_records` with operator + reason

**4-layer concurrency limits** (PRD §12):

| Layer | Limit | Where enforced |
|---|---|---|
| Release | `max_concurrent_releases_per_project` (default 2) | `release.Service.Create()` checks count |
| Project | `max_concurrent_shards_per_project` (default 5) | shard dispatcher consults Redis counter |
| Host | `max_concurrent_tasks_per_host` (default 1; `nginx -t` is serial) | `agent.Service.Dispatch()` per-agent counter |
| Domain | `domain_task_lock:{domain_id}` Redis lock | `release:dispatch_shard` task acquires |

### 2.6 Agent Management (`internal/agent`)

The Agent subsystem on the **control plane side** is responsible for:

- **Registration**: accept `POST /agent/v1/register`, issue or accept
  certificate, write `agents` row, transition state to `registered → online`
- **Heartbeat tracking**: process `POST /agent/v1/heartbeat`, update
  `agent_heartbeats` and `agents.last_seen_at`, detect drift
- **Task dispatch**: when a release shard needs work done on host group X,
  enqueue `agent_tasks` rows targeted at the agents in that group
- **Pull endpoint**: serve `GET /agent/v1/tasks` — returns the next pending
  task for the calling agent (long-poll with timeout)
- **State machine**: `internal/agent.Service.TransitionAgent()` is the only
  write path for `agents.status`. CI gate: `make check-agent-writes`
- **Drain / disable / quarantine**: operator actions that change agent state
- **Self-upgrade dispatch**: `agent_upgrade_jobs` rows describe desired
  agent versions; canary upgrade picks N agents, monitors, expands

**State machine** (CLAUDE.md §"Agent State Machine"): the lifecycle includes
`registered → online ↔ offline / busy / idle / draining / disabled / upgrading
/ error`.

**Offline detection**: `TypeAgentHealthCheck` runs every 30s on the `critical`
queue. For each `online` agent whose `last_seen_at < NOW() - 90s`, transition
to `offline`. After `offline > 5min`, escalate to `error` and alert.

**Pull agent (the binary, on each Nginx host)** is described in §3.4 below.

### 2.7 Probe Subsystem (`internal/probe`) — Deployment Verification

> **Important**: probing in this platform is for **deployment verification**,
> not GFW detection. The L1/L2/L3 tiers below verify "did my release actually
> take effect", not "is this domain reachable from China". The previous
> CN-node probe network and GFW detection logic are out of scope per
> ADR-0003 D8 / D11.

**Three tiers** (PRD §15):

| Tier | Target | Checks | When |
|---|---|---|---|
| **L1** | All `active` domains | DNS resolves, TCP :443, HTTP status, response time | Every 5 min |
| **L2** | Domains in current/recent release | Expected `<meta name="release-version" content="...">`, expected title/keyword, content checksum | After every release shard succeeds, plus every 15 min for the most recent release |
| **L3** | Domains tagged `core` | Business endpoint health, specific API/resource availability | Every 1 min |

**L2 verification details**: every artifact build embeds the artifact ID into
the rendered HTML as `<meta name="release-version" content="{artifact_id}">`.
The probe fetches the page, parses the meta tag, and confirms the value matches
the artifact that *should* be deployed. This is the only way to be confident
the release actually landed (HTTP 200 alone is not enough — could be cached,
could be the previous version, could be a misconfigured fallback).

**Probe orchestration**: `cmd/worker` runs probe dispatchers that pull pending
checks from `probe_tasks` and execute HTTP / TLS / DNS calls. Results write to
the TimescaleDB `probe_results` hypertable (90-day retention). On state
changes (pass → fail or fail → pass), the alert engine is notified.

**Where probes run**:

- L1 / L2 / L3 from one or more "probe runner" hosts in the platform's own
  network (not from agents — agents only deploy)
- Phase 1 ships with one probe runner colocated with `cmd/worker`
- Phase 2+ may add geographically distributed probe runners

### 2.8 Alert & Notification (`internal/alert`)

**Severity** (PRD §16):

| Level | Trigger | Auto-action |
|---|---|---|
| **P1** | Core domain unavailable / nginx reload failed on > 20% of shard / release auto-paused | Page operators (Telegram + Webhook), pause releases |
| **P2** | Single-host failure / probe degraded / agent went offline | Notify (Telegram channel) |
| **P3** | Latency anomaly / non-critical L3 fail | Log only, daily digest |
| **INFO** | Release succeeded / agent recovered / rollback complete | Notify channel |

**Alert deduplication** (CLAUDE.md Critical Rule #8):

- Same `(target, alert_type, severity)` → 1 alert per hour max
- Multi-target alerts batch into one message ("3 domains in project A failed
  L2 probe: a.example.com, b.example.com, c.example.com")
- `alert_dedup:{type}:{target}` Redis key with TTL = dedup window

**Notification channels** (`pkg/notify`):

- Telegram bot (P1, configured via `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID`)
- Generic HTTP webhook (P1)
- Slack / Teams (P2+)

### 2.9 Approval Flow (`internal/approval`) — Phase 4

Production releases and nginx releases require an approval. The approval flow
adds:

- `approval_requests` table: `(release_id, requested_by, requested_at,
  required_role, status)` where `status IN ('pending', 'granted', 'denied')`
- A reviewer with role `release_manager` or `admin` can grant or deny
- A release in `ready` state with `approval_required = true` cannot transition
  to `executing` until a `granted` approval row exists
- Grant / deny is audited

In Phase 1, the schema is in place but an `auto_approve` mechanism is wired
so dev work can proceed without a separate approver.

### 2.10 Audit (`internal/audit`)

Every state transition, every operator action, every release decision writes
an `audit_logs` row:

```sql
audit_logs (
    id          BIGSERIAL,
    user_id     BIGINT,           -- nullable for system actions
    action      VARCHAR(64),      -- e.g. 'release.created', 'domain.transition.requested→approved'
    target_kind VARCHAR(32),      -- 'release', 'domain', 'agent', etc.
    target_id   VARCHAR(64),      -- UUID of target
    detail      JSONB,
    created_at  TIMESTAMPTZ
)
```

State machine `Transition()` calls write history rows to per-object tables
(`domain_lifecycle_history`, `release_state_history`, `agent_state_history`)
in addition to the global `audit_logs`.

---

## 3. Pull Agent Detailed Design

The Pull Agent is the most security-sensitive component of the platform —
it runs as root (or with sudo for nginx reload) on every Nginx host. Its
design is structurally constrained to minimize blast radius (PRD §3-§7,
§20-§21).

### 3.1 Binary characteristics

- Single Go static binary (`cmd/agent`), cross-compiled for `linux/amd64`
- Built by `make agent` → `bin/agent-linux-amd64`
- Deployed via systemd (unit file in `deploy/systemd/agent.service`)
- Configuration via `/etc/domain-platform/agent.yaml` + environment
- Resource footprint: target < 20 MB RAM idle, < 5% CPU heartbeat

### 3.2 Allowed actions (whitelist)

Per CLAUDE.md Critical Rule #3, the agent binary contains code paths for
**only** these actions:

1. **Register**: `POST /agent/v1/register` with hostname, IP, region,
   datacenter, host_group (from config), agent version, capabilities
2. **Heartbeat**: `POST /agent/v1/heartbeat` every 15s with current state,
   current task ID, last error
3. **Pull tasks**: long-poll `GET /agent/v1/tasks` (60s timeout)
4. **Claim task**: `POST /agent/v1/tasks/{id}/claim`
5. **Download artifact**: HTTP GET against signed S3 URL (URL is in the task)
6. **Verify checksum**: SHA-256 of every file vs `manifest.json`
7. **Verify signature**: per ADR-0004 scheme
8. **Write to staging**: copy files into `{deploy_path}/.staging/{release_id}/`
9. **Run `nginx -t`** against staging conf path (only if task is type `nginx` or `full`)
10. **Snapshot previous**: copy current files to `{deploy_path}/.previous/{release_id}/` (Critical Rule #6)
11. **Atomic swap**: rename staging → real path
12. **Reload nginx**: `nginx -s reload` (only if task allows + previous step succeeded)
13. **Local verification**: HTTP HEAD against `localhost:80/{configured_path}` returning expected code
14. **Report**: `POST /agent/v1/tasks/{id}/report` with status, duration, error message
15. **Upload logs**: `POST /agent/v1/logs` with the structured log buffer

### 3.3 Forbidden actions (NOT in the binary)

The binary contains **no** code paths for:

- `os/exec.Command(...)` outside of the four allowed shell-out points
  (`nginx -t`, `nginx -s reload`, the configured local-verify HTTP curl, and
   the systemd self-restart on upgrade) — each call site is reviewed and
  hard-coded, never user-input-derived
- Pulling from git, svn, http, or any source
- Reading or writing files outside `{deploy_path}` and `/etc/domain-platform`
- Loading dynamic plugins / `os.Plugin`
- Network calls to anywhere except the control plane and S3 / MinIO
- Storing third-party API tokens

This is enforced by:

- Code review on every PR touching `cmd/agent/`
- A `make check-agent-safety` grep gate that rejects PRs adding `os/exec`,
  `plugin`, or `net/http.Get` to URL-derived addresses
- The Opus model is required for any `cmd/agent/` PR per the Model Selection
  Policy

### 3.4 Wire protocol (in `pkg/agentprotocol`)

```go
// Registration
type RegisterRequest struct {
    Hostname     string            `json:"hostname"`
    IP           string            `json:"ip"`
    Region       string            `json:"region"`
    Datacenter   string            `json:"datacenter"`
    HostGroup    string            `json:"host_group"`
    AgentVersion string            `json:"agent_version"`
    Capabilities []string          `json:"capabilities"`
    Tags         map[string]string `json:"tags,omitempty"`
}
type RegisterResponse struct {
    AgentID    string `json:"agent_id"`
    AssignedAt int64  `json:"assigned_at"`
}

// Heartbeat
type HeartbeatRequest struct {
    AgentID         string  `json:"agent_id"`
    Status          string  `json:"status"`     // online | busy | idle | draining | error
    CurrentTaskID   string  `json:"current_task_id,omitempty"`
    AgentVersion    string  `json:"agent_version"`
    LoadAvg1        float64 `json:"load_avg_1,omitempty"`
    DiskFreePercent float64 `json:"disk_free_percent,omitempty"`
    LastError       string  `json:"last_error,omitempty"`
    Timestamp       int64   `json:"timestamp"`
}
type HeartbeatResponse struct {
    AcceptedAt        int64  `json:"accepted_at"`
    NextPollAfterMS   int    `json:"next_poll_after_ms"` // backoff hint
    DesiredAgentVersion string `json:"desired_agent_version,omitempty"`
}

// Task pull (long-poll)
type TaskEnvelope struct {
    TaskID       string         `json:"task_id"`
    ReleaseID    string         `json:"release_id"`
    ArtifactID   string         `json:"artifact_id"`
    ArtifactURL  string         `json:"artifact_url"`  // signed URL
    ManifestSHA  string         `json:"manifest_sha"`
    DeployPath   string         `json:"deploy_path"`
    Type         string         `json:"type"`          // html | nginx | full
    AllowReload  bool           `json:"allow_reload"`
    LocalVerify  *VerifyConfig  `json:"local_verify,omitempty"`
    TimeoutSec   int            `json:"timeout_sec"`
}

// Report
type TaskReport struct {
    TaskID      string `json:"task_id"`
    AgentID     string `json:"agent_id"`
    Status      string `json:"status"`        // success | failed | timeout
    DurationMS  int64  `json:"duration_ms"`
    Phases      []PhaseReport `json:"phases"` // download / verify / write / nginx_test / swap / reload / verify
    LastError   string `json:"last_error,omitempty"`
    NewVersion  string `json:"new_version,omitempty"`
}
```

### 3.5 mTLS

- A platform-internal CA issues per-agent client certificates
- Agent stores its key + cert in `/etc/domain-platform/agent.{key,crt}`
  (mode 0600, owned by the agent service user)
- Control plane verifies the client cert chain against the platform CA on
  every `/agent/v1/*` request
- Certificate validity: 1 year by default (`AGENT_CERT_VALIDITY=8760h`)
- Rotation: agent self-rotates 30 days before expiry by calling
  `POST /agent/v1/cert/rotate` (returns a new cert + key)
- Revocation: control plane maintains a CRL in Redis (`agent:crl` set);
  middleware checks this on every request
- Cert serial → agent_id mapping is in the `agents` table

### 3.6 Self-upgrade

Per PRD §21, agent self-upgrade follows canary semantics:

1. Operator tags a new agent binary version in `agent_versions` (uploaded to MinIO)
2. Operator creates an `agent_upgrade_jobs` row with target version + scope (host group / tags)
3. Control plane picks N (canary count, default 3) agents and sends upgrade tasks
4. Each canary agent: download new binary → checksum verify → swap → systemd restart
5. After restart the new agent registers fresh, heartbeats, and reports its new version
6. Control plane monitors canary for 30 minutes
7. If all healthy → expand to 25% → 50% → 100% (configurable)
8. If any error → halt upgrade, leave canaries on new version, alert operator
9. Operator can roll back: `agent_upgrade_jobs.rollback_version` triggers another upgrade in reverse

---

## 4. Data Architecture

### 4.1 PostgreSQL Tables

See `docs/DATABASE_SCHEMA.md` for the authoritative schema. High-level groups:

| Group | Tables |
|---|---|
| Identity | `users`, `roles`, `user_roles` |
| Project | `projects` |
| Domain Lifecycle | `domains`, `domain_variables`, `domain_lifecycle_history` |
| Template | `templates`, `template_versions` |
| Artifact | `artifacts` |
| Release | `releases`, `release_scopes`, `release_shards`, `release_state_history` |
| Tasks | `domain_tasks`, `agent_tasks`, `deployment_logs` |
| Rollback | `rollback_records` |
| Probe | `probe_policies`, `probe_tasks` (TimescaleDB: `probe_results`) |
| Alert | `alert_events`, `notification_rules` |
| Agent | `agents`, `agent_heartbeats`, `agent_versions`, `agent_upgrade_jobs`, `agent_logs`, `agent_state_history` |
| Approval | `approval_requests` (Phase 4) |
| Audit | `audit_logs` |

### 4.2 TimescaleDB

Only `probe_results` is a hypertable. 90-day retention. Excluded from
nightly `pg_dump`. Compaction: chunks > 7 days are compressed.

### 4.3 Redis Key Design

```
# Distributed locks
release:lock:{release_id}              SET NX PX  Release planning lock
release:dispatch:lock:{shard_id}       SET NX PX  Shard dispatch lock
domain_task:lock:{domain_id}           SET NX PX  Per-domain task lock
agent:cert:rotate:{agent_id}           SET NX PX  Cert rotation lock

# Counters
project:concurrent_shards:{project_id}    INCR/DECR    For 4-layer concurrency limits
agent:concurrent_tasks:{agent_id}         INCR/DECR

# Dedup / aggregation
alert_dedup:{type}:{target}               SET EX       Alert dedup
nginx_reload_buffer:{agent_id}            LIST + EXPIRE

# Long-poll synchronization
agent:tasks:available:{agent_id}          PUB/SUB      Notify agent there's a new task

# Certificate revocation list
agent:crl                                 SET (members are cert serials)

# asynq internal keys
asynq:*                                   reserved by hibiken/asynq
```

---

## 5. Communication Security

### 5.1 Agent ↔ Control Plane: mTLS

- Platform CA issues per-agent client certificates
- Server certificate is the standard public web cert (Let's Encrypt via Caddy)
- Both sides verify
- Client cert revocation list (CRL) checked on every request
- See §3.5 for cert rotation

### 5.2 Management Console: JWT + HTTPS

- JWT Bearer tokens issued by `/api/v1/auth/login` (username + password,
  bcrypt)
- 24-hour validity by default; refreshable
- Stored in localStorage on the SPA side
- Caddy handles HTTPS termination
- All `/api/v1/*` routes require valid JWT except `/api/v1/auth/login`

### 5.3 Artifact Storage Access

- Control plane uses MinIO root credentials (or scoped IAM role) to write
- Agents receive **time-limited signed URLs** (presigned, 15-minute validity)
  in their task envelopes — they never hold permanent S3 credentials
- Bucket policy: control plane RW, agents read-only via presigned URLs only

### 5.4 Secrets Management

- DB password, JWT secret, S3 credentials, Telegram bot token, DNS provider
  tokens: loaded from environment variables OR a secrets file outside the
  repo (`configs/secrets.yaml`, gitignored)
- For production: recommended to integrate with Vault / AWS Secrets Manager
  / GCP Secret Manager (Phase 4)
- Agent mTLS keys are generated per-agent and never committed

---

## 6. Deployment Topology

### 6.1 Phase 1 minimum

```
┌── Platform side (single host or small cluster) ───────────┐
│                                                             │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Caddy → cmd/server  (REST API on :8080)                │ │
│  │         cmd/worker  (asynq workers)                    │ │
│  │         cmd/server  (mTLS endpoint on :8443 for agents)│ │
│  └────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ PostgreSQL 16 + TimescaleDB                            │ │
│  │ Redis 7                                                │ │
│  │ MinIO (artifact storage)                               │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ mTLS (https + client cert)
                              │
            ┌─────────────────┼─────────────────┐
            ▼                 ▼                 ▼
    ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
    │ Nginx host 1 │  │ Nginx host 2 │  │  Nginx host N│
    │ + cmd/agent  │  │ + cmd/agent  │  │ + cmd/agent  │
    └──────────────┘  └──────────────┘  └──────────────┘
```

PRD §26 minimum recommendation for production:

| Component | Sizing | Count |
|---|---|---|
| Control plane (server + worker, can be co-located) | 4C/8G | 2 (HA) |
| PostgreSQL + TimescaleDB | 4C/16G + SSD | 1 (Phase 1), 2 with replication (Phase 4) |
| Redis | 2C/4G | 1 |
| MinIO | 4C/8G + storage | 1 (Phase 1), distributed (Phase 4) |
| Probe runner (colocated with worker in P1) | 2C/4G | 1 (Phase 1), N (Phase 3) |
| Pull Agent (per Nginx host) | trivial overhead | 1 per Nginx host |

### 6.2 Backup & Disaster Recovery

- **PostgreSQL**: nightly `pg_dump` of all business tables (everything except
  `probe_results`); WAL archiving to off-site storage every 5 minutes (RPO ≤ 5min)
- **MinIO**: lifecycle policy mirrors artifacts to a secondary bucket
  (could be cross-region); artifacts are immutable so partial sync is safe
- **Redis**: AOF `everysec` for asynq state; can be lost without data
  corruption (releases will resume from DB state on restart)
- **TimescaleDB `probe_results`**: 90-day retention, NOT backed up (loss
  acceptable)
- **Configs / secrets**: separate private repository, never in main repo
- **RTO**: ≤ 2 hours for the platform; agents continue running last-known
  releases without control plane
- **Agent disconnection**: agents that lose control-plane connectivity stop
  pulling new tasks but the last deployed artifact continues serving traffic
  (zero impact on running production)

---

## 7. Build & Deploy

### 7.1 Build Artifacts

| Binary | Source | Target | Size (target) |
|---|---|---|---|
| `server` | `cmd/server` | linux/amd64 | < 25 MB |
| `worker` | `cmd/worker` | linux/amd64 | < 25 MB |
| `migrate` | `cmd/migrate` | linux/amd64 | < 15 MB |
| `agent` | `cmd/agent` | **linux/amd64 (cross-compiled from any host)** | < 20 MB |

```bash
make build       # builds server worker migrate agent
make agent       # cross-compile agent only (most common during agent dev)
make web         # builds Vue dist into web/dist/
```

### 7.2 Frontend Cache Rule (carried from ADR-0001 D12)

`web/dist/index.html` must be served by Caddy with `Cache-Control: no-cache`.
Vite already emits hashed JS chunks; without this header, users open the
console pre-deploy and load stale chunk manifests post-deploy, hitting 404s.

### 7.3 Deployment Process

1. Build binaries on a CI runner (or developer machine)
2. `scp` binaries to platform host(s)
3. `systemctl restart domain-server domain-worker`
4. Run `make migrate-up` if migrations are pending
5. For agents: upload new agent binary as a new `agent_versions` row in MinIO,
   then create an `agent_upgrade_jobs` row to roll it out via canary

---

## 8. Implementation Phases

The phasing is **per PRD §28**, with Phase 1 expanded to include the platform
foundation.

| Phase | Scope | Gate |
|---|---|---|
| **Phase 1** | Project / Domain (basic CRUD, auto-approve) / Template / Artifact build / Basic Release (no sharding, no canary, no probe) / Agent (register / heartbeat / pull / report) | A user can: log in → create project → register domain → write template → publish version → create release → agent picks up task → deploys → reports back → see in UI |
| **Phase 2** | Sharding / Rollback / Dry-run / Diff / Per-host concurrency limit / Agent management UI (list, drain, disable) | Multi-shard releases with rollback work end-to-end; operator can drain an agent |
| **Phase 3** | Gray release (canary) / Probe L1+L2+L3 / Alert engine with dedup / Agent canary upgrade | Releases are gated by probe verification; alerts deduplicate; agents self-upgrade |
| **Phase 4** | Domain lifecycle approval flow / Nginx artifact deployment (separated from HTML) / Approval flow / High availability | Production releases require approved approval row; nginx releases require Release Manager |
| **(Future, unscheduled)** | GFW vertical (separate ADR-0004 or later) | N/A |

The Phase 1 task list is in `docs/PHASE1_TASKLIST.md`.
