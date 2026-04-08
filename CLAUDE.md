# CLAUDE.md — Domain Lifecycle Management Platform

## Project Identity

- **Name**: Domain Lifecycle Management Platform (域名全生命週期管理平台)
- **Purpose**: Manage 12,000+ domains across 10 projects, ensuring continuous reachability from mainland China with <2min detection and <5min automated failover.
- **Architecture**: Go monolith (API + Worker + Scanner) with Vue 3 SPA frontend.

---

## Tech Stack (Non-Negotiable)

| Layer | Technology | Notes |
|-------|-----------|-------|
| Language | Go 1.22+ | All backend services |
| API Framework | Gin | RESTful JSON API |
| DB Access | sqlx | Raw SQL + struct scanning, NO ORM magic |
| Task Queue | asynq (hibiken/asynq) | Redis-backed async tasks |
| Template Engine | Go `text/template` | nginx conf rendering |
| Config | Viper | YAML + env vars |
| Logging | Zap (uber-go/zap) | Structured JSON logs |
| Auth | golang-jwt/jwt/v5 | JWT Bearer tokens |
| DB | PostgreSQL 16 + TimescaleDB | Single instance for business + time-series |
| Cache/Queue | Redis 7 | State dedup + asynq broker |
| Frontend | Vue 3 + Naive UI + TypeScript | SPA management console |
| Build | Vite | Frontend tooling |
| State | Pinia | Vue global state |
| Reverse Proxy | Caddy | Auto HTTPS + static files |

---

## Project Structure

```
domain-platform/
├── cmd/                      # Entry points (one main.go per binary)
│   ├── server/               # API + Web server
│   ├── worker/               # asynq task worker
│   ├── scanner/              # Probe scanner (deployed to CN nodes)
│   └── migrate/              # DB migration tool
├── internal/                 # Internal packages (NOT importable)
│   ├── domain/               # Domain business logic
│   ├── project/              # Project business logic
│   ├── release/              # Release subsystem
│   ├── probe/                # Probe monitoring logic
│   ├── alert/                # Alert & auto-disposition
│   ├── switcher/             # Domain auto-switch engine
│   └── pool/                 # Standby domain pool management
├── pkg/                      # Exportable packages
│   ├── provider/             # Vendor abstraction layer
│   │   ├── dns/              # DNS provider interface + implementations
│   │   └── cdn/              # CDN provider interface + implementations
│   ├── svnagent/             # SVN Agent client
│   ├── template/             # nginx conf template engine
│   └── notify/               # Notification (Telegram + Webhook)
├── api/                      # API definitions
│   ├── handler/              # Gin handlers
│   ├── middleware/            # Auth, RBAC, Logger, RateLimit
│   └── router/               # Route registration
├── store/                    # Data access layer
│   ├── postgres/             # PostgreSQL queries
│   ├── timescale/            # TimescaleDB probe data
│   └── redis/                # Redis operations
├── migrations/               # SQL migration files (sequential numbering)
├── templates/                # nginx conf template files (.tmpl)
├── deploy/                   # Deployment artifacts
│   ├── docker-compose.yml
│   ├── systemd/              # systemd service files
│   └── svn-agent/            # Python SVN Agent (target machines)
├── web/                      # Vue 3 frontend
│   ├── src/
│   │   ├── api/              # API client + TypeScript types
│   │   ├── views/            # Page components
│   │   ├── components/       # Reusable components
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

---

## Go Coding Standards

### Naming

- Package names: lowercase, single word, no underscores (`domain`, `provider`, `alert`)
- Files: `snake_case.go`
- Exported: `PascalCase` — Unexported: `camelCase`
- Interfaces: describe behavior, NOT prefixed with `I` (e.g., `DNSProvider` not `IDNSProvider`)
- Receiver names: 1-2 letter abbreviation of type (`func (s *Server) Start()`)

### Error Handling

```go
// ALWAYS check errors. NEVER use panic for business logic.
result, err := store.GetDomain(ctx, id)
if err != nil {
    return fmt.Errorf("get domain %d: %w", id, err)
}

// Use sentinel errors for expected conditions
var ErrDomainNotFound = errors.New("domain not found")
var ErrPoolExhausted  = errors.New("standby pool exhausted")

// Wrap with context at every layer boundary
if err != nil {
    return fmt.Errorf("release shard %d: %w", shardID, err)
}
```

### Structs & DTOs

```go
// Model — maps to DB row. Lives in store/ or internal/.
type MainDomain struct {
    ID        int64     `db:"id"`
    UUID      string    `db:"uuid"`
    Domain    string    `db:"domain"`
    ProjectID int64     `db:"project_id"`
    Status    string    `db:"status"`
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}

