# CLAUDE_CODE_INSTRUCTIONS.md — Master Guide for Claude Code

> **This file is the entry point.** Read this first, then consult the referenced documents as needed.

---

## What You're Building

A **Domain Lifecycle Management Platform** that manages 12,000+ domains across 10 projects, monitors their reachability from mainland China, and automatically fails over to backup domains when GFW blocks them.

**This is an enterprise-grade SaaS application.** Every feature must be production-ready: proper error handling, audit logging, input validation, graceful degradation, and comprehensive tests.

---

## Document Map

Read these documents in this order when starting a new task:

| Document | When to Read | Content |
|----------|-------------|---------|
| **CLAUDE.md** | Always (should be auto-loaded) | Tech stack, project structure, coding standards, all conventions |
| **ARCHITECTURE.md** | When working on cross-cutting features or system design | Subsystem details, data flow, deployment topology |
| **DATABASE_SCHEMA.md** | When creating migrations or writing queries | Complete schema, index strategy, conventions |
| **DEVELOPMENT_PLAYBOOK.md** | When implementing any feature | Step-by-step patterns for adding APIs, providers, tasks, pages |
| **TESTING.md** | When writing or modifying tests | Test patterns, mock strategies, coverage requirements |
| **docs/adr/0001-...** | Before touching schema, probe, pool, release, or auth | 13 decisions that revised the original architecture on 2026-04-08 |
| **docs/adr/0002-...** | Before touching switcher, state machine, prefix_rules, CDN providers, worker queues | 6 pre-implementation decisions added on 2026-04-08: switch-lock fallback, status single write path, prefix_rules rebuild flow, CDN idempotency, queue priorities, pool lifecycle |

> **When an ADR conflicts with an older line in this document, the ADR wins.**
> The ADRs are the most recent source of truth and supersede any stale
> description here.

---

## 優先順序總覽 (Priority Overview)

> 快速確認當前應該做什麼。詳細規格見各 Phase 章節。

### 依賴關係圖

```
[Migration 000001]
       │
       ├──→ [Shared Response / Middleware / Auth]
       │              │
       │    ┌─────────┴──────────┐
       │    ▼                    ▼
       │ [Project CRUD]    [Prefix Rules]  ← 必須先於 Domain
       │         └──────────────┘
       │                  │
       │                  ▼
       │           [Domain CRUD + State Machine]
       │                  │
       │    ┌─────────────┼─────────────┐
       │    ▼             ▼             ▼
       │ [Server]  [DNS Provider]  [CDN Provider]
       │                  └──────┬──────┘
       │                         ▼
       │              [Template Engine]
       │                         │
       │                         ▼
       │              [asynq Task Queue]
       │                         │
       │         ┌───────────────┼───────────────┐
       │         ▼               ▼               ▼
       │   [SVN Agent]    [Deploy Pipeline]  [Release]
       │                         │
       │              ┌──────────┴──────────┐
       │              ▼                     ▼
       │        [Scanner / Probe]     [Reload Buffer]
       │              │
       │     ┌────────┴────────┐
       │     ▼                 ▼
       │ [Alert Engine]  [Auto-Switch]
       │
       └──→ [Vue 3 Frontend]  ← 最後，依賴穩定 API
```

---

### 整體開發序列（6 週）

| 週次 | Phase | 核心交付物 | 完成標準 |
|------|-------|-----------|---------|
| Week 1 | Foundation (上) | docker-compose、migration、response/middleware/auth、Project CRUD | `go test ./...` 全綠 |
| Week 2 | Foundation (下) | Prefix Rules、Domain State Machine、Domain CRUD、Server CRUD | 可透過 API 完整 CRUD |
| Week 3 | Provider + Template | DNS/CDN provider (CF+Ali)、Template Engine、asynq 基礎、DNS/CDN tasks | provider 單元測試通過 |
| Week 4 | Deploy Pipeline | SVN Agent、svnagent client、Single Domain Deploy、Reload Buffer、Release Shard | 單一 domain 能走完 deploy → verify |
| Week 5 | Probe + Alert | Scanner L1/L2、mTLS、probe push API、Alert Engine、Auto-Switch | 模擬封鎖能在 5min 內完成切換 |
| Week 6 | Frontend | Vue 3 初始化、Login、Dashboard、Domain list/detail、Release 管理 | 全功能可操作 |

---

### 分項優先度清單

#### P0 — 核心骨架（Week 1–2，阻塞後續一切）

```
□ deploy/docker-compose.yml                   基礎設施
□ migrations/000001_init.up.sql + down.sql    全部表格一次建好
□ cmd/migrate/main.go                         up/down/version/force
□ api/handler/response.go                     統一 Response 格式（建好後不能改）
□ api/middleware/keys.go                      Context key 常數
□ api/middleware/auth.go                      JWT 驗證
□ api/middleware/rbac.go                      角色權限表（hardcoded）
□ api/middleware/logger.go                    結構化請求 log
□ internal/auth/service.go + store            Login、bcrypt
□ internal/project/service.go + store         Project CRUD
□ store/postgres/project.go                   SQL queries
□ api/handler/project.go + router             5 個端點 + RBAC
□ internal/project/prefix.go                  PrefixRule + ResolvePrefix（兩層覆寫）
□ store/postgres/prefix.go
□ internal/domain/statemachine.go             CanTransition + MustTransition + full test
□ internal/domain/service.go + model          Create（含 prefix 解析）、Get、List、UpdateStatus
□ store/postgres/domain.go + subdomain.go     cursor-based List
□ api/handler/domain.go + router
□ internal/server/service.go + store
□ api/handler/server.go + router
```

