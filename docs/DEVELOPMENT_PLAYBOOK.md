# DEVELOPMENT_PLAYBOOK.md — Step-by-Step Implementation Guide

> This document tells Claude Code exactly HOW to implement each feature.
> Follow the patterns here. Do not invent new patterns unless explicitly asked.

---

## 1. How to Add a New API Endpoint

### Step 1: Define the handler

```go
// api/handler/domain.go

// CreateDomain godoc
// @Summary Register a new domain
// @Tags domains
// @Accept json
// @Produce json
// @Param body body CreateDomainRequest true "Domain registration"
// @Success 201 {object} Response{data=DomainResponse}
// @Failure 400 {object} Response
// @Router /api/v1/domains [post]
func (h *DomainHandler) Create(c *gin.Context) {
    var req CreateDomainRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, ErrorResponse(40001, err.Error()))
        return
    }

    domain, err := h.service.Create(c.Request.Context(), &req)
    if err != nil {
        if errors.Is(err, ErrDomainExists) {
            c.JSON(http.StatusConflict, ErrorResponse(40901, "domain already exists"))
            return
        }
        h.logger.Error("create domain failed", zap.Error(err))
        c.JSON(http.StatusInternalServerError, ErrorResponse(50000, "internal error"))
        return
    }

    c.JSON(http.StatusCreated, SuccessResponse(domain.ToResponse()))
}
```

### Step 2: Add service method

```go
// internal/domain/service.go
func (s *Service) Create(ctx context.Context, req *CreateDomainRequest) (*MainDomain, error) {
    // 1. Validate
    if !isValidDomain(req.Domain) {
        return nil, fmt.Errorf("invalid domain: %s", req.Domain)
    }

    // 2. Check duplicates
    existing, _ := s.store.GetByDomain(ctx, req.Domain)
    if existing != nil {
        return nil, ErrDomainExists
    }

    // 3. Business logic in transaction
    tx, err := s.db.BeginTxx(ctx, nil)
    if err != nil {
        return nil, fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback()

    domain := &MainDomain{
        UUID:      uuid.Must(uuid.NewV7()).String(),
        Domain:    req.Domain,
        ProjectID: req.ProjectID,
        Status:    "inactive",
    }

    if err := s.store.CreateTx(ctx, tx, domain); err != nil {
        return nil, fmt.Errorf("create domain: %w", err)
    }

    // 4. Auto-generate subdomains from prefix rules
    rules, err := s.prefixStore.ListByProject(ctx, req.ProjectID)
    if err != nil {
        return nil, fmt.Errorf("list prefix rules: %w", err)
    }

    for _, prefix := range req.Prefixes {
        rule := findRule(rules, prefix)
        if rule == nil {
            return nil, fmt.Errorf("unknown prefix: %s", prefix)
        }
        sub := &Subdomain{
            MainDomainID: domain.ID,
            Prefix:       prefix,
            FQDN:         prefix + "." + domain.Domain,
            DNSProvider:   rule.DNSProvider,
            CDNProvider:   rule.CDNProvider,
            NginxTemplate: rule.NginxTemplate,
        }
        if err := s.subStore.CreateTx(ctx, tx, sub); err != nil {
            return nil, fmt.Errorf("create subdomain %s: %w", sub.FQDN, err)
        }
    }

    // 5. Audit log
    s.audit.Log(ctx, AuditEntry{
        Action:   "domain.created",
        TargetID: domain.UUID,
        Detail:   fmt.Sprintf("domain=%s project=%d prefixes=%v", domain.Domain, req.ProjectID, req.Prefixes),
    })

    return domain, tx.Commit()
}
```

### Step 3: Add store method

```go
// store/postgres/domain.go
func (s *DomainStore) CreateTx(ctx context.Context, tx *sqlx.Tx, d *MainDomain) error {
    const q = `
        INSERT INTO main_domains (uuid, domain, project_id, status, created_at, updated_at)
        VALUES ($1, $2, $3, $4, NOW(), NOW())
        RETURNING id, created_at, updated_at`
    return tx.QueryRowxContext(ctx, q, d.UUID, d.Domain, d.ProjectID, d.Status).
        Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}
```

### Step 4: Register route

