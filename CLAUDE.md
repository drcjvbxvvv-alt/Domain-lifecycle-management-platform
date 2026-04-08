# CLAUDE.md — Domain Lifecycle & Deployment Platform

> **Single source of truth as of ADR-0003 (2026-04-09).** This file replaces
> the GFW-failover-centric CLAUDE.md that was in effect through 2026-04-08.
> See [`docs/adr/0003-pivot-to-generic-release-platform-2026-04.md`](docs/adr/0003-pivot-to-generic-release-platform-2026-04.md)
> for the pivot rationale and what was preserved versus discarded.

## Project Identity

- **Name**: Domain Lifecycle & Deployment Platform (域名生命週期與發布運維平台)
- **Purpose**: Enterprise HTML + Nginx release platform for managing 10+ projects
  and 1万+ domains. Builds immutable artifacts in the control plane, deploys
  them through pull-based Go agents on each Nginx server, with canary,
  rollback, probe verification, alerting, and full audit.
- **Architecture**: 4-layer — Control Plane (`cmd/server`) + Task & Data Layer
  (PostgreSQL + Redis + MinIO/S3 + TimescaleDB) + Execution Plane
  (`cmd/worker`) + Pull Agent (`cmd/agent`, single Go binary on each Nginx
  host).
- **PRD**: `/Users/ahern/Documents/AI-tools/Domain Lifecycle & Deployment Platform（域名生命週期與發布運維平台）.md`

> **Out of scope (parked, not abandoned)**: GFW failover, mainland-China
> reachability monitoring, prefix-based subdomain auto-generation, standby
> domain pool with warmup/promotion, automated DNS+CDN switching. These will
> return as a future vertical built on top of this platform; see ADR-0003 D11.

---

## Tech Stack (Non-Negotiable)

| Layer | Technology | Notes |
|-------|-----------|-------|
| Language | Go 1.22+ | All backend services AND the agent |
| API Framework | Gin | RESTful JSON API |
| DB Access | sqlx | Raw SQL + struct scanning, NO ORM magic |
| Task Queue | asynq (hibiken/asynq) | Redis-backed async tasks |
| Template Engine | Go `text/template` | Artifact rendering (HTML + nginx conf) |
| Config | Viper | YAML + env vars |
| Logging | Zap (uber-go/zap) | Structured JSON logs |
| Auth | golang-jwt/jwt/v5 | JWT Bearer tokens for the management console |
| Agent ↔ Control Plane | mTLS over HTTPS | Per-agent certificate, rotated |
| Artifact Storage | MinIO (S3-compatible) | Immutable artifacts, checksum + signature |
| Artifact Signing | TBD per ADR-0004 | cosign / GPG / HMAC — chosen at P1.8 time |
| DB | PostgreSQL 16 + TimescaleDB | Single instance for business + probe time-series |
| Cache/Queue | Redis 7 | Short-lived state + asynq broker |
| Frontend | Vue 3 + Naive UI + TypeScript | SPA management console |
| Build | Vite | Frontend tooling |
| State | Pinia | Vue global state |
| Reverse Proxy | Caddy | Auto HTTPS + static files |

---

## Project Structure