#### P0 — Provider 抽象層（Week 3，Deploy Pipeline 的前提）

```
□ pkg/provider/dns/provider.go                Provider 介面 + Record + RecordFilter
□ pkg/provider/dns/registry.go                Register / GetProvider（thread-safe）
□ pkg/provider/dns/cloudflare.go              Cloudflare 實作（含 rate limit retry）
□ pkg/provider/dns/aliyun.go                  Alibaba Cloud 實作
□ pkg/provider/cdn/provider.go                Provider 介面 + DomainConfig + DomainStatus
□ pkg/provider/cdn/registry.go
□ pkg/provider/cdn/cloudflare.go              含 CloneConfig（auto-switch 關鍵）
□ pkg/provider/cdn/aliyun.go
□ pkg/template/engine.go                      Render + RenderAll + checksum
□ templates/                                  至少一個 .conf.tmpl 範本
□ internal/tasks/types.go                     全部 task type 常數
□ cmd/worker/main.go                          asynq Server + 所有 task 注冊
□ 各 task handler                             DNS/CDN create/delete/clone tasks
```

#### P1 — 部署管線（Week 4）

```
□ deploy/svn-agent/agent.py                   /deploy、/reload、/ping
□ pkg/svnagent/client.go                      Deploy、Reload、Ping（120s timeout）
□ internal/domain/deployer.go                 7 步驟 orchestrator + rollback 邏輯
□ internal/release/reload_buffer.go           30s buffer、50 domains cap、Emergency flush
□ internal/release/service.go                 Shard 執行、canary 判斷、pause/resume/rollback
□ api/handler/release.go + router             CRUD + 操作端點
```

#### P1 — 探針與告警（Week 5）

```
□ cmd/scanner/main.go                         L1 500 goroutines / 60s，L2 5min（ADR-0001 D5）
□ internal/probe/poison.go                    GFW 毒化 IP 清單（可 API 更新）
□ api/handler/probe.go                        POST /api/v1/probe/push（mTLS）
□ store/timescale/probe.go                    批次寫入 probe_results
□ store/redis/probe.go                        state dedup key 操作
□ internal/alert/engine.go                    嚴重度判斷、去重、批次聚合
□ pkg/notify/telegram.go                      Telegram Bot 通知
□ pkg/notify/webhook.go                       Webhook 通知
□ internal/switcher/service.go                Redis + PG 雙鎖 + 9 步驟切換流程（ADR-0002 D1）
□ internal/pool/service.go                    備用域名管理、預熱流程
```

#### P2 — 前端（Week 6）

```
□ web/ npm 初始化（Vite + Vue3 + Naive UI + Pinia + TS）
□ web/src/utils/http.ts                       Axios instance + 攔截器
□ web/src/router/index.ts                     全部路由（history mode）
□ web/src/views/LoginView.vue
□ web/src/views/DashboardView.vue
□ web/src/views/domains/DomainList.vue
□ web/src/views/domains/DomainDetail.vue
□ web/src/views/projects/ProjectList.vue
□ web/src/views/releases/ReleaseList.vue
□ web/src/views/AlertList.vue
□ web/src/views/pool/PoolList.vue
□ web/src/views/servers/ServerList.vue
□ web/src/views/settings/UserList.vue
```

#### P2 — Provider 擴充（Week 3–4 空檔補充）

```
□ pkg/provider/dns/tencent.go
□ pkg/provider/dns/godaddy.go
□ pkg/provider/cdn/tencent.go
□ pkg/provider/cdn/huawei.go
```

---

### Provider 實作優先度

| 優先 | DNS | CDN |
|------|-----|-----|
| P0（Week 3 必須） | Cloudflare、Alibaba Cloud | Cloudflare、Alibaba Cloud |
| P1（Week 4 補充） | Tencent Cloud、GoDaddy | Tencent Cloud |
| P2（後期） | — | Huawei Cloud、Self-hosted |

---

### 關鍵依賴警告

> 以下項目若順序錯誤，會導致大幅重構：

1. **`response.go` 必須在所有 handler 之前定型** — Response 結構改變等於所有 handler 全改
2. **`prefix_rules` 必須在 `main_domains` 之前** — Create Domain 依賴 ResolvePrefix
3. **`statemachine.go` 必須在 `domain service` 之前** — UpdateStatus 依賴狀態機
4. **`conf_snapshots` 必須在 Deploy Orchestrator 之前** — Rollback 機制依賴它
5. **DNS/CDN Provider 介面一旦定型不能隨意加方法** — 所有實作都要改
6. **`internal/tasks/types.go` 必須在任何 task 之前** — Task type 字串一改，asynq queue 歷史任務全部無法處理

---

## Critical Rules (Never Violate)