```go
// api/router/router.go
func RegisterRoutes(r *gin.Engine, h *Handlers, mw *Middleware) {
    v1 := r.Group("/api/v1")
    v1.Use(mw.Auth())

    domains := v1.Group("/domains")
    {
        domains.GET("", mw.RequireRole("viewer"), h.Domain.List)
        domains.POST("", mw.RequireRole("admin"), h.Domain.Create)
        domains.GET("/:id", mw.RequireRole("viewer"), h.Domain.Get)
        domains.POST("/:id/deploy", mw.RequireRole("release_manager"), h.Domain.Deploy)
    }
}
```

### Step 5: Write tests

```go
// api/handler/domain_test.go
func TestCreateDomain(t *testing.T) {
    // Setup mock service
    svc := &mockDomainService{
        createFn: func(ctx context.Context, req *CreateDomainRequest) (*MainDomain, error) {
            return &MainDomain{UUID: "test-uuid", Domain: req.Domain, Status: "inactive"}, nil
        },
    }
    handler := NewDomainHandler(svc, zap.NewNop())

    // Create request
    body := `{"domain":"example.com","project_id":1,"prefixes":["www","ws"]}`
    req := httptest.NewRequest("POST", "/api/v1/domains", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()

    // Execute
    router := gin.New()
    router.POST("/api/v1/domains", handler.Create)
    router.ServeHTTP(w, req)

    // Assert
    assert.Equal(t, http.StatusCreated, w.Code)
    var resp Response
    json.Unmarshal(w.Body.Bytes(), &resp)
    assert.Equal(t, 0, resp.Code)
}
```

---

## 2. How to Add a New Provider

Example: Adding Tencent Cloud DNS

### Step 1: Implement the interface

```go
// pkg/provider/dns/tencent.go
package dns

import (
    dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

type TencentProvider struct {
    client *dnspod.Client
    logger *zap.Logger
}

func NewTencentProvider(secretID, secretKey string, logger *zap.Logger) (*TencentProvider, error) {
    credential := common.NewCredential(secretID, secretKey)
    client, err := dnspod.NewClient(credential, "", profile.NewClientProfile())
    if err != nil {
        return nil, fmt.Errorf("create tencent dns client: %w", err)
    }
    return &TencentProvider{client: client, logger: logger}, nil
}

func (p *TencentProvider) Name() string { return "tencent" }

func (p *TencentProvider) CreateRecord(ctx context.Context, zone string, record Record) (*Record, error) {
    req := dnspod.NewCreateRecordRequest()
    req.Domain = &zone
    req.SubDomain = &record.Name
    req.RecordType = &record.Type
    req.Value = &record.Content
    ttl := uint64(record.TTL)
    req.TTL = &ttl
    recordLine := "默认"
    req.RecordLine = &recordLine

    resp, err := p.client.CreateRecord(req)
    if err != nil {
        return nil, fmt.Errorf("tencent create record %s.%s: %w", record.Name, zone, err)
    }

    record.ID = fmt.Sprintf("%d", *resp.Response.RecordId)
    return &record, nil
}

// ... implement DeleteRecord, ListRecords, UpdateRecord similarly
```

### Step 2: Register in the registry

```go
// pkg/provider/dns/registry.go
var registry = map[string]ProviderFactory{}

type ProviderFactory func(cfg map[string]string, logger *zap.Logger) (Provider, error)

func Register(name string, factory ProviderFactory) {
    registry[name] = factory
}

func GetProvider(name string, cfg map[string]string, logger *zap.Logger) (Provider, error) {
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown dns provider: %s", name)
    }
    return factory(cfg, logger)
}

func init() {
    Register("cloudflare", func(cfg map[string]string, l *zap.Logger) (Provider, error) {
        return NewCloudflareProvider(cfg["api_token"], l)
    })
    Register("aliyun", func(cfg map[string]string, l *zap.Logger) (Provider, error) {
        return NewAliyunProvider(cfg["access_key_id"], cfg["access_key_secret"], l)
    })
    Register("tencent", func(cfg map[string]string, l *zap.Logger) (Provider, error) {
        return NewTencentProvider(cfg["secret_id"], cfg["secret_key"], l)
    })
}
```

### Step 3: Add config

```yaml
# configs/providers.yaml
dns:
  cloudflare:
    api_token: "${CLOUDFLARE_API_TOKEN}"
  aliyun:
    access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
    access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
  tencent:
    secret_id: "${TENCENT_SECRET_ID}"
    secret_key: "${TENCENT_SECRET_KEY}"
```