// Request DTO — API input. Lives in api/handler/.
type CreateDomainRequest struct {
    Domain    string   `json:"domain" binding:"required,fqdn"`
    ProjectID int64    `json:"project_id" binding:"required"`
    Prefixes  []string `json:"prefixes" binding:"required,min=1"`
}

// Response DTO — API output. Lives in api/handler/.
type DomainResponse struct {
    UUID       string    `json:"uuid"`
    Domain     string    `json:"domain"`
    Status     string    `json:"status"`
    Subdomains []SubdomainResponse `json:"subdomains,omitempty"`
}

// NEVER expose DB model directly in API response.
// ALWAYS convert Model → Response DTO explicitly.
```

### Interfaces & Dependency Injection

```go
// Define interfaces where they are USED, not where they are implemented.
// internal/domain/service.go
type DomainStore interface {
    Create(ctx context.Context, d *MainDomain) error
    GetByID(ctx context.Context, id int64) (*MainDomain, error)
    ListByProject(ctx context.Context, projectID int64, opts ListOpts) ([]MainDomain, error)
}

type Service struct {
    store    DomainStore
    dns      dns.Provider
    cdn      cdn.Provider
    tasks    *asynq.Client
    logger   *zap.Logger
}

func NewService(store DomainStore, dns dns.Provider, cdn cdn.Provider, tasks *asynq.Client, logger *zap.Logger) *Service {
    return &Service{store: store, dns: dns, cdn: cdn, tasks: tasks, logger: logger}
}
```

### Context & Timeouts

```go
// ALWAYS pass context. ALWAYS set timeouts for external calls.
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()

// DNS/CDN provider calls: 30s timeout
// SVN Agent calls: 120s timeout
// DB queries: 5s timeout (default via connection pool)
// Probe checks: 3s per target (DNS), 3s (TCP), 8s (HTTP)
```

### Database Queries (sqlx)

```go
// Use named queries for inserts/updates
const insertDomain = `
    INSERT INTO main_domains (uuid, domain, project_id, status, created_at, updated_at)
    VALUES (:uuid, :domain, :project_id, :status, NOW(), NOW())
    RETURNING id`

// Use ? placeholders rebound by sqlx for selects
const getDomainByID = `
    SELECT id, uuid, domain, project_id, status, created_at, updated_at
    FROM main_domains
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

func TestDomainService_Create(t *testing.T) {
    tests := []struct {
        name    string
        input   CreateDomainRequest
        wantErr bool
    }{
        {"valid domain", CreateDomainRequest{Domain: "example.com", ProjectID: 1, Prefixes: []string{"www"}}, false},
        {"empty domain", CreateDomainRequest{Domain: "", ProjectID: 1, Prefixes: []string{"www"}}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}

// Mock interfaces for unit tests — define mock in _test.go file
type mockDomainStore struct {
    createFn func(ctx context.Context, d *MainDomain) error
}
func (m *mockDomainStore) Create(ctx context.Context, d *MainDomain) error {
    return m.createFn(ctx, d)
}
```

### Logging

```go
// Use structured logging. NEVER use fmt.Println in production code.
logger.Info("domain created",
    zap.String("domain", domain.Domain),
    zap.Int64("project_id", domain.ProjectID),
    zap.String("status", domain.Status),
)

logger.Error("dns record creation failed",
    zap.String("domain", subdomain.FQDN),
    zap.String("provider", "cloudflare"),
    zap.Error(err),
)

// Log levels:
// Debug — development diagnostics
// Info  — normal operations (domain created, release started, probe completed)
// Warn  — recoverable anomalies (retry triggered, pool running low)
// Error — failures requiring attention (provider API error, deploy failed)
```

---

## Provider Abstraction Layer

This is the most critical architectural pattern in the system. ALL DNS and CDN operations go through these interfaces.

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
    Type    string  // A, CNAME, MX, TXT
    Name    string  // subdomain prefix
    Content string  // target (CNAME value, IP, etc.)
    TTL     int
    Proxied bool    // Cloudflare-specific, ignored by others
}

// pkg/provider/cdn/provider.go
package cdn

type Provider interface {
    Name() string
    AddDomain(ctx context.Context, domain string, config DomainConfig) error
    RemoveDomain(ctx context.Context, domain string) error
    GetDomainStatus(ctx context.Context, domain string) (*DomainStatus, error)
    PurgeCache(ctx context.Context, domain string, paths []string) error
    CloneConfig(ctx context.Context, src string, dst string) error
}
```

### Adding a New Provider

1. Create `pkg/provider/dns/cloudflare.go` (or cdn equivalent)
2. Implement the `Provider` interface
3. Register in `pkg/provider/dns/registry.go`: `Register("cloudflare", NewCloudflareProvider)`
4. Business logic uses `dns.GetProvider("cloudflare")` — ZERO changes to internal/ code

---

## API Conventions

### URL Structure

```
/api/v1/{resource}          # Collection
/api/v1/{resource}/:id      # Single resource
/api/v1/{resource}/:id/{action}  # Action on resource
```

### Response Format

```json
// Success
{
    "code": 0,
    "data": { ... },
    "message": "ok"
}