### Architecture Rules

1. **Layer discipline**: handler → service → store. Never skip layers. Never import upward.
2. **Provider abstraction**: ALL DNS/CDN operations go through `pkg/provider/` interfaces. Business logic in `internal/` NEVER imports vendor SDKs directly.
3. **State machine single write path** (ADR-0002 D2): Domain status transitions MUST go through `internal/domain.Service.Transition(ctx, id, from, to, reason, triggeredBy)`. NO package may `UPDATE main_domains SET status` directly. `grep -r 'UPDATE main_domains SET status' --include='*.go'` must return exactly one hit — the query constant inside `store/postgres/domain.go::updateStatusTx`, called only by `Transition()`.
4. **Snapshot before deploy**: Every nginx conf change MUST save a snapshot to `conf_snapshots` BEFORE deployment. Rollback depends on this.
5. **Audit everything**: Every write operation (create, update, delete, deploy, switch, rollback) MUST write an audit log entry.

### Architectural Rules added by ADR-0002

18. **Switch lock** (D1): Redis `switch:lock:{main_domain_id}` (fast path) + Postgres `SELECT ... FOR UPDATE` on `main_domains` (authoritative). Redis loss MUST NOT enable double switching. See ARCHITECTURE.md §2.5 step 0.
19. **Prefix rules soft-frozen** (D3): After a `prefix_rules` row has been referenced by any `subdomains` row, editing its runtime fields requires a `kind='rebuild'` release. See DEVELOPMENT_PLAYBOOK.md §7.
20. **CDN CloneConfig idempotency** (D4): Every CDN provider's `CloneConfig` must be idempotent. Unit test `TestCloneConfig_Idempotent` is mandatory. See ARCHITECTURE.md §2.2.
21. **asynq queue layout is canonical** (D5): `critical / dns / cdn / deploy / default` with the priority and concurrency in ARCHITECTURE.md §2.3. `cmd/worker/main.go` is the only place this is configured.
22. **Pool lifecycle completeness** (D6): After `promoted`, allowed transitions are `blocked → retired` (terminal, burn the domain) or `blocked → pending` (operator un-block, re-warm). See ARCHITECTURE.md §2.6.

### Code Quality Rules

6. **Error wrapping**: Always wrap errors with context: `fmt.Errorf("operation %s: %w", id, err)`. NEVER return bare errors.
7. **Context propagation**: Every function that does I/O takes `context.Context` as first parameter. NEVER use `context.Background()` in business logic.
8. **No global state**: All dependencies injected through constructors. No `init()` functions for business logic. No package-level mutable variables (except provider registry).
9. **No ORM magic**: Use `sqlx` with explicit SQL. Every query is visible, reviewable, and optimizable.
10. **Timeout everything**: Every external call (DNS API, CDN API, SVN Agent, DB query) has an explicit timeout via context.

### Security Rules

11. **Never expose internal IDs**: API responses use UUID, never BIGSERIAL id.
12. **Never log secrets**: API keys, passwords, JWT tokens must NEVER appear in logs.
13. **Validate all input**: Use Gin binding tags for request validation. Additional business validation in service layer.
14. **RBAC on every endpoint**: Every API handler must have role middleware. No unprotected write endpoints.

### Frontend Rules

15. **TypeScript strict mode**: No `any` types except in third-party library wrappers.
16. **API types mirror Go DTOs**: Keep `web/src/types/` in sync with Go response structs.
17. **Error handling in every API call**: Every `await` in a component must have try/catch with user-facing error message.

---

## Implementation Priorities (Per Feature)

When building any feature, always implement in this exact order:

```
1. Database migration  (schema first — defines the contract)
2. Store layer         (data access, SQL queries)
3. Service layer       (business logic + state machine)
4. asynq task          (if the operation is async)
5. API handler         (parse → call service → format response)
6. Route registration  (with correct RBAC middleware)
7. Unit tests          (service layer first, then handler)
8. Vue frontend page   (last — depends on stable API)
```

---

## Key Architectural Decisions (Never Revisit Without Discussion)

| # | Decision | Choice | Reason |
|---|----------|--------|--------|
| 1 | Pagination strategy | Cursor-based (by id, base64 encoded) | 12K+ domains, OFFSET degrades at scale |
| 2 | Enum storage | VARCHAR + CHECK constraint | Avoids ALTER TYPE migration pain |
| 3 | External ID | UUID only in API responses, never BIGSERIAL | Security, prevents enumeration |
| 4 | Audit log location | Service layer, NOT middleware | Middleware doesn't know target UUID |
| 5 | Provider rate limit | Handled inside provider implementation | Business logic must not sense vendor quirks |
| 6 | nginx reload | 30s aggregation buffer / max 50 domains | Batch efficiency, avoid reload storms |
| 7 | Switch concurrency | Redis fast path + Postgres `SELECT … FOR UPDATE` fallback (ADR-0002 D1) | Prevent double-switch even under Redis loss |
| 8 | Probe auth | mTLS (client cert per probe node) | Scanner is in mainland China, needs mutual auth |
| 9 | Conf rollback | Snapshot to conf_snapshots BEFORE deploy | Rollback requires known-good snapshot |
| 10 | Vue routing | History mode | Caddy must have try_files fallback rule |