```
domain-platform/
├── cmd/                      # Entry points (one main.go per binary)
│   ├── server/               # Control Plane: API + Web server
│   ├── worker/               # Execution Plane: asynq task worker
│   ├── agent/                # Pull Agent: single binary deployed to each Nginx server
│   ├── migrate/              # DB migration tool
│   └── scanner/              # PARKED — reserved for future GFW vertical (ADR-0003 D10)
├── internal/                 # Internal packages (NOT importable)
│   ├── project/              # Project management
│   ├── lifecycle/            # Domain lifecycle module: requested → ... → retired
│   ├── template/             # Template + template_versions + variables
│   ├── artifact/             # Artifact build pipeline + manifest + signature
│   ├── release/              # Release subsystem: shards, scopes, executors
│   ├── deploy/               # Deployment orchestration (release → shards → tasks)
│   ├── agent/                # Agent management (control-plane side): registration,
│   │                         # heartbeat, task dispatch, state machine, drain, upgrade
│   ├── probe/                # Probe orchestration (L1 / L2 / L3 deployment verification)
│   ├── alert/                # Alert engine: dedup, aggregation, severity, notify
│   ├── approval/             # Approval flow for prod releases (Phase 4)
│   └── audit/                # Audit log writes
├── pkg/                      # Exportable packages
│   ├── agentprotocol/        # Agent ↔ Control Plane wire protocol (request/response types,
│   │                         # task envelopes, manifest format) — shared by cmd/server
│   │                         # and cmd/agent
│   ├── storage/              # Artifact storage interface + MinIO implementation
│   ├── provider/dns/         # DNS provider abstraction (used by lifecycle module
│   │                         # to provision DNS records when a domain transitions to
│   │                         # provisioned state)
│   ├── template/             # Template rendering helpers (text/template wrappers)
│   └── notify/               # Notification: Telegram + Slack + Webhook
├── api/                      # API definitions
│   ├── handler/              # Gin handlers (control plane)
│   ├── middleware/           # Auth, RBAC, Logger, RateLimit
│   └── router/               # Route registration
├── store/                    # Data access layer
│   ├── postgres/             # PostgreSQL queries (sqlx)
│   ├── timescale/            # TimescaleDB probe results
│   └── redis/                # Redis operations
├── migrations/               # SQL migration files (sequential numbering)
├── templates/                # Built-in / system template files (.tmpl) — examples,
│                             # smoke tests; user templates live in DB
├── deploy/                   # Deployment artifacts
│   ├── docker-compose.yml    # PostgreSQL + Redis + MinIO + (optional) TimescaleDB
│   └── systemd/              # systemd unit files for server / worker / agent
├── web/                      # Vue 3 frontend
│   ├── src/
│   │   ├── api/              # API client + TypeScript types
│   │   ├── views/            # Page components
│   │   ├── components/       # Reusable components (FRONTEND_GUIDE.md)
│   │   ├── stores/           # Pinia stores
│   │   ├── composables/      # Vue composables (useXxx)
│   │   ├── router/           # Vue Router config
│   │   ├── types/            # Shared TypeScript types
│   │   └── utils/            # Utility functions
│   └── package.json
├── docs/                     # Architecture docs & ADRs
├── configs/                  # Config examples
├── Makefile
├── go.mod
└── go.sum
```

> The `cmd/scanner/` directory and `pkg/svnagent/` directory may exist on disk
> as historical leftovers but are NOT referenced by the current architecture.
> `cmd/scanner` is parked for future GFW work; `pkg/svnagent` is dead and will
> be removed once that future work is scheduled.

---

## Go Coding Standards

### Naming

- Package names: lowercase, single word, no underscores (`agent`, `lifecycle`, `artifact`)
- Files: `snake_case.go`
- Exported: `PascalCase` — Unexported: `camelCase`
- Interfaces: describe behavior, NOT prefixed with `I` (e.g., `ArtifactStore` not `IArtifactStore`)
- Receiver names: 1-2 letter abbreviation of type (`func (s *Server) Start()`)

### Error Handling

```go
// ALWAYS check errors. NEVER use panic for business logic.
artifact, err := store.GetArtifact(ctx, id)
if err != nil {
    return fmt.Errorf("get artifact %s: %w", id, err)
}

// Use sentinel errors for expected conditions
var (
    ErrDomainNotFound        = errors.New("domain not found")
    ErrArtifactNotFound      = errors.New("artifact not found")
    ErrInvalidLifecycleState = errors.New("invalid domain lifecycle transition")
    ErrInvalidReleaseState   = errors.New("invalid release state transition")
    ErrInvalidAgentState     = errors.New("invalid agent state transition")
    ErrAgentOffline          = errors.New("agent offline")
    ErrChecksumMismatch      = errors.New("artifact checksum mismatch")
    ErrSignatureInvalid      = errors.New("artifact signature invalid")
    ErrApprovalRequired      = errors.New("approval required")
)

// Wrap with context at every layer boundary
if err != nil {
    return fmt.Errorf("dispatch shard %d: %w", shardID, err)
}
```

### Structs & DTOs