// Error
{
    "code": 40001,
    "data": null,
    "message": "domain not found"
}

// Paginated list
{
    "code": 0,
    "data": {
        "items": [...],
        "total": 1200,
        "cursor": "eyJpZCI6MTAwfQ=="
    },
    "message": "ok"
}
```

### HTTP Status Codes

- 200: Success (GET, PUT, PATCH)
- 201: Created (POST)
- 204: Deleted (DELETE)
- 400: Validation error
- 401: Not authenticated
- 403: Not authorized (RBAC)
- 404: Resource not found
- 409: Conflict (duplicate domain, etc.)
- 500: Internal server error (always log, never expose details)

---

## Frontend Conventions (Vue 3)

### Component Structure

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
const activeDomains = computed(() => domains.value.filter(d => d.status === 'active'))

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

<template>
  <!-- Template here -->
</template>
```

### API Client Pattern

```typescript
// web/src/api/domain.ts
import { http } from '@/utils/http'
import type { DomainResponse, CreateDomainRequest } from '@/types/domain'

export const domainApi = {
    list: (projectId: number, cursor?: string) =>
        http.get<PaginatedResponse<DomainResponse>>('/api/v1/domains', { params: { project_id: projectId, cursor } }),
    get: (id: string) =>
        http.get<DomainResponse>(`/api/v1/domains/${id}`),
    create: (data: CreateDomainRequest) =>
        http.post<DomainResponse>('/api/v1/domains', data),
    deploy: (id: string) =>
        http.post(`/api/v1/domains/${id}/deploy`),
}
```

### TypeScript Types

```typescript
// web/src/types/domain.ts
// Mirror Go DTOs exactly. Keep in sync.
export interface DomainResponse {
    uuid: string
    domain: string
    status: DomainStatus
    project_id: number
    subdomains?: SubdomainResponse[]
    created_at: string
    updated_at: string
}

export type DomainStatus =
    | 'inactive' | 'deploying' | 'active' | 'degraded'
    | 'switching' | 'suspended' | 'failed' | 'blocked' | 'retired'
```

---

## Database Migrations

```bash
# File naming: sequential, descriptive
migrations/
├── 000001_create_projects.up.sql
├── 000001_create_projects.down.sql
├── 000002_create_main_domains.up.sql
├── 000002_create_main_domains.down.sql
└── ...
```

### Migration Rules

1. Every UP migration MUST have a corresponding DOWN migration
2. NEVER modify an existing migration that has been applied
3. Destructive operations (DROP COLUMN, DROP TABLE) require explicit comment explaining why
4. Add indexes in the same migration as the table creation
5. Use `IF NOT EXISTS` / `IF EXISTS` for safety
6. All tables MUST include: `id BIGSERIAL PRIMARY KEY`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `deleted_at TIMESTAMPTZ`

---

## Task Queue Patterns (asynq)

```go
// Task type constants — one file: internal/tasks/types.go
const (
    TypeDNSCreateRecord  = "dns:create_record"
    TypeDNSDeleteRecord  = "dns:delete_record"
    TypeCDNAddDomain     = "cdn:add_domain"
    TypeCDNCloneConfig   = "cdn:clone_config"
    TypeTemplateRender   = "template:render_conf"
    TypeSVNCommit        = "svn:commit"
    TypeAgentDeploy      = "agent:deploy"
    TypeProbeVerify      = "probe:verify"
    TypeSwitchExecute    = "switch:execute"
    TypePoolWarmup       = "pool:warmup"
)

// Task creation — include all data needed for execution
payload, _ := json.Marshal(DNSCreatePayload{
    SubdomainID: sub.ID,
    Zone:        mainDomain.Domain,
    Record:      dns.Record{Type: "CNAME", Name: sub.Prefix, Content: cdnEndpoint},
    Provider:    sub.DNSProvider,
})
task := asynq.NewTask(TypeDNSCreateRecord, payload,
    asynq.MaxRetry(3),
    asynq.Timeout(30*time.Second),
    asynq.Queue("dns"),
)
```

---

## Domain State Machine

```
inactive ──→ deploying ──→ active ──→ degraded ──→ switching ──→ active
                │                                      │
                ▼                                      ▼
              failed                                 failed
                │
                ▼
            deploying (retry)

active ──→ suspended (manual) ──→ active (manual restore)
blocked ──→ retired (terminal)
```