---

## Phase 1 — Foundation (Week 1–2)

### 1.1 Infrastructure: `deploy/docker-compose.yml`

```
Services:
  postgres:  timescale/timescaledb:latest-pg16  → port 5432
  redis:     redis:7-alpine                     → port 6379
  caddy:     caddy:2-alpine                     → port 80 / 443 (prod only)
```

Notes:
- TimescaleDB extension enabled in migration via `CREATE EXTENSION IF NOT EXISTS timescaledb`
- Local dev connects directly to `:8080`, no Caddy needed

### 1.2 Migration 000001

File: `migrations/000001_init.up.sql` + `000001_init.down.sql`
Full schema defined in DATABASE_SCHEMA.md. Critical details:

| Detail | Rule |
|--------|------|
| Enums as VARCHAR + CHECK | Never use PostgreSQL ENUM type |
| `prefix_rules.project_id` nullable | NULL = system-wide default; needs partial unique index separately |
| `domain_tasks.status` doubles as step name | Values: dns / cdn / render / svn / deploy / verify / completed / failed / rolled_back |
| `probe_results` hypertable | `create_hypertable()` must be called after TimescaleDB extension is enabled |

Migration tool: `golang-migrate/migrate/v4` wrapped in `cmd/migrate/main.go` with sub-commands: `up`, `down`, `version`, `force`.

### 1.3 Shared Base Packages (Build First — Everything Depends on These)

**`api/handler/response.go`** — Unified response format

```go
type Response struct {
    Code    int    `json:"code"`
    Data    any    `json:"data"`
    Message string `json:"message"`
}

type PaginatedData[T any] struct {
    Items  []T    `json:"items"`
    Total  int64  `json:"total"`
    Cursor string `json:"cursor,omitempty"`
}

// Helpers (all handlers use these — never construct Response manually):
func OK(c *gin.Context, data any)
func Created(c *gin.Context, data any)
func NoContent(c *gin.Context)
func BadRequest(c *gin.Context, code int, msg string)
func NotFound(c *gin.Context, msg string)
func Conflict(c *gin.Context, msg string)
func InternalError(c *gin.Context, logger *zap.Logger, err error)
// InternalError: log full error, return fixed "internal error" — never expose err to client
```

### 1.4 Middleware Stack

**`api/middleware/auth.go`**
- Validate JWT Bearer token
- Inject userID, role, clientIP into gin.Context
- Key constants live in `api/middleware/keys.go`:
  ```go
  const (
      ContextKeyUserID = "user_id"
      ContextKeyRole   = "role"
      ContextKeyIP     = "client_ip"
  )
  ```

**`api/middleware/rbac.go`**
- Roles are linear: `viewer < operator < release_manager < admin`
- `auditor` is a special non-linear role (read-only + audit log access)
- Role table is hardcoded — NOT stored in DB
- `RequireRole(minRole string) gin.HandlerFunc` → 403 if insufficient

**`api/middleware/audit.go`**
- Does NOT write audit log (middleware has no target UUID)
- Only injects userID + IP into context
- Actual audit writes happen in the service layer

**`api/middleware/logger.go`**
- Zap structured request log: method, path, status, latency, user_id, request_id
- Never log request body (may contain credentials)

### 1.5 Auth

```
internal/auth/
  service.go     Login, ChangePassword
  model.go       User struct
store/postgres/
  user.go        GetByUsername, GetByID, Create, UpdateLastLogin
api/handler/
  auth.go        POST /api/v1/auth/login
```

Rules:
- Password: bcrypt cost=12
- JWT payload: `user_id`, `role`, `exp`, `iat`
- Login failure: always return "invalid credentials" — never distinguish username vs password (login identifier = `username`, NOT email, per ADR-0001)
- No refresh token (24h expiry is sufficient for this use case)

### 1.6 Project CRUD

```
internal/project/
  model.go        Project struct
  service.go      Create, List, Get, Update, Delete
  service_test.go
store/postgres/
  project.go      CRUD queries
api/handler/
  project.go      5 endpoints
```

API endpoints + RBAC:
```
GET    /api/v1/projects         → viewer
POST   /api/v1/projects         → admin
GET    /api/v1/projects/:id     → viewer
PUT    /api/v1/projects/:id     → admin
DELETE /api/v1/projects/:id     → admin  (soft delete)
```

Delete rule: reject with 409 if project has any domain with status NOT IN ('inactive', 'failed', 'blocked', 'retired').

### 1.7 Prefix Rules (Build Before Domain CRUD)

Prefix determines everything — must exist before any domain can be registered.

```
internal/project/prefix.go     PrefixRule struct + service methods
store/postgres/prefix.go        Queries

Two-level resolution (project overrides global):
  1. SELECT WHERE project_id = ? AND prefix = ?
  2. If not found: SELECT WHERE project_id IS NULL AND prefix = ?
  3. Both empty → ErrPrefixNotFound

service.ResolvePrefix(ctx, projectID int64, prefix string) (*PrefixRule, error)
```