```go
// Model — maps to DB row. Lives in store/ or internal/.
type Domain struct {
    ID             int64     `db:"id"`
    UUID           string    `db:"uuid"`
    ProjectID      int64     `db:"project_id"`
    FQDN           string    `db:"fqdn"`
    LifecycleState string    `db:"lifecycle_state"`
    OwnerUserID    int64     `db:"owner_user_id"`
    CreatedAt      time.Time `db:"created_at"`
    UpdatedAt      time.Time `db:"updated_at"`
}

// Request DTO — API input. Lives in api/handler/.
type RegisterDomainRequest struct {
    ProjectID int64    `json:"project_id" binding:"required"`
    FQDN      string   `json:"fqdn"       binding:"required,fqdn"`
    Tags      []string `json:"tags"`
}

// Response DTO — API output. Lives in api/handler/.
type DomainResponse struct {
    UUID           string    `json:"uuid"`
    FQDN           string    `json:"fqdn"`
    LifecycleState string    `json:"lifecycle_state"`
    ProjectID      int64     `json:"project_id"`
    Tags           []string  `json:"tags,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
}

// NEVER expose DB model directly in API response.
// ALWAYS convert Model → Response DTO explicitly.
```

### Interfaces & Dependency Injection

```go
// Define interfaces where they are USED, not where they are implemented.
// internal/release/service.go
type ArtifactStore interface {
    Put(ctx context.Context, ref ArtifactRef, body io.Reader, meta Manifest) error
    Get(ctx context.Context, ref ArtifactRef) (io.ReadCloser, *Manifest, error)
    Stat(ctx context.Context, ref ArtifactRef) (*Manifest, error)
}

type AgentDispatcher interface {
    Enqueue(ctx context.Context, agentID string, task AgentTask) error
}

type Service struct {
    store      ReleaseStore
    artifacts  ArtifactStore
    agents     AgentDispatcher
    tasks      *asynq.Client
    logger     *zap.Logger
}

func NewService(store ReleaseStore, artifacts ArtifactStore, agents AgentDispatcher, tasks *asynq.Client, logger *zap.Logger) *Service {
    return &Service{store: store, artifacts: artifacts, agents: agents, tasks: tasks, logger: logger}
}
```

### Context & Timeouts

```go
// ALWAYS pass context. ALWAYS set timeouts for external calls.
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()

// MinIO/S3 upload:        120s timeout (large artifacts)
// MinIO/S3 download:      120s timeout
// Agent task RPC:         30s timeout
// DNS provider call:      30s timeout
// DB queries:             5s timeout (default via connection pool)
// Probe HTTP check:       8s timeout per target
```

### Database Queries (sqlx)

```go
// Use named queries for inserts/updates
const insertDomain = `
    INSERT INTO domains (uuid, project_id, fqdn, lifecycle_state, owner_user_id, created_at, updated_at)
    VALUES (:uuid, :project_id, :fqdn, :lifecycle_state, :owner_user_id, NOW(), NOW())
    RETURNING id`

// Use $N placeholders for selects
const getDomainByID = `
    SELECT id, uuid, project_id, fqdn, lifecycle_state, owner_user_id, created_at, updated_at
    FROM domains
    WHERE id = $1 AND deleted_at IS NULL`

// ALWAYS use transactions for multi-table writes
tx, err := db.BeginTxx(ctx, nil)
if err != nil {
    return fmt.Errorf("begin tx: %w", err)
}
defer tx.Rollback()
// ... operations ...
return tx.Commit()
```

### Testing

```go
// File naming: xxx_test.go in the same package
// Use testify/assert for assertions
// Use table-driven tests for multiple cases

func TestLifecycleService_Transition(t *testing.T) {
    tests := []struct {
        name    string
        from    string
        to      string
        wantErr error
    }{
        {"valid: requested → approved", "requested", "approved", nil},
        {"valid: approved → provisioned", "approved", "provisioned", nil},
        {"valid: provisioned → active", "provisioned", "active", nil},
        {"invalid: requested → active", "requested", "active", ErrInvalidLifecycleState},
        {"invalid: retired → active", "retired", "active", ErrInvalidLifecycleState},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { /* ... */ })
    }
}

// State-machine race tests are MANDATORY for all three new state machines
// (Domain Lifecycle, Release, Agent). Run with -race -count=50.