**State transitions MUST be validated AND go through `Transition()`.** Never set status directly — always use the state machine via the single write path (see Critical Rule #8 and ADR-0002 D2):

```go
// internal/domain/statemachine.go
var validTransitions = map[string][]string{
    "inactive":  {"deploying"},
    "deploying": {"active", "failed"},
    "active":    {"degraded", "switching", "deploying", "suspended"},
    "degraded":  {"switching", "active"},
    "switching": {"active", "failed"},
    "suspended": {"active"},
    "failed":    {"deploying"},
    "blocked":   {"retired"},
}

func CanTransition(from, to string) bool {
    targets, ok := validTransitions[from]
    if !ok { return false }
    for _, t := range targets {
        if t == to { return true }
    }
    return false
}

// internal/domain/service.go — THE ONLY write path for main_domains.status
func (s *Service) Transition(
    ctx context.Context,
    id int64,
    from string,          // expected current status (optimistic check)
    to string,
    reason string,
    triggeredBy string,   // "user:{uuid}" | "system" | "probe:{node}" | "switcher" | "release:{uuid}"
) error {
    // Inside one transaction:
    //   1. SELECT status FROM main_domains WHERE id = $1 FOR UPDATE
    //   2. Assert current == from       → ErrStatusRaceCondition
    //   3. Assert CanTransition(from,to) → ErrInvalidTransition
    //   4. UPDATE main_domains SET status = $to, updated_at = NOW()
    //   5. INSERT domain_state_history
    //   6. COMMIT
}
```

**Enforcement**: `grep -r 'UPDATE main_domains SET status' --include='*.go'` must return exactly one hit — the query constant inside `store/postgres/domain.go::updateStatusTx`, which is only called by `Transition()`. Any PR that introduces a second hit must be rejected in review.

---

## Critical Business Rules

1. **Prefix determines everything.** A subdomain's DNS provider, CDN provider, nginx template, and HTML template are ALL derived from its prefix + project-level overrides.
2. **Blocking granularity = main domain.** When GFW blocks `example.com`, ALL subdomains are affected. Failover switches the entire main domain.
3. **Switch = full redeploy.** Switching to a backup domain means: new DNS records + new CDN config + re-rendered nginx conf + SVN commit + Agent deploy + probe verification.
4. **nginx reload must be batched.** Same server, multiple conf changes → aggregate into ONE reload (30s buffer or 50 domains, whichever comes first).
5. **Publish success ≠ final success.** SVN deploy success is not enough — probe verification from CN nodes must pass before marking `active`.
6. **Every conf publish must snapshot.** Save full conf to DB/filesystem BEFORE deployment. Rollback depends on this.
7. **Alerts must deduplicate.** Same domain, same status → 1 alert per hour max. Batch multiple domain alerts into one message.
8. **`main_domains.status` has ONE write path.** All status mutations go through `internal/domain.Service.Transition(ctx, id, from, to, reason, triggeredBy)`. NO package may `UPDATE main_domains SET status` directly — `internal/release`, `internal/switcher`, `internal/pool`, and `internal/domain/deployer.go` all route through `Transition()`. The method atomically `SELECT ... FOR UPDATE`, validates the transition, updates the row, and writes `domain_state_history`. See ADR-0002 D2.
9. **`prefix_rules` are soft-frozen after first use.** Once any `subdomains` row references a `prefix_rules` row, its runtime fields (`dns_provider`, `cdn_provider`, `nginx_template`, `html_template`) cannot be changed in isolation — any edit request must be accompanied by a `kind='rebuild'` release that re-renders + redeploys all affected subdomains through the standard canary pipeline. See ADR-0002 D3.
10. **Switch lock is Redis fast path + Postgres row lock.** `internal/switcher/service.go` acquires `switch:lock:{main_domain_id}` in Redis AND `SELECT ... FOR UPDATE` on the `main_domains` row. Redis loss must not enable double switching. See ADR-0002 D1.

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
make dev          # Start Docker Compose + API server (air hot reload)
make build        # Build all Go binaries
make test         # Run all unit tests
make lint         # golangci-lint + eslint
make migrate      # Run DB migrations
make migrate-down # Rollback last migration
make scanner      # Cross-compile scanner for linux/amd64
make web          # Build Vue frontend
make deploy-prod  # scp binaries + restart systemd services
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

# JWT
JWT_SECRET=
JWT_EXPIRY=24h

# Telegram
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=

# Webhook
WEBHOOK_URL=

# Providers (loaded per-provider via Viper config file)
# See configs/providers.yaml
```