API endpoints:
```
GET    /api/v1/prefix-rules                              → viewer  (global defaults)
GET    /api/v1/projects/:id/prefix-rules                 → viewer
POST   /api/v1/projects/:id/prefix-rules                 → admin
PUT    /api/v1/projects/:id/prefix-rules/:prefix         → admin
DELETE /api/v1/projects/:id/prefix-rules/:prefix         → admin
```

### 1.8 Domain State Machine

File: `internal/domain/statemachine.go`

```go
var validTransitions = map[string][]string{
    "inactive":  {"deploying"},
    "deploying": {"active", "failed"},
    "active":    {"degraded", "switching", "deploying", "suspended"},
    "degraded":  {"switching", "active"},
    "switching": {"active", "failed"},
    "suspended": {"active"},
    "failed":    {"deploying"},
    "blocked":   {"retired"},
    // "retired" is terminal — no outbound transitions
}

func CanTransition(from, to string) bool
func MustTransition(from, to string) error  // wraps CanTransition, returns typed error
```

`domain_state_history` written on every `UpdateStatus()` call.
`triggered_by` format:
- User action: `"user:{uuid}"`
- System automation: `"system"`
- Probe triggered: `"probe:{node_name}"`

### 1.9 Domain CRUD

```
internal/domain/
  model.go         MainDomain, Subdomain structs
  service.go       Create, Get, List, Delete, UpdateStatus
  service_test.go
store/postgres/
  domain.go        CRUD + ListByProject (cursor-based)
  subdomain.go     CreateBatch, ListByDomain, GetByFQDN
api/handler/
  domain.go
```

Create Domain — exact sequence:
```
1. Validate domain format (FQDN)
2. Check domain uniqueness (WHERE deleted_at IS NULL)
3. Validate all requested prefixes resolve in this project
4. BEGIN TX
5. INSERT main_domains (status='inactive')
6. For each prefix: ResolvePrefix → INSERT subdomains
7. INSERT audit_logs
8. COMMIT
```

List API (cursor-based):
```
GET /api/v1/domains?project_id=1&status=active&cursor=xxx&limit=50
cursor = base64(last_id)
```

Delete: soft delete only. Allowed only when status IN ('inactive', 'failed').

### 1.10 Server CRUD

```
internal/server/
  model.go    Server struct
  service.go  Create, List, Get, Update, Delete, Ping
store/postgres/
  server.go

API endpoints + RBAC:
GET    /api/v1/servers           → viewer
POST   /api/v1/servers           → admin
GET    /api/v1/servers/:id       → viewer
PUT    /api/v1/servers/:id       → admin
DELETE /api/v1/servers/:id       → admin
POST   /api/v1/servers/:id/ping  → operator  (tests agent connectivity)
```

---

## Phase 2 — Provider Layer + Templates (Week 3)

### 2.1 DNS Provider Interface

File: `pkg/provider/dns/provider.go`

```go
type Provider interface {
    Name() string
    CreateRecord(ctx context.Context, zone string, record Record) (*Record, error)
    DeleteRecord(ctx context.Context, zone string, recordID string) error
    ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error)
    UpdateRecord(ctx context.Context, zone string, recordID string, record Record) (*Record, error)
}

type Record struct {
    ID      string
    Type    string   // "CNAME", "A", "TXT"
    Name    string   // subdomain prefix only, no zone
    Content string
    TTL     int
    Proxied bool     // Cloudflare-specific, ignored by other providers
}
```

Registry (`pkg/provider/dns/registry.go`):
- `init()` only calls `Register()` — never creates clients
- Clients instantiated lazily in `GetProvider()` based on cfg
- Thread-safe with `sync.RWMutex`

Every provider implementation MUST:
- Handle rate limit (HTTP 429) with exponential backoff, max 3 retries
- Map vendor error codes to unified sentinel errors:
  `ErrProviderRateLimit`, `ErrRecordNotFound`, `ErrZoneNotFound`
- Set 30s context timeout on every API call

Implementation order: Cloudflare → Alibaba Cloud → Tencent Cloud → GoDaddy

### 2.2 CDN Provider Interface

File: `pkg/provider/cdn/provider.go`

```go
type Provider interface {
    Name() string
    AddDomain(ctx context.Context, domain string, config DomainConfig) error
    RemoveDomain(ctx context.Context, domain string) error
    GetDomainStatus(ctx context.Context, domain string) (*DomainStatus, error)
    PurgeCache(ctx context.Context, domain string, paths []string) error
    CloneConfig(ctx context.Context, src string, dst string) error
}
// CloneConfig: copies ALL CDN config from src domain to dst domain.
// This is the most critical operation in auto-switch.

type DomainStatus struct {
    Active    bool
    SSLExpiry *time.Time
    CNAME     string  // CDN-assigned CNAME target
}
```

Implementation order: Cloudflare → Alibaba Cloud → Tencent Cloud → Huawei Cloud

### 2.3 nginx Template Engine

File: `pkg/template/engine.go`

```go
type Engine struct { dir string }

type ConfData struct {
    MainDomain string
    Subdomains []SubdomainConf
}

type SubdomainConf struct {
    FQDN         string
    Prefix       string
    Upstream     string
    TemplateName string
}

func (e *Engine) Render(templateName string, data ConfData) (string, error)
func (e *Engine) RenderAll(data ConfData) (map[string]string, error)
// RenderAll: renders each subdomain with its own template.
// Returns map[templateName]renderedContent.
// Fail-fast: if any template fails, abort all.
```