### Step 4: Write provider-specific tests

```go
// pkg/provider/dns/tencent_test.go
// Use build tags for integration tests that need real API keys
//go:build integration

func TestTencentProvider_CreateRecord(t *testing.T) {
    // Only runs with: go test -tags=integration ./pkg/provider/dns/ -run TestTencent
}
```

---

## 3. How to Add a New asynq Task

Example: Domain pre-warming task

### Step 1: Define payload

```go
// internal/tasks/pool_warmup.go
type PoolWarmupPayload struct {
    PoolDomainID int64  `json:"pool_domain_id"`
    MainDomain   string `json:"main_domain"`
    ProjectID    int64  `json:"project_id"`
}
```

### Step 2: Implement handler

```go
func (h *TaskHandler) HandlePoolWarmup(ctx context.Context, t *asynq.Task) error {
    var payload PoolWarmupPayload
    if err := json.Unmarshal(t.Payload(), &payload); err != nil {
        return fmt.Errorf("unmarshal warmup payload: %w", err)
    }

    h.logger.Info("starting pool warmup",
        zap.String("domain", payload.MainDomain),
        zap.Int64("pool_domain_id", payload.PoolDomainID),
    )

    // Step 0: Transition pool row pending → warming (see ARCHITECTURE.md §2.6)
    // Step 1: Create DNS records for all prefixes (TTL = 60, see §2.2 rule)
    // Step 2: Create CDN configs for all prefixes
    // Step 3: Wait for propagation
    // Step 4: Probe verification — require ≥ majority of active CN probe nodes
    //         (Phase 1: ≥ 2 of 3; Phase 2: ≥ 4 of 6)
    // Step 5: Update status → ready on success, → pending on failure (with
    //         warmup_attempts++ and warmup_last_error set). NEVER write 'standby'
    //         or 'active' here — those names belong to the old schema.

    return nil
}
```

### Step 3: Register in worker

```go
// cmd/worker/main.go
mux := asynq.NewServeMux()
mux.HandleFunc(tasks.TypeDNSCreateRecord, handler.HandleDNSCreate)
mux.HandleFunc(tasks.TypePoolWarmup, handler.HandlePoolWarmup)
// ... register all task types
```

### Step 4: Dispatch from business logic

```go
// internal/pool/service.go
func (s *Service) AddStandbyDomain(ctx context.Context, projectID int64, domain string) error {
    // ... create pool entry with status=pending ...

    payload, _ := json.Marshal(tasks.PoolWarmupPayload{
        PoolDomainID: poolDomain.ID,
        MainDomain:   domain,
        ProjectID:    projectID,
    })

    task := asynq.NewTask(tasks.TypePoolWarmup, payload,
        asynq.MaxRetry(3),
        asynq.Timeout(10*time.Minute),
        asynq.Queue("pool"),
    )

    _, err := s.taskClient.Enqueue(task)
    return err
}
```

---

## 4. How to Add a Database Migration

```bash
# Create migration files
make migrate-create name=add_ssl_expiry_to_subdomains
# This creates:
#   migrations/000015_add_ssl_expiry_to_subdomains.up.sql
#   migrations/000015_add_ssl_expiry_to_subdomains.down.sql
```

```sql
-- 000015_add_ssl_expiry_to_subdomains.up.sql
ALTER TABLE subdomains ADD COLUMN ssl_expiry TIMESTAMPTZ;
CREATE INDEX idx_subdomains_ssl_expiry ON subdomains (ssl_expiry) WHERE ssl_expiry IS NOT NULL;

-- 000015_add_ssl_expiry_to_subdomains.down.sql
DROP INDEX IF EXISTS idx_subdomains_ssl_expiry;
ALTER TABLE subdomains DROP COLUMN IF EXISTS ssl_expiry;
```

**Rules:**
- NEVER modify existing migrations
- ALWAYS write both up and down
- ALWAYS use `IF NOT EXISTS` / `IF EXISTS`
- Test down migration actually works before committing

---

## 5. How to Add a Vue Page

### Step 1: Create the view component

```
web/src/views/domains/DomainList.vue
web/src/views/domains/DomainDetail.vue
```

### Step 2: Add route