// Mock interfaces for unit tests — define mock in _test.go file
type mockArtifactStore struct {
    putFn func(ctx context.Context, ref ArtifactRef, body io.Reader, meta Manifest) error
}
func (m *mockArtifactStore) Put(ctx context.Context, ref ArtifactRef, body io.Reader, meta Manifest) error {
    return m.putFn(ctx, ref, body, meta)
}
```

### Logging

```go
// Use structured logging. NEVER use fmt.Println in production code.
logger.Info("artifact built",
    zap.String("artifact_id", manifest.ID),
    zap.String("project", manifest.Project),
    zap.Int("domain_count", len(manifest.Domains)),
    zap.String("checksum", manifest.Checksum),
)

logger.Error("agent task failed",
    zap.String("agent_id", agentID),
    zap.String("task_id", taskID),
    zap.String("phase", "verify_checksum"),
    zap.Error(err),
)

// Log levels:
// Debug — development diagnostics
// Info  — normal operations (artifact built, release dispatched, agent registered, task completed)
// Warn  — recoverable anomalies (retry triggered, agent draining, probe retry)
// Error — failures requiring attention (checksum mismatch, agent crashed, nginx -t failed)
```

---

## Domain Object Model

The platform has five top-level business objects, each owning its own state
machine and its own subsystem:

| Object | Owner package | Cardinality | State machine |
|---|---|---|---|
| **Project** | `internal/project` | many | none (just CRUD + soft delete) |
| **Domain** | `internal/lifecycle` | many per project | `requested → approved → provisioned → active → disabled → retired` |
| **Template** + TemplateVersion | `internal/template` | many per project; many versions per template | none on Template; TemplateVersion is immutable once published |
| **Artifact** | `internal/artifact` | many per release | immutable after build; only metadata can change (e.g., `signed_at`) |
| **Release** | `internal/release` | many per project | `pending → planning → ready → executing → succeeded` (with branches to `paused`, `failed`, `rolling_back`, `rolled_back`, `cancelled`) |
| **Agent** | `internal/agent` | many globally; tagged by host_group / region | `registered → online ↔ offline` (and `busy / idle / draining / disabled / upgrading / error`) |

### Domain Lifecycle State Machine

```
requested ──→ approved ──→ provisioned ──→ active ──→ disabled
                                              │           │
                                              │           ▼
                                              │       active (re-enable)
                                              ▼
                                            retired (terminal)
```

**Semantics**:
- `requested`: User has filed a domain registration request. Awaiting Admin / Release Manager approval.
- `approved`: Admin approved. The provisioning worker will pick this up and call DNS provider to create records.
- `provisioned`: DNS records exist; domain is technically reachable but no template/release has been bound to it yet.
- `active`: Domain is bound to a template + has a current release. **Only `active` domains can be the target of a new release.**
- `disabled`: Operator disabled the domain (e.g., maintenance, dispute). Releases are blocked. Reversible.
- `retired`: Terminal. The domain is permanently retired from the platform. DNS records may or may not still exist (operator decision). The FQDN cannot be re-registered without a new `requested` row.

**State transitions are validated AND go through `Transition()`.** Never set
`lifecycle_state` directly — always use the state machine via the single write
path (see Critical Rule #1).

```go
// internal/lifecycle/statemachine.go
var validLifecycleTransitions = map[string][]string{
    "requested":   {"approved", "retired"},      // can be rejected directly to retired
    "approved":    {"provisioned", "retired"},
    "provisioned": {"active", "disabled", "retired"},
    "active":      {"disabled", "retired"},
    "disabled":    {"active", "retired"},
    "retired":     {},                            // terminal
}
```

### Release State Machine

```
pending ──→ planning ──→ ready ──→ executing ──→ succeeded
   │           │           │          │
   ▼           ▼           ▼          ├──→ paused ──→ executing (resume)
cancelled  cancelled   cancelled      │              ↓
                                      │           rolling_back ──→ rolled_back
                                      └──→ failed ──→ rolling_back ──→ rolled_back