After rendering: compute SHA256 checksum and save to `conf_snapshots` BEFORE any deployment.
Templates stored in: `templates/*.conf.tmpl`

### 2.4 asynq Task Architecture

File: `internal/tasks/types.go` — all task type constants:

```go
const (
    TypeDNSCreateRecord = "dns:create_record"
    TypeDNSDeleteRecord = "dns:delete_record"
    TypeCDNAddDomain    = "cdn:add_domain"
    TypeCDNRemoveDomain = "cdn:remove_domain"
    TypeCDNCloneConfig  = "cdn:clone_config"
    TypeTemplateRender  = "template:render"
    TypeSVNCommit       = "svn:commit"
    TypeAgentDeploy     = "agent:deploy"
    TypeProbeVerify     = "probe:verify"
    TypeSwitchExecute   = "switch:execute"
    TypePoolWarmup      = "pool:warmup"
    TypeNginxReload     = "nginx:reload"
)
```

Queue configuration (canonical layout in ARCHITECTURE.md §2.2 per ADR-0002 D5 — this table is a mirror; if they ever diverge, ARCHITECTURE.md wins):

| Queue | Tasks | Priority weight | Concurrency |
|-------|-------|-----------------|-------------|
| `critical` | `switch:execute`, `probe:verify` | 10 | 20 |
| `dns` | `dns:*` | 6 | 10 |
| `cdn` | `cdn:*` | 6 | 10 |
| `deploy` | `svn:*`, `agent:*`, `nginx:*` | 4 | 5 (serial per server) |
| `default` | `template:*`, `pool:*` | 2 | 10 |

`strict: false` — weighted priority, not strict priority. `cmd/worker/main.go::asynq.Config.Queues` is the only place this layout is configured.

Rule: every task Payload struct MUST include `DomainTaskID int64` for progress tracking to DB.

---

## Phase 3 — Deployment Pipeline (Week 4)

### 3.1 SVN Agent

File: `deploy/svn-agent/agent.py` (~100 lines, Python)

```
Endpoints:
  POST /deploy   body: {path: string, content: string, reload: bool}
    → write file to disk
    → svn add (if new file)
    → svn commit
    → if reload=true: nginx -t && nginx -s reload
    → return {ok: bool, error: string}

  POST /reload   → nginx -t && nginx -s reload
  GET  /ping     → return {ok: true}

Authentication: Bearer token (static, set via env var AGENT_TOKEN)
```

File: `pkg/svnagent/client.go`

```go
type Client struct {
    baseURL string
    token   string
    http    *http.Client  // timeout: 120s
}

func (c *Client) Deploy(ctx context.Context, req DeployRequest) error
func (c *Client) Reload(ctx context.Context) error
func (c *Client) Ping(ctx context.Context) error
```

### 3.2 Single Domain Deploy Orchestrator

File: `internal/domain/deployer.go`

This is the most complex business flow. Every step has rollback logic.

```
Deploy(ctx, mainDomainID int64) error

Step 1 — DNS
  For each subdomain: enqueue TypeDNSCreateRecord
  Poll domain_tasks until all complete (interval 5s, timeout 5min)
  Any failure → rollback: delete created DNS records → return error

Step 2 — CDN
  For each subdomain: enqueue TypeCDNAddDomain
  Poll until complete
  Failure → rollback CDN + DNS

Step 3 — Render
  Call template.Engine.RenderAll()
  Save conf_snapshots (one per subdomain, SHA256 checksum)
  Failure → rollback CDN + DNS (conf never deployed, no conf rollback needed)

Step 4 — SVN Commit
  Call svnagent.Deploy(reload=false)
  Failure → rollback CDN + DNS (conf_snapshot rows kept for audit, mark as rolled_back)

Step 5 — nginx Reload
  Enqueue TypeNginxReload (enters 30s aggregation buffer)
  Wait for reload confirmation

Step 6 — Probe Verify
  Enqueue TypeProbeVerify
  Wait up to 2 minutes
  Failure → status = "failed", NO auto-rollback (requires human decision)

Step 7 — Finalize
  UpdateStatus("inactive" → "active") via state machine
  Write domain_state_history (triggered_by="system")
  Write audit_log
```

Progress tracking: every step updates `domain_tasks.step` so the UI can show live progress.

### 3.3 nginx Reload Aggregation Buffer

File: `internal/release/reload_buffer.go`

```go
type ReloadBuffer struct {
    mu      sync.Mutex
    pending map[int64][]int64    // serverID → []domainTaskIDs
    timers  map[int64]*time.Timer
}

const (
    reloadBufferTimeout = 30 * time.Second
    reloadBufferMaxSize = 50
)

// AddToBuffer: add domainTaskID. Triggers flush if size >= 50 or timer fires.
// Flush: runs nginx -t first. If test fails → rollback ALL conf changes in the batch.
// Emergency: bypass buffer, flush immediately. Used by P1 auto-switch.
```

---