```typescript
// web/src/router/index.ts
{
    path: '/domains',
    name: 'DomainList',
    component: () => import('@/views/domains/DomainList.vue'),
    meta: { title: 'Domains', requiresAuth: true, minRole: 'viewer' }
},
{
    path: '/domains/:id',
    name: 'DomainDetail',
    component: () => import('@/views/domains/DomainDetail.vue'),
    meta: { title: 'Domain Detail', requiresAuth: true, minRole: 'viewer' }
}
```

### Step 3: Add API client

```typescript
// web/src/api/domain.ts
export const domainApi = {
    list: (params: DomainListParams) =>
        http.get<PaginatedResponse<DomainResponse>>('/api/v1/domains', { params }),
    get: (id: string) =>
        http.get<DomainResponse>(`/api/v1/domains/${id}`),
    create: (data: CreateDomainRequest) =>
        http.post<DomainResponse>('/api/v1/domains', data),
}
```

### Step 4: Add Pinia store (if needed for shared state)

```typescript
// web/src/stores/domain.ts
export const useDomainStore = defineStore('domain', () => {
    const domains = ref<DomainResponse[]>([])
    const loading = ref(false)

    async function fetchByProject(projectId: number) {
        loading.value = true
        try {
            const res = await domainApi.list({ project_id: projectId })
            domains.value = res.data.items
        } finally {
            loading.value = false
        }
    }

    return { domains, loading, fetchByProject }
})
```

---

## 5.5 How to Add a Release (canary + shard sizing)

When building a Release row in code or in an admin form, honor the following invariants
(enforced in the service layer, not the DB):

```go
// internal/release/service.go
func (s *Service) NewRelease(projectID int64, total int) *Release {
    canary := total * 2 / 100  // 2%
    if canary > 30 { canary = 30 }
    if canary < 10 { canary = 10 }
    if canary > total { canary = total }

    return &Release{
        ProjectID:       projectID,
        TotalDomains:    total,
        ShardSize:       200,     // normal shards 200–500
        CanaryShardSize: canary,
        CanaryThreshold: 0.95,
    }
}
```

- A Release is scoped to **one project**. To roll out across projects, dispatch
  multiple Releases and coordinate in the UI, never merge them into one DB row.
- Shard 0 is always the canary shard sized per the formula above.
- Shards 1..N use `shard_size` (default 200). Domains are assigned by
  `hash(main_domain_id) % shard_count` so retries land in the same shard.
- The release scheduler MUST check `release:pause:{project_id}` before starting
  each shard and abort cleanly if the pause flag is set.

---

## 6. Common Patterns Reference

### Pagination (cursor-based)

```go
// Store layer
type ListOpts struct {
    Cursor  string
    Limit   int
    Status  string
    OrderBy string
}

func (s *DomainStore) List(ctx context.Context, projectID int64, opts ListOpts) ([]MainDomain, string, error) {
    if opts.Limit == 0 { opts.Limit = 50 }
    if opts.Limit > 200 { opts.Limit = 200 }

    var domains []MainDomain
    q := `SELECT * FROM main_domains WHERE project_id = $1 AND deleted_at IS NULL`
    args := []interface{}{projectID}

    if opts.Cursor != "" {
        cursorID, _ := decodeCursor(opts.Cursor)
        q += ` AND id > $2`
        args = append(args, cursorID)
    }

    q += ` ORDER BY id ASC LIMIT $` + fmt.Sprintf("%d", len(args)+1)
    args = append(args, opts.Limit+1) // fetch one extra to detect hasMore

    err := s.db.SelectContext(ctx, &domains, q, args...)
    // ... build next cursor from last item
}
```

### Audit Logging

```go
// EVERY write operation must log an audit entry
s.audit.Log(ctx, AuditEntry{
    UserID:   middleware.GetUserID(ctx),
    Action:   "domain.deployed",
    TargetID: domain.UUID,
    Detail:   fmt.Sprintf("release=%s shard=%d", release.UUID, shardID),
    IP:       middleware.GetClientIP(ctx),
})
```

### Graceful Shutdown

```go
// cmd/server/main.go
srv := &http.Server{Addr: ":8080", Handler: router}

go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Fatal("server failed", zap.Error(err))
    }
}()

quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

logger.Info("shutting down server...")
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
srv.Shutdown(ctx)
```

### Login identifier

- User accounts use **`username`**, not email. API payloads, Pinia stores, TypeScript
  types, and Go DTOs MUST all name the field `username`. Do not reintroduce `email` on
  the `users` table or in any login path. See ARCHITECTURE.md §4 and ADR-0001.