```

**Semantics**:
- `pending`: Just created. Awaiting planning (artifact build dispatched but not done).
- `planning`: Artifact is being built; release scopes are being computed; shards are being sized.
- `ready`: Artifact built and signed; shards computed; awaiting execution trigger (manual or auto per release policy).
- `executing`: Shards being dispatched to agents.
- `paused`: Operator or auto-pause hit (success rate threshold). Resumable.
- `succeeded`: All shards reported success and probe verification passed.
- `failed`: Hard failure that cannot be auto-recovered. Requires operator decision (rollback or cancel).
- `rolling_back`: Rollback in progress (re-deploying previous artifact).
- `rolled_back`: Rollback completed. Terminal until a new release is created.
- `cancelled`: Operator cancelled before any shard executed. Terminal.

### Agent State Machine

```
registered ──→ online ──┬──→ busy ──→ online
                        │
                        ├──→ idle ──→ online
                        │
                        ├──→ draining ──→ disabled
                        │
                        ├──→ upgrading ──→ online (success) / error (failure)
                        │
                        └──→ offline ──┬──→ online (heartbeat resumed)
                                       │
                                       └──→ error (offline > threshold)

disabled ──→ online (operator re-enable)
error    ──→ online (operator clear) / disabled / quarantine
```

**Semantics**:
- `registered`: First contact made with control plane; certificate issued; awaiting first heartbeat.
- `online`: Heartbeat fresh; ready to accept tasks.
- `busy`: Currently executing a task.
- `idle`: Online with no current task (synonym for online + free; some UIs distinguish).
- `draining`: Operator initiated drain; agent finishes current tasks but accepts no new ones.
- `disabled`: Operator disabled. No tasks dispatched. Agent may still heartbeat.
- `upgrading`: Agent is downloading + installing a new agent binary (canary upgrade flow).
- `offline`: Heartbeat missed. After threshold, escalate to `error`.
- `error`: Hard error reported by agent OR offline > threshold. Operator must clear.

---

## Provider Abstraction Layer (DNS only — Phase 1 scope)

The previous design also abstracted CDN providers. Under the new architecture
**CDN configuration is not the platform's responsibility** — CDNs sit in front
of nginx and are managed externally. The platform deploys nginx config to the
origin servers via Pull Agents.

DNS providers ARE still abstracted because the **Domain Lifecycle module**
needs to create DNS records when a domain transitions `approved → provisioned`.

```go
// pkg/provider/dns/provider.go
package dns

type Provider interface {
    Name() string
    CreateRecord(ctx context.Context, zone string, record Record) (*Record, error)
    DeleteRecord(ctx context.Context, zone string, recordID string) error
    ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error)
    UpdateRecord(ctx context.Context, zone string, recordID string, record Record) (*Record, error)
}

type Record struct {
    ID      string
    Type    string  // A, AAAA, CNAME, MX, TXT
    Name    string  // record name (relative to zone)
    Content string  // target
    TTL     int
}
```

### Adding a New DNS Provider

1. Create `pkg/provider/dns/cloudflare.go` (or other vendor)
2. Implement the `Provider` interface
3. Register in `pkg/provider/dns/registry.go`: `Register("cloudflare", NewCloudflareProvider)`
4. Lifecycle module uses `dns.GetProvider(rule.DNSProvider)` — ZERO changes to `internal/` code

---

## Agent Protocol (Pull-based)

Per ADR-0003 D3, the Agent ↔ Control Plane communication is **pull-based over
HTTPS with mTLS**. The Agent is the active party for all requests; the control
plane is a stateless responder that consults its DB.

### Endpoints (control plane side, served by `cmd/server`)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/agent/v1/register` | First contact; agent posts its identity, control plane issues certificate (or accepts pre-provisioned cert) |
| `POST` | `/agent/v1/heartbeat` | Periodic alive ping; payload includes agent state, current task, host metrics |
| `GET`  | `/agent/v1/tasks` | Long-poll for new tasks assigned to this agent |
| `POST` | `/agent/v1/tasks/{task_id}/claim` | Mark task as claimed (`busy`) |
| `POST` | `/agent/v1/tasks/{task_id}/report` | Report progress / completion / failure |
| `POST` | `/agent/v1/logs` | Bulk upload of task logs |
| `GET`  | `/agent/v1/upgrade` | Check for agent self-upgrade |

### Agent's allowed actions (whitelist — PRD §5)

The Agent may ONLY do these things:

1. Register with control plane and heartbeat
2. Pull tasks assigned to itself
3. Download an artifact from MinIO/S3 (URL provided by control plane)
4. Verify artifact `checksum` and `signature`
5. Write artifact's HTML files to a configured path
6. Write artifact's nginx conf files to a configured path (staging area first)
7. Run `nginx -t` against the staging conf
8. If `nginx -t` succeeds AND control plane allowed reload → swap staging → real path → run `nginx -s reload`
9. Run a configured local verification script (HTTP HEAD against localhost)
10. Report results back to control plane
11. Upload logs to control plane