## Phase 4 — Probe Monitoring (Week 5)

### 4.1 Scanner Architecture

File: `cmd/scanner/main.go`

```
L1 Scan (every 60s) — ADR-0001 D5:
  Source:      GET /api/v1/probe/domains  (returns all active/degraded domains)
  Workers:     500 goroutines
  Per domain:  DNS query (3s timeout) + TCP :443 connect (3s timeout)
  Batch push:  POST /api/v1/probe/push, 100 results per batch
  Capacity:    12K domains × 3 nodes ÷ 60s ≈ 200 checks/s/node (see ARCHITECTURE.md §2.4)
  Fallback:    if 1C/1G probe box CPU > 80% sustained in load test,
               fall back to 90s + renegotiate SLA to < 3 min BEFORE cutover.

L2 Scan (every 5min):
  Source:      L1-passed domains only
  Sample:      1–2 random subdomains per domain
  Per domain:  HTTP GET (8s timeout), check status code + latency

L3 Scan (every 30s) — tagged core domains only:
  HTTP + keyword check + TLS handshake + cert expiry + content hash diff

Alerting paths (L1, ADR-0001 D5):
  - Fast path: single cycle, all active nodes report same non-ok status → P1
  - Confirmation path: majority of nodes, 2 consecutive cycles → P1
    (Phase 1 majority = 2 of 3 nodes; Phase 2 majority = 4 of 6 nodes)

GFW poison IP list (hardcoded, updatable via API):
  127.0.0.1, 243.185.187.39, 74.125.127.102, ...
```

mTLS configuration (read from env at startup):
```
PROBE_CLIENT_CERT  = /etc/domain-scanner/client.crt
PROBE_CLIENT_KEY   = /etc/domain-scanner/client.key
PROBE_CA_CERT      = /etc/domain-scanner/ca.crt
PROBE_NODE_NAME    = cn-probe-ct    ← unique identifier per node
```

Block detection logic:
```
DNS poisoning:   resolved IP ∈ known GFW poison IP list
TCP block:       connect() timeout after 3s
SNI block:       TCP connects but TLS handshake fails
HTTP hijack:     response contains block keywords OR unexpected redirect
Content tamper:  response body checksum mismatch (L3 only)
```

### 4.2 Alert Engine

File: `internal/alert/engine.go`

```
On each probe result batch:
  1. Read current state from Redis: domain:status:{node}:{domain}
  2. If state changed → evaluate alert
  3. Update Redis key (TTL 3600s)

Alert dedup (Redis):
  Key: alert:dedup:{node}:{domain}:{status}  TTL=3600s
  Exists → skip (same status will not re-alert within 1 hour)
  Not exists → send alert + set key

Severity mapping:
  P1 → dns_ok=false (DNS poisoning)
  P1 → tcp_latency_ms IS NULL (TCP block)
  P1 → http_hijacked=true
  P2 → L2 fails but L1 passes (CDN / app layer issue)
  P3 → tcp_latency_ms > 3000ms
  INFO → domain recovers from blocked/degraded to ok

Batch alert rule:
  Multiple domains in same project trigger alerts within 5s
  → Aggregate into one Telegram message listing all affected domains
```

### 4.3 Auto-Switch Flow

File: `internal/switcher/service.go`

```
TriggerSwitch(ctx, mainDomainID int64, reason string) error

0. Acquire switch lock (ADR-0002 D1):
   a. Redis fast path: SETNX switch:lock:{mainDomainID} <workerID> EX 600
      → Already locked → return ErrSwitchInProgress
      → Redis unreachable → log warning, skip to (b)
   b. Postgres row lock (authoritative):
      BEGIN; SELECT id, status FROM main_domains WHERE id = $1 FOR UPDATE;
      → PG unreachable → ABORT, no fallback
   Hold BOTH locks for the full switch cycle (≤ 600s TTL).

1. Select ready domain from pool:
   SELECT ... WHERE project_id=? AND status='ready'
   ORDER BY priority DESC LIMIT 1 FOR UPDATE
   → None available → P0 alert + return ErrPoolExhausted

2. Send P1 alert (Telegram + Webhook)

3. Pause all pending/running releases for this project (set release:pause:{project_id})

4. INSERT switch_history (status='in_progress')

5. Transition old main_domain: 'active' → 'switching' via domain.Service.Transition()

6. Execute switch steps (each step has rollback on failure):
   a. CDN CloneConfig:  old_domain → new_domain  (MUST be idempotent, ADR-0002 D4)
   b. Per subdomain:    DNS DeleteRecord(old) + CreateRecord(new, TTL=60)
   c. Update DB:        subdomains.fqdn, main_domains.domain
   d. RenderAll with new domain name
   e. Snapshot conf to conf_snapshots BEFORE deploy
   f. SVN commit + Agent deploy
   g. Wait 30–60s (DNS TTL + CDN propagation — assumes TTL=60 per ADR-0001 D3)
   h. Probe verify — ≥ majority of active CN nodes must pass
      (Phase 1: ≥ 2 of 3;  Phase 2: ≥ 4 of 6)  — ADR-0001 D2

7. On success (all transitions via domain.Service.Transition()):
   - old main_domain.status  'switching' → 'blocked'   triggeredBy='switcher'
   - new main_domain.status  'deploying' → 'active'    triggeredBy='switcher'
   - pool.Service.OnSwitchCommitted(poolRowID):
       ready → promoted  (atomic with the new main_domain transition)
   - switch_history.status → 'completed'
   - Send INFO alert (switch completed)

8. On failure:
   - switch_history.status → 'failed', error_detail = failed step
   - Rollback completed steps in reverse order
   - old main_domain: 'switching' → 'active' via Transition() (if rollback succeeds)
   - Send P0 alert (auto-switch failed, manual intervention required)

9. Release locks:
   - DEL switch:lock:{mainDomainID}  (Redis first)
   - COMMIT Postgres tx               (then PG)
   If the process crashes between the two, Redis TTL (600s) cleans up and
   PG tx is rolled back by connection termination.
```

---

## Phase 5 — Release Subsystem (Week 4, parallel with deploy pipeline)

### Release State Machine

```
pending → running → completed
              ↓
           paused → running (resume)
              ↓
           failed
              ↓
         rolled_back
```

### Shard Execution Logic

File: `internal/release/service.go`

```
Run(ctx, releaseID int64):
  Load all shards ordered by shard_index
  Execute shards sequentially:

  Per shard:
    1. Dispatch domain deploy tasks (TypeAgentDeploy) for all domains in shard
    2. Poll domain_tasks completion (interval 10s, timeout 30min)
    3. Compute success rate: success_count / domain_count
    4. Rate < canary_threshold (default 95%) → auto-pause Release + P2 alert
    5. Rate >= threshold → proceed to next shard

API endpoints:
  POST /api/v1/releases              → release_manager  (create)
  GET  /api/v1/releases              → viewer           (list)
  GET  /api/v1/releases/:id          → viewer           (detail)
  POST /api/v1/releases/:id/start    → release_manager
  POST /api/v1/releases/:id/pause    → release_manager
  POST /api/v1/releases/:id/resume   → release_manager
  POST /api/v1/releases/:id/rollback → admin
```

---

## Phase 6 — Frontend (Week 6)

### Vue 3 Project Setup

```bash
cd web && npm create vite@latest . -- --template vue-ts
npm install naive-ui @vicons/ionicons5
npm install pinia vue-router @vueuse/core axios
npm install -D vitest @vue/test-utils @pinia/testing
```

Caddy config must include: `try_files {path} /index.html` for Vue Router history mode.

### Page List (Priority Order)

| Priority | Route | Page | Min Role |
|----------|-------|------|----------|
| P0 | `/login` | Login | — |
| P0 | `/` | Dashboard: domain status counts, recent alerts | viewer |
| P0 | `/domains` | Domain list: filter by project/status, cursor pagination | viewer |
| P0 | `/domains/:id` | Domain detail: subdomains, state history, probe status | viewer |
| P1 | `/projects` | Project list + detail | viewer |
| P1 | `/releases` | Release list + pause/resume/rollback controls | viewer |
| P1 | `/alerts` | Alert history + ack | viewer |
| P2 | `/pool` | Standby pool management | operator |
| P2 | `/servers` | Server management | admin |
| P2 | `/settings/users` | User management | admin |

### TypeScript Type Discipline

- Strict mode: no `any` except in third-party library wrappers
- `web/src/types/` mirrors Go response DTOs exactly — keep in sync
- Every API call wrapped in try/catch with user-facing `message.error()`

---

## Common Mistakes to Avoid

1. **Don't create God services.** Each service handles one domain concept. `DomainService` handles domain CRUD and state. `ReleaseService` handles releases. `SwitchService` handles failover. They can call each other through interfaces.

2. **Don't put business logic in handlers.** Handlers only: parse request → call service → format response. Validation beyond struct tags goes in the service.

3. **Don't create DTOs for everything.** Internal service-to-service communication uses domain models directly. DTOs only at API boundary (request/response).

4. **Don't forget transactions.** Any operation that writes to multiple tables MUST use a transaction. Pass `*sqlx.Tx` through the call chain.

5. **Don't ignore the down migration.** Every `up.sql` needs a working `down.sql`. Test it.

6. **Don't hardcode provider-specific logic.** If you find yourself writing `if provider == "cloudflare"` in business logic, you're doing it wrong. That logic belongs inside the provider implementation.

7. **Don't use `time.Sleep` for waiting.** Use tickers, timers, or channels for periodic operations. Use context deadlines for timeouts.

8. **Don't skip graceful shutdown.** The API server, worker, and scanner all need proper signal handling and graceful shutdown to avoid data corruption.

---

## How to Ask for Help

When you encounter ambiguity in the spec, check documents in this order:
1. CLAUDE.md (conventions)
2. ARCHITECTURE.md (design decisions)
3. DEVELOPMENT_PLAYBOOK.md (implementation patterns)
4. DATABASE_SCHEMA.md (data model)

If the answer isn't in any document, make a reasonable decision following these principles:
- Prefer simplicity over cleverness
- Prefer explicit over implicit
- Prefer composition over inheritance
- Prefer failing fast over silent degradation
- Log the decision as a code comment for future reference