The Agent may NOT do any of these (PRD §5 explicit NOT list):

- Run arbitrary shell commands
- Pull from git/svn/any source repo
- Decide deployment scope
- Operate on hosts other than itself
- Persist business state
- Hold third-party credentials (DNS API tokens, CDN tokens, etc.)

The whitelist is enforced **in the Agent binary itself** (the binary literally
has no code path that runs an arbitrary shell command), not just by control
plane policy. This is a structural safety property, not a configuration.

---

## API Conventions

### URL Structure

```
/api/v1/{resource}              # Collection (management console)
/api/v1/{resource}/:id          # Single resource
/api/v1/{resource}/:id/{action} # Action on resource
/agent/v1/...                   # Agent protocol (mTLS, separate auth)
```

### Response Format

```json
// Success
{ "code": 0, "data": { ... }, "message": "ok" }

// Error
{ "code": 40001, "data": null, "message": "domain not found" }

// Paginated list
{ "code": 0, "data": { "items": [...], "total": 1200, "cursor": "eyJpZCI6MTAwfQ==" }, "message": "ok" }
```

### HTTP Status Codes

- 200: Success (GET, PUT, PATCH)
- 201: Created (POST)
- 202: Accepted (long-running async operations like release create, artifact build)
- 204: Deleted (DELETE)
- 400: Validation error
- 401: Not authenticated
- 403: Not authorized (RBAC, including approval requirements)
- 404: Resource not found
- 409: Conflict (duplicate FQDN, invalid state transition, missing approval)
- 500: Internal server error (always log, never expose details)

---

## Frontend Conventions (Vue 3)

See `docs/FRONTEND_GUIDE.md` for the full design system. Key rules:

```vue
<script setup lang="ts">
// 1. Imports
import { ref, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useDomainStore } from '@/stores/domain'
import type { DomainResponse } from '@/types/domain'

// 2. Props & Emits
const props = defineProps<{ projectId: number }>()
const emit = defineEmits<{ (e: 'refresh'): void }>()

// 3. Composables & Stores
const message = useMessage()
const domainStore = useDomainStore()

// 4. Reactive state
const loading = ref(false)
const domains = ref<DomainResponse[]>([])

// 5. Computed
const activeDomains = computed(() => domains.value.filter(d => d.lifecycle_state === 'active'))

// 6. Methods
async function fetchDomains() {
    loading.value = true
    try {
        domains.value = await domainStore.listByProject(props.projectId)
    } catch (err) {
        message.error('Failed to load domains')
    } finally {
        loading.value = false
    }
}

// 7. Lifecycle
onMounted(fetchDomains)
</script>
```

### TypeScript Types

```typescript
// web/src/types/domain.ts
// Mirror Go DTOs exactly. Keep in sync.
export interface DomainResponse {
    uuid: string
    fqdn: string
    project_id: number
    lifecycle_state: DomainLifecycleState
    tags?: string[]
    created_at: string
}

export type DomainLifecycleState =
    | 'requested' | 'approved' | 'provisioned'
    | 'active' | 'disabled' | 'retired'

export type ReleaseStatus =
    | 'pending' | 'planning' | 'ready' | 'executing'
    | 'paused' | 'succeeded' | 'failed'
    | 'rolling_back' | 'rolled_back' | 'cancelled'

export type AgentStatus =
    | 'registered' | 'online' | 'busy' | 'idle'
    | 'offline' | 'draining' | 'disabled'
    | 'upgrading' | 'error'
```

---

## Database Migrations

```
migrations/
├── 000001_init.up.sql            # Phase 1 tables (see DATABASE_SCHEMA.md)
├── 000001_init.down.sql
├── 000002_timescale.up.sql       # TimescaleDB hypertable for probe_results
├── 000002_timescale.down.sql
└── ...
```

### Migration Rules

1. Every UP migration MUST have a corresponding DOWN migration
2. **Pre-launch exception (carried over from ADR-0001 + ADR-0002, reaffirmed
   by ADR-0003 D9)**: `000001_init.up.sql` may be edited in place during
   Phase 1 because no production data exists. **This window closes at Phase 1
   cutover** — every subsequent schema change MUST be a new numbered migration
   file.
3. NEVER modify a migration that has been applied to a system with production data
4. Destructive operations (DROP COLUMN, DROP TABLE) require explicit comment explaining why
5. Add indexes in the same migration as the table creation
6. Use `IF NOT EXISTS` / `IF EXISTS` for safety
7. All tables MUST include: `id BIGSERIAL PRIMARY KEY`, `uuid UUID NOT NULL DEFAULT gen_random_uuid()`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `deleted_at TIMESTAMPTZ` (where soft-delete applies)

---

## Task Queue Patterns (asynq)

```go
// Task type constants — one file: internal/tasks/types.go
const (
    // Domain lifecycle
    TypeLifecycleProvision    = "lifecycle:provision"     // approved → provisioned (DNS provider call)
    TypeLifecycleDeprovision  = "lifecycle:deprovision"   // any → retired (DNS cleanup)

    // Artifact build
    TypeArtifactBuild         = "artifact:build"          // render template + variables → upload to MinIO
    TypeArtifactSign          = "artifact:sign"           // checksum + signature

    // Release execution
    TypeReleasePlan           = "release:plan"            // pending → planning → ready
    TypeReleaseDispatchShard  = "release:dispatch_shard"  // dispatch one shard's tasks to agents
    TypeReleaseProbeVerify    = "release:probe_verify"    // L2 probe after dispatch
    TypeReleaseFinalize       = "release:finalize"        // succeeded / failed transition
    TypeReleaseRollback       = "release:rollback"

    // Agent management
    TypeAgentHealthCheck      = "agent:health_check"      // periodic offline detection
    TypeAgentUpgradeDispatch  = "agent:upgrade_dispatch"

    // Probe
    TypeProbeRunL1            = "probe:run_l1"
    TypeProbeRunL2            = "probe:run_l2"
    TypeProbeRunL3            = "probe:run_l3"

    // Notify
    TypeNotifySend            = "notify:send"
)

// Task creation — include all data needed for execution
payload, _ := json.Marshal(ReleaseDispatchShardPayload{
    ReleaseID: rel.ID,
    ShardID:   shard.ID,
    AgentIDs:  shard.AgentIDs,
})
task := asynq.NewTask(TypeReleaseDispatchShard, payload,
    asynq.MaxRetry(3),
    asynq.Timeout(120*time.Second),
    asynq.Queue("release"),
)
```

### asynq Queue Layout (canonical)

| Queue | Tasks | Priority weight | Concurrency |
|-------|-------|-----------------|-------------|
| `critical` | `release:rollback`, `agent:health_check` (when escalating to error) | 10 | 20 |
| `release` | `release:plan`, `release:dispatch_shard`, `release:finalize` | 6 | 10 |
| `artifact` | `artifact:build`, `artifact:sign` | 5 | 5 (CPU bound) |
| `lifecycle` | `lifecycle:*` | 4 | 10 |
| `probe` | `probe:run_*` | 3 | 20 |
| `default` | `notify:send`, misc | 2 | 10 |

`strict: false` — weighted priority, not strict priority. `cmd/worker/main.go::asynq.Config.Queues` is the only place this layout is configured.

---

## Critical Business Rules

These are **load-bearing** rules that any contributor (human or AI) must
follow. Each rule has a single-line statement followed by an enforcement
mechanism.

1. **State machines have ONE write path each.** All `domains.lifecycle_state`
   mutations go through `internal/lifecycle.Service.Transition()`. All
   `releases.status` mutations go through `internal/release.Service.TransitionRelease()`.
   All `agents.status` mutations go through `internal/agent.Service.TransitionAgent()`.
   Each is enforced with a `make check-*-writes` CI grep gate; see ADR-0003 D9
   and ADR-0002 D2 (the original methodology).

2. **Artifacts are immutable.** Once `artifacts.signed_at` is set, the
   manifest, checksum, and content of an artifact MUST NOT change. Rollback
   means "redeploy a previous artifact_id", not "rebuild and overwrite".
   Enforcement: store-layer write methods reject updates to signed artifacts;
   filesystem/MinIO writes use content-addressed paths.

3. **Agent only does whitelisted actions.** The agent binary MUST NOT contain
   code paths that execute arbitrary shell commands, pull from arbitrary URLs,
   or write to arbitrary paths. The whitelist is enforced structurally
   (the code literally cannot do these things), not by configuration. Any PR
   that adds an `os/exec.Command` to `cmd/agent/` requires Opus review.

4. **Releases are scoped to one project.** A `releases.project_id` is required
   and immutable. Cross-project releases are not supported and never will be.
   If an operator wants to release across projects, they create N releases.

5. **Production releases require approval.** A release in a `prod`-tagged
   project (or any release with `release_type='nginx'`) cannot transition
   from `ready → executing` without an `approval_requests` row in `granted`
   state, reviewed by a user with role `release_manager` or `admin`.
   (Phase 4 deliverable; Phase 1 implements the schema and check, but seeds
   an "auto-approve" path so dev can proceed.)

6. **Every artifact deploy must snapshot the previous state.** Before the
   agent swaps staging → real path, it MUST copy the current files into
   `{deploy_path}/.previous/{release_id}/` so that an in-place rollback to
   the immediate previous release is local-only and fast.

7. **Nginx reload is batched per host.** Same host, multiple conf changes
   from the same release shard → buffer 30 seconds OR 50 domains, whichever
   comes first, then issue a single `nginx -s reload`. Emergency rollbacks
   skip the buffer.

8. **Alerts must deduplicate.** Same agent / same release / same severity
   → 1 alert per hour max. Batch multiple-target alerts into one message.

9. **`templates.runtime_fields` are immutable per version.** A `templates`
   row points to one or more `template_versions`. The `template_versions`
   row is immutable once published (`published_at IS NOT NULL`). Editing
   a template means publishing a new version. Releases pin to a specific
   `template_version_id`, never to a `template_id`.

10. **mTLS for agent traffic, JWT for management console.** The two
    auth schemes are separate; an agent certificate is NOT a JWT and cannot
    access `/api/v1/*` endpoints. A user JWT cannot access `/agent/v1/*`
    endpoints. The middleware stack enforces this separation.

11. **TimescaleDB is for `probe_results` only.** Business tables (projects,
    domains, releases, etc.) live in regular PostgreSQL tables. The
    `probe_results` hypertable has a 90-day retention policy and is
    excluded from nightly `pg_dump`.

12. **Pre-launch migration exception.** During Phase 1, the initial migration
    `000001_init.up.sql` may be edited in place. After Phase 1 cutover, this
    window closes permanently. (Carried over from ADR-0001/ADR-0002,
    reaffirmed by ADR-0003 D9.)

---

## Git Workflow

- `main` — always deployable, protected branch
- `feature/*` — feature branches, one per task
- `fix/*` — bugfix branches
- `release/*` — release preparation (if needed)
- PR required for all merges to main
- Squash merge preferred for clean history

---

## Makefile Commands

```makefile
make dev          # Start Docker Compose (PG + Redis + MinIO) + API server (air hot reload)
make build        # Build all Go binaries: server worker migrate agent
make agent        # Cross-compile agent for linux/amd64 (deployment artifact)
make test         # Run all unit tests
make lint         # golangci-lint + eslint
make migrate-up   # Run DB migrations
make migrate-down # Rollback last migration
make web          # Build Vue frontend
make clean        # Remove bin/
```

---

## Environment Variables

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_NAME=domain_platform
DB_USER=postgres
DB_PASSWORD=
DB_SSL_MODE=disable

# Redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=

# Object storage (MinIO/S3)
S3_ENDPOINT=http://localhost:9000
S3_REGION=us-east-1
S3_BUCKET=domain-platform-artifacts
S3_ACCESS_KEY=
S3_SECRET_KEY=
S3_USE_SSL=false

# Management console JWT
JWT_SECRET=
JWT_EXPIRY=24h

# Agent mTLS
AGENT_CA_CERT_PATH=/etc/domain-platform/ca.crt
AGENT_CA_KEY_PATH=/etc/domain-platform/ca.key
AGENT_CERT_VALIDITY=8760h     # 1 year per agent cert; rotate before expiry

# Notifications
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
WEBHOOK_URL=

# Provider configs (DNS only in Phase 1) loaded from configs/providers.yaml
```
