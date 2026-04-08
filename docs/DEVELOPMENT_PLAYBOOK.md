# DEVELOPMENT_PLAYBOOK.md — Step-by-Step Implementation Guide

> **Aligned with PRD + ADR-0003 (2026-04-09).** This document tells Claude
> Code exactly HOW to implement each feature pattern. Follow the patterns
> here. Do not invent new patterns unless explicitly asked.

---

## Table of contents

1. How to add a new API endpoint
2. How to add a state machine transition (lifecycle / release / agent)
3. How to add a DNS provider
4. How to add an asynq task
5. How to add an artifact build step
6. How to add an agent task type
7. How to add a probe check
8. How to add a database migration
9. How to add a Vue page
10. How to dispatch a release
11. Common patterns reference

---

## 1. How to add a new API endpoint

### Step 1: Define the handler

```go
// api/handler/release.go

// CreateRelease godoc
// @Summary  Create a new release
// @Tags     releases
// @Accept   json
// @Produce  json
// @Param    body  body      CreateReleaseRequest  true  "Release request"
// @Success  202   {object}  Response{data=ReleaseResponse}
// @Failure  400   {object}  Response
// @Failure  409   {object}  Response
// @Router   /api/v1/releases [post]
func (h *ReleaseHandler) Create(c *gin.Context) {
    var req CreateReleaseRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, ErrorResponse(40001, err.Error()))
        return
    }

    rel, err := h.service.Create(c.Request.Context(), &req, currentUserID(c))
    if err != nil {
        switch {
        case errors.Is(err, release.ErrInvalidScope):
            c.JSON(http.StatusBadRequest, ErrorResponse(40002, "invalid release scope"))
        case errors.Is(err, release.ErrApprovalRequired):
            c.JSON(http.StatusConflict, ErrorResponse(40901, "approval required for prod release"))
        default:
            h.logger.Error("create release failed", zap.Error(err))
            c.JSON(http.StatusInternalServerError, ErrorResponse(50000, "internal error"))
        }
        return
    }

    c.JSON(http.StatusAccepted, SuccessResponse(rel.ToResponse()))
}
```

### Step 2: Add service method

```go
// internal/release/service.go
func (s *Service) Create(ctx context.Context, req *CreateReleaseRequest, userID int64) (*Release, error) {
    // 1. Validate scope
    if len(req.DomainIDs) == 0 {
        return nil, ErrInvalidScope
    }

    // 2. Project must exist; user must have at least operator role
    project, err := s.projectStore.GetByID(ctx, req.ProjectID)
    if err != nil {
        return nil, fmt.Errorf("get project: %w", err)
    }

    // 3. Approval check (Phase 4 will enforce; Phase 1 auto-approves)
    requiresApproval := project.IsProd || req.ReleaseType == "nginx"

    // 4. Begin transaction
    tx, err := s.db.BeginTxx(ctx, nil)
    if err != nil {
        return nil, fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback()

    rel := &Release{
        ReleaseID:        generateReleaseID(),
        ProjectID:        req.ProjectID,
        TemplateVersionID: req.TemplateVersionID,
        ReleaseType:      req.ReleaseType,
        TriggerSource:    "ui",
        Status:           "pending",
        RequiresApproval: requiresApproval,
        CreatedBy:        userID,
    }

    if err := s.store.CreateTx(ctx, tx, rel); err != nil {
        return nil, fmt.Errorf("create release: %w", err)
    }

    // 5. Insert release_scopes rows
    for _, dID := range req.DomainIDs {
        scope := ReleaseScope{ReleaseID: rel.ID, DomainID: dID, HostGroupID: req.HostGroupID}
        if err := s.store.CreateScopeTx(ctx, tx, scope); err != nil {
            return nil, fmt.Errorf("create scope: %w", err)
        }
    }

    // 6. Audit
    s.audit.LogTx(ctx, tx, AuditEntry{
        UserID: userID, Action: "release.created",
        TargetKind: "release", TargetID: rel.ReleaseID,
        Detail: map[string]any{"type": rel.ReleaseType, "scope_count": len(req.DomainIDs)},
    })

    if err := tx.Commit(); err != nil {
        return nil, fmt.Errorf("commit: %w", err)
    }

    // 7. Dispatch async planning task (after commit, never inside the tx)
    payload, _ := json.Marshal(ReleasePlanPayload{ReleaseID: rel.ID})
    task := asynq.NewTask(tasks.TypeReleasePlan, payload, asynq.Queue("release"))
    if _, err := s.tasks.Enqueue(task); err != nil {
        s.logger.Error("enqueue release plan failed", zap.String("release_id", rel.ReleaseID), zap.Error(err))
        // The release row exists; a periodic janitor will pick it up. Do not fail the API call.
    }

    return rel, nil
}
```

### Step 3: Add store method

```go
// store/postgres/release.go
const insertRelease = `
    INSERT INTO releases (
        uuid, release_id, project_id, template_version_id, release_type,
        trigger_source, status, requires_approval, canary_shard_size, shard_size,
        description, created_by
    ) VALUES (
        :uuid, :release_id, :project_id, :template_version_id, :release_type,
        :trigger_source, :status, :requires_approval, :canary_shard_size, :shard_size,
        :description, :created_by
    ) RETURNING id`

func (s *ReleaseStore) CreateTx(ctx context.Context, tx *sqlx.Tx, r *Release) error {
    rows, err := tx.NamedQueryContext(ctx, insertRelease, r)
    if err != nil {
        return fmt.Errorf("insert release: %w", err)
    }
    defer rows.Close()
    if rows.Next() {
        return rows.Scan(&r.ID)
    }
    return errors.New("insert release: no rows returned")
}
```

### Step 4: Register the route

```go
// api/router/router.go
v1 := r.Group("/api/v1", middleware.Auth(jwtVerifier))
{
    releases := v1.Group("/releases", middleware.RequireRole("operator"))
    {
        releases.POST("",       releaseHandler.Create)
        releases.GET("",        releaseHandler.List)
        releases.GET("/:id",    releaseHandler.Get)
        releases.POST("/:id/pause",    releaseHandler.Pause)
        releases.POST("/:id/resume",   releaseHandler.Resume)
        releases.POST("/:id/rollback", middleware.RequireRole("release_manager"), releaseHandler.Rollback)
    }
}
```

### Step 5: Write tests

```go
// internal/release/service_test.go
func TestService_Create(t *testing.T) {
    t.Run("happy path", func(t *testing.T) { /* ... */ })
    t.Run("empty scope returns ErrInvalidScope", func(t *testing.T) { /* ... */ })
    t.Run("nginx release on prod project requires approval", func(t *testing.T) { /* ... */ })
}
```

---

## 2. How to add a state machine transition

The platform has three state machines, each with a single write path. Adding
a new transition means adding it to the validity map AND verifying the
caller goes through `Transition()`.

### The pattern

```go
// internal/lifecycle/statemachine.go
var validLifecycleTransitions = map[string][]string{
    "requested":   {"approved", "retired"},
    "approved":    {"provisioned", "retired"},
    "provisioned": {"active", "disabled", "retired"},
    "active":      {"disabled", "retired"},
    "disabled":    {"active", "retired"},
    "retired":     {},
}

func CanLifecycleTransition(from, to string) bool {
    targets, ok := validLifecycleTransitions[from]
    if !ok { return false }
    for _, t := range targets {
        if t == to { return true }
    }
    return false
}
```

```go
// internal/lifecycle/service.go — THE ONLY write path for domains.lifecycle_state
func (s *Service) Transition(
    ctx context.Context,
    id int64,
    from string,           // expected current state (optimistic check)
    to string,
    reason string,
    triggeredBy string,    // "user:{uuid}" | "system" | "approval:{id}" | "provisioning"
) error {
    tx, err := s.db.BeginTxx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback()

    var current string
    err = tx.QueryRowxContext(ctx,
        `SELECT lifecycle_state FROM domains WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id,
    ).Scan(&current)
    if errors.Is(err, sql.ErrNoRows) {
        return ErrDomainNotFound
    }
    if err != nil {
        return fmt.Errorf("select for update: %w", err)
    }

    if current != from {
        return ErrLifecycleRaceCondition
    }
    if !CanLifecycleTransition(from, to) {
        return ErrInvalidLifecycleState
    }

    if err := s.store.updateLifecycleStateTx(ctx, tx, id, from, to); err != nil {
        return fmt.Errorf("update state: %w", err)
    }
    if err := s.store.insertLifecycleHistoryTx(ctx, tx, LifecycleHistoryEntry{
        DomainID: id, FromState: from, ToState: to,
        Reason: reason, TriggeredBy: triggeredBy,
    }); err != nil {
        return fmt.Errorf("insert history: %w", err)
    }

    return tx.Commit()
}
```

```go
// store/postgres/lifecycle.go — THE ONLY file allowed to UPDATE domains.lifecycle_state
const updateLifecycleStateSQL = `
    UPDATE domains
    SET lifecycle_state = $3, updated_at = NOW()
    WHERE id = $1 AND lifecycle_state = $2`

func (s *DomainStore) updateLifecycleStateTx(ctx context.Context, tx *sqlx.Tx, id int64, from, to string) error {
    _, err := tx.ExecContext(ctx, updateLifecycleStateSQL, id, from, to)
    return err
}
```

### The CI gate

```makefile
# Makefile
check-lifecycle-writes:
	@hits=$$(grep -rn 'UPDATE domains SET lifecycle_state' --include='*.go' . | \
		grep -v 'store/postgres/lifecycle.go' || true); \
	if [ -n "$$hits" ]; then \
		echo "ERROR: direct lifecycle_state writes found outside store/postgres/lifecycle.go:"; \
		echo "$$hits"; exit 1; \
	fi
```

The same pattern repeats for `release_state` (`make check-release-writes`)
and `agent.status` (`make check-agent-writes`).

### Race test (mandatory for state machine code)

```go
// internal/lifecycle/service_test.go
func TestTransition_RaceCondition(t *testing.T) {
    // Real DB needed; this is an integration test, not a pure unit test
    db := setupTestDB(t)
    s := NewService(db, ...)

    domainID := createTestDomain(t, db, "active")

    var winners, losers int32
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            err := s.Transition(ctx, domainID, "active", "disabled", "race test", "user:test")
            if err == nil {
                atomic.AddInt32(&winners, 1)
            } else if errors.Is(err, ErrLifecycleRaceCondition) {
                atomic.AddInt32(&losers, 1)
            }
        }()
    }
    wg.Wait()

    require.Equal(t, int32(1), winners, "exactly one Transition must succeed")
    require.Equal(t, int32(9), losers,  "nine Transitions must fail with ErrLifecycleRaceCondition")
}
```

Run with `go test -race -count=50 ./internal/lifecycle/...` — must pass green
in CI.

---

## 3. How to add a DNS provider

The Domain Lifecycle module uses DNS providers to create records when a
domain transitions from `approved` to `provisioned`.

### Step 1: Implement the interface

```go
// pkg/provider/dns/cloudflare.go
package dns

type cloudflareProvider struct {
    apiToken string
    client   *http.Client
}

func NewCloudflareProvider(cfg Config) (Provider, error) {
    if cfg.APIToken == "" {
        return nil, errors.New("cloudflare: api_token required")
    }
    return &cloudflareProvider{apiToken: cfg.APIToken, client: defaultHTTPClient()}, nil
}

func (p *cloudflareProvider) Name() string { return "cloudflare" }

func (p *cloudflareProvider) CreateRecord(ctx context.Context, zone string, rec Record) (*Record, error) {
    // ... HTTP call to Cloudflare API
}

func (p *cloudflareProvider) DeleteRecord(ctx context.Context, zone string, recordID string) error {
    // ...
}

func (p *cloudflareProvider) ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error) {
    // ...
}

func (p *cloudflareProvider) UpdateRecord(ctx context.Context, zone string, recordID string, rec Record) (*Record, error) {
    // ...
}
```

### Step 2: Register

```go
// pkg/provider/dns/registry.go
func init() {
    Register("cloudflare", NewCloudflareProvider)
}
```

### Step 3: Add config

```yaml
# configs/providers.yaml
dns:
  cloudflare:
    api_token: "${CLOUDFLARE_API_TOKEN}"
  aliyun:
    access_key_id:     "${ALIYUN_ACCESS_KEY_ID}"
    access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
```

### Step 4: Provider tests (table-driven, with httptest mock)

```go
// pkg/provider/dns/cloudflare_test.go
func TestCloudflareProvider_CreateRecord(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // assert request shape, return canned response
    }))
    defer server.Close()

    p := newCloudflareWithBaseURL(server.URL, "test-token")

    rec, err := p.CreateRecord(context.Background(), "example.com",
        Record{Type: "A", Name: "www", Content: "1.2.3.4", TTL: 300})
    require.NoError(t, err)
    require.NotEmpty(t, rec.ID)
}
```

---

## 4. How to add an asynq task

### Step 1: Add task type constant

```go
// internal/tasks/types.go
const (
    TypeArtifactBuild        = "artifact:build"
    TypeArtifactSign         = "artifact:sign"
    TypeReleasePlan          = "release:plan"
    TypeReleaseDispatchShard = "release:dispatch_shard"
    // ... add new constant here
    TypeMyNewTask            = "myscope:my_action"
)
```

### Step 2: Define payload struct

```go
// internal/tasks/payloads.go
type MyNewTaskPayload struct {
    TargetID int64  `json:"target_id"`
    Reason   string `json:"reason"`
}
```

### Step 3: Implement handler

```go
// internal/myscope/handler.go
func (h *Handler) HandleMyNewTask(ctx context.Context, t *asynq.Task) error {
    var p tasks.MyNewTaskPayload
    if err := json.Unmarshal(t.Payload(), &p); err != nil {
        return fmt.Errorf("unmarshal: %w", err)
    }

    h.logger.Info("processing my new task",
        zap.String("type", t.Type()),
        zap.Int64("target_id", p.TargetID))

    if err := h.service.DoThing(ctx, p.TargetID, p.Reason); err != nil {
        // Returning the error triggers asynq retry per task config
        return fmt.Errorf("do thing: %w", err)
    }

    return nil
}
```

### Step 4: Register handler in worker

```go
// cmd/worker/main.go
mux := asynq.NewServeMux()
mux.HandleFunc(tasks.TypeMyNewTask, myscopeHandler.HandleMyNewTask)
// ...
```

### Step 5: Dispatch from business logic

```go
// internal/somewhere/service.go
payload, _ := json.Marshal(tasks.MyNewTaskPayload{TargetID: id, Reason: "user request"})
task := asynq.NewTask(tasks.TypeMyNewTask, payload,
    asynq.MaxRetry(3),
    asynq.Timeout(60*time.Second),
    asynq.Queue("default"),
)
if _, err := s.tasks.Enqueue(task); err != nil {
    return fmt.Errorf("enqueue task: %w", err)
}
```

### Choosing a queue (per ARCHITECTURE.md §2.5 canonical layout)

- `critical` — auto-rollback, escalating health checks
- `release` — release planning, shard dispatch, finalization
- `artifact` — build, sign (CPU bound, low concurrency)
- `lifecycle` — domain state transitions
- `probe` — probe runs
- `default` — notify, misc

---

## 5. How to add an artifact build step

The artifact build pipeline is in `internal/artifact/builder.go`. It produces
an immutable artifact from a `(template_version, domains, variables)` tuple.

### Build flow

```go
// internal/artifact/builder.go
func (b *Builder) Build(ctx context.Context, req BuildRequest) (*Artifact, error) {
    // 1. Load template version
    tv, err := b.templateStore.GetVersion(ctx, req.TemplateVersionID)
    if err != nil { return nil, fmt.Errorf("load template version: %w", err) }
    if tv.PublishedAt == nil {
        return nil, errors.New("template version not published")
    }

    // 2. Load domains and merged variables
    domains, err := b.domainStore.ListByIDs(ctx, req.DomainIDs)
    if err != nil { return nil, err }

    artifactID := generateArtifactID()
    workdir := filepath.Join(b.workdirRoot, artifactID)
    if err := os.MkdirAll(workdir, 0o755); err != nil {
        return nil, fmt.Errorf("mkdir workdir: %w", err)
    }
    defer os.RemoveAll(workdir)

    manifest := &Manifest{
        ArtifactID:        artifactID,
        ReleaseID:         req.ReleaseID,
        Project:           req.ProjectSlug,
        ReleaseType:       req.ReleaseType,
        TemplateVersionID: tv.UUID,
        TemplateVersion:   tv.VersionLabel,
        CreatedAt:         time.Now().UTC(),
        CreatedBy:         req.UserID,
    }

    // 3. Render each domain — DETERMINISTIC ORDER
    sort.Slice(domains, func(i, j int) bool { return domains[i].FQDN < domains[j].FQDN })
    for _, d := range domains {
        vars := mergeVariables(tv.DefaultVariables, d.Variables, b.systemVariables(d, artifactID))

        if req.ReleaseType == "html" || req.ReleaseType == "full" {
            html, err := renderTemplate(tv.ContentHTML, vars)
            if err != nil {
                return nil, fmt.Errorf("render html for %s: %w", d.FQDN, err)
            }
            path := filepath.Join(workdir, "domains", d.FQDN, "index.html")
            if err := writeFile(path, html); err != nil { return nil, err }
            manifest.AddDomainFile(d.FQDN, "domains/"+d.FQDN+"/index.html", html)
        }

        if req.ReleaseType == "nginx" || req.ReleaseType == "full" {
            nginxConf, err := renderTemplate(tv.ContentNginx, vars)
            if err != nil {
                return nil, fmt.Errorf("render nginx for %s: %w", d.FQDN, err)
            }
            for _, hg := range req.HostGroups {
                path := filepath.Join(workdir, "nginx", hg.Name, d.FQDN+".conf")
                if err := writeFile(path, nginxConf); err != nil { return nil, err }
                manifest.AddNginxFile(hg.Name, "nginx/"+hg.Name+"/"+d.FQDN+".conf", nginxConf)
            }
        }
    }

    // 4. Write checksums.txt and manifest.json
    if err := manifest.WriteChecksums(filepath.Join(workdir, "checksums.txt")); err != nil { return nil, err }
    if err := manifest.WriteManifest(filepath.Join(workdir, "manifest.json")); err != nil { return nil, err }

    // 5. Compute artifact-level checksum
    manifest.Checksum = computeArtifactChecksum(workdir) // sha256 of manifest.json + checksums.txt

    // 6. Sign (per ADR-0004; placeholder in P1)
    sig, err := b.signer.Sign(ctx, manifest.Checksum)
    if err != nil { return nil, fmt.Errorf("sign: %w", err) }
    manifest.Signature = sig

    // 7. Upload to MinIO/S3
    storageURI, err := b.storage.UploadDir(ctx, workdir,
        fmt.Sprintf("%s/%s/", req.ProjectSlug, req.ReleaseID))
    if err != nil { return nil, fmt.Errorf("upload: %w", err) }

    // 8. Persist artifact row + mark signed_at
    art := &Artifact{
        ArtifactID:        artifactID,
        ProjectID:         req.ProjectID,
        ReleaseID:         req.ReleaseDBID,
        TemplateVersionID: req.TemplateVersionID,
        StorageURI:        storageURI,
        Manifest:          manifest.ToJSON(),
        Checksum:          manifest.Checksum,
        Signature:         sig,
        DomainCount:       len(domains),
        FileCount:         manifest.FileCount(),
        TotalSizeBytes:    manifest.TotalSize(),
        BuiltBy:           req.UserID,
        SignedAt:          ptr(time.Now().UTC()),
    }
    if err := b.store.Create(ctx, art); err != nil { return nil, err }

    return art, nil
}
```

### Reproducibility test (mandatory)

```go
// internal/artifact/builder_test.go
func TestBuilder_Build_Deterministic(t *testing.T) {
    b := newTestBuilder(t)
    req := BuildRequest{ /* fixed input */ }

    art1, err := b.Build(ctx, req); require.NoError(t, err)
    art2, err := b.Build(ctx, req); require.NoError(t, err)

    require.Equal(t, art1.Checksum, art2.Checksum,
        "two builds with the same input must produce the same checksum")
}
```

If this test fails, find the source of nondeterminism (random ID, timestamp,
map iteration order, sleep) and remove it. Building artifacts must be a pure
function.

---

## 6. How to add an agent task type

### Step 1: Define the task envelope kind

The wire protocol's `TaskEnvelope.Type` field is constrained:

```go
// pkg/agentprotocol/types.go
const (
    TaskTypeDeployHTML  = "deploy_html"
    TaskTypeDeployNginx = "deploy_nginx"
    TaskTypeDeployFull  = "deploy_full"
    TaskTypeRollback    = "rollback"
    TaskTypeVerify      = "verify"
    // Adding a new type means changing both server and agent in the same PR.
)
```

### Step 2: Implement on the agent side

```go
// cmd/agent/handler.go
func (a *Agent) handleTask(ctx context.Context, env *agentprotocol.TaskEnvelope) error {
    switch env.Type {
    case agentprotocol.TaskTypeDeployHTML:
        return a.handleDeployHTML(ctx, env)
    case agentprotocol.TaskTypeDeployNginx:
        return a.handleDeployNginx(ctx, env)
    // ... add new case here
    default:
        return fmt.Errorf("unknown task type: %s", env.Type)
    }
}
```

**Critical** (CLAUDE.md Rule #3): the handler must use only the agent's
whitelisted actions. No `exec.Command` other than the four hard-coded shell-out
points (`nginx -t`, `nginx -s reload`, configured local-verify HTTP, systemd
self-restart). PRs that touch `cmd/agent/` require Opus review.

### Step 3: Implement on the control-plane side

```go
// internal/agent/dispatcher.go
func (d *Dispatcher) DispatchTask(ctx context.Context, dt *DomainTask, agent *Agent, art *Artifact) error {
    presignedURL, err := d.storage.Presign(ctx, art.StorageURI, 15*time.Minute)
    if err != nil { return err }

    env := agentprotocol.TaskEnvelope{
        TaskID:      generateTaskID(),
        ReleaseID:   dt.ReleaseUUID,
        ArtifactID:  art.ArtifactID,
        ArtifactURL: presignedURL,
        ManifestSHA: art.Checksum,
        DeployPath:  agent.DeployPath(),
        Type:        deriveTaskType(dt.TaskType),
        AllowReload: dt.AllowReload(),
        TimeoutSec:  120,
    }

    payload, _ := json.Marshal(env)
    task := &AgentTask{
        TaskID:       env.TaskID,
        DomainTaskID: dt.ID,
        AgentID:      agent.ID,
        ArtifactID:   art.ID,
        ArtifactURL:  presignedURL,
        Payload:      payload,
        Status:       "pending",
    }
    return d.store.Create(ctx, task)
}
```

---

## 7. How to add a probe check

```go
// internal/probe/runner.go
func (r *Runner) runL2Check(ctx context.Context, task *ProbeTask) (*ProbeResult, error) {
    domain, err := r.domainStore.GetByID(ctx, task.DomainID)
    if err != nil { return nil, err }

    expected, err := r.artifactStore.GetByID(ctx, task.ExpectedArtifactID)
    if err != nil { return nil, err }

    url := "https://" + domain.FQDN + "/"
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    start := time.Now()
    resp, err := r.client.Do(req)
    duration := time.Since(start)

    result := &ProbeResult{
        DomainID:           task.DomainID,
        PolicyID:           task.PolicyID,
        ProbeTaskID:        task.ID,
        Tier:               2,
        ResponseTimeMs:     int(duration.Milliseconds()),
        ProbeRunner:        r.runnerID,
        CheckedAt:          time.Now().UTC(),
        ExpectedArtifactID: expected.ID,
    }

    if err != nil {
        result.Status = "tcp_fail"
        result.ErrorMessage = err.Error()
        return result, nil
    }
    defer resp.Body.Close()

    result.HTTPStatus = resp.StatusCode
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

    detected := parseReleaseVersionMeta(body)
    if detected == "" {
        result.Status = "content_mismatch"
        result.ErrorMessage = "release-version meta tag missing"
        return result, nil
    }
    if detected != expected.ArtifactID {
        result.Status = "content_mismatch"
        result.ErrorMessage = fmt.Sprintf("expected %s, got %s", expected.ArtifactID, detected)
        return result, nil
    }

    result.Status = "ok"
    result.DetectedArtifactID = expected.ID
    return result, nil
}
```

The result writes to `probe_results` (TimescaleDB hypertable). The alert
engine subscribes to status changes and dispatches notifications.

---

## 8. How to add a database migration

### Pre-launch (Phase 1) — modify the initial migration in place

Per the pre-launch exception (DATABASE_SCHEMA.md, ADR-0003 D9), Phase 1 may
edit `migrations/000001_init.up.sql` directly. Just edit the file, then run
`make migrate-down && make migrate-up` to recreate the DB.

### Post-launch — new numbered migration

```bash
# Pick the next sequence number
ls migrations/ | sort | tail -n 4
# Create up + down files
touch migrations/000003_add_my_table.up.sql
touch migrations/000003_add_my_table.down.sql
```

```sql
-- migrations/000003_add_my_table.up.sql
CREATE TABLE IF NOT EXISTS my_table (
    id         BIGSERIAL PRIMARY KEY,
    uuid       UUID NOT NULL DEFAULT gen_random_uuid(),
    -- ...
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_my_table_xxx ON my_table (xxx) WHERE deleted_at IS NULL;
```

```sql
-- migrations/000003_add_my_table.down.sql
DROP TABLE IF EXISTS my_table CASCADE;
```

Rules: see CLAUDE.md §"Database Migrations" — every UP needs a DOWN, never
modify applied migrations after launch, always include `id`/`created_at`/
`updated_at`/`deleted_at`.

---

## 9. How to add a Vue page

### Step 1: Create the view component

```vue
<!-- web/src/views/releases/ReleaseList.vue -->
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { PageHeader, AppTable, StatusTag } from '@/components'
import { releaseApi } from '@/api/release'
import type { ReleaseResponse } from '@/types/release'

const releases = ref<ReleaseResponse[]>([])
const loading  = ref(false)
const total    = ref(0)

const columns = [
  { title: 'Release', key: 'release_id' },
  { title: 'Project', key: 'project_name' },
  { title: 'Type',    key: 'release_type' },
  { title: 'Status',  key: 'status', render: (row: ReleaseResponse) => h(StatusTag, { status: row.status }) },
  { title: 'Created', key: 'created_at' },
]

async function fetchReleases() {
  loading.value = true
  try {
    const res = await releaseApi.list()
    releases.value = res.items
    total.value    = res.total
  } finally {
    loading.value = false
  }
}

onMounted(fetchReleases)
</script>

<template>
  <div class="list-page">
    <PageHeader title="Releases" :subtitle="`共 ${total} 筆`" />
    <AppTable :columns="columns" :data="releases" :loading="loading" />
  </div>
</template>
```

### Step 2: Add route

```typescript
// web/src/router/index.ts
{
  path: '/releases',
  component: () => import('@/views/releases/ReleaseList.vue'),
  meta: { requiresAuth: true },
}
```

### Step 3: Add API client

```typescript
// web/src/api/release.ts
import { http } from '@/utils/http'
import type { ReleaseResponse, CreateReleaseRequest } from '@/types/release'

export const releaseApi = {
  list:   ()       => http.get<{ items: ReleaseResponse[]; total: number }>('/api/v1/releases'),
  get:    (id: string) => http.get<ReleaseResponse>(`/api/v1/releases/${id}`),
  create: (data: CreateReleaseRequest) => http.post<ReleaseResponse>('/api/v1/releases', data),
  pause:  (id: string) => http.post(`/api/v1/releases/${id}/pause`),
  resume: (id: string) => http.post(`/api/v1/releases/${id}/resume`),
}
```

### Step 4: Add TypeScript types

```typescript
// web/src/types/release.ts
export interface ReleaseResponse {
  uuid: string
  release_id: string
  project_id: number
  project_name?: string
  release_type: 'html' | 'nginx' | 'full'
  status: ReleaseStatus
  created_at: string
}

export type ReleaseStatus =
  | 'pending' | 'planning' | 'ready' | 'executing'
  | 'paused' | 'succeeded' | 'failed'
  | 'rolling_back' | 'rolled_back' | 'cancelled'
```

### FRONTEND_GUIDE.md compliance

Read `docs/FRONTEND_GUIDE.md` before adding any page. Use only the shared
components (`PageHeader`, `AppTable`, `StatusTag`, `SeverityTag`, `ConfirmModal`).
No raw `NDataTable`, no inline hex colors, no inline pixel values.

---

## 10. How to dispatch a release

The release execution flow is the most complex pipeline in the platform.
Here's the canonical sequence (Phase 1 simplified version — Phase 2 adds
sharding, Phase 3 adds canary + probe gating):

```
Operator → POST /api/v1/releases → release row in 'pending'
                                          │
                                          ▼ (asynq enqueue)
                              TypeReleasePlan handler
                                          │
                                          │ Transition: pending → planning
                                          │
                              ┌───────────┼───────────┐
                              ▼           ▼           ▼
                       artifact build  scope expand  shard size
                              │
                              │ artifacts.signed_at = NOW()
                              │ Transition: planning → ready
                              ▼
                       (auto or manual trigger)
                              │
                              ▼ (asynq enqueue)
                       TypeReleaseDispatchShard handler
                              │
                              │ Transition: ready → executing
                              │
                              │ For each agent in shard:
                              │   create agent_tasks row (status=pending)
                              │   notify via Redis pubsub
                              ▼
                       Agents long-poll, claim, execute
                              │
                              │ POST /agent/v1/tasks/{id}/report
                              ▼
                       Update agent_tasks + domain_tasks status
                              │
                              ▼
                       (when all tasks done in shard)
                       TypeReleaseProbeVerify handler  [Phase 3]
                              │
                              ▼
                       (when all shards done)
                       TypeReleaseFinalize handler
                              │
                              │ Transition: executing → succeeded
                              ▼
                       Audit + notify
```

The Phase 1 implementation can flatten this: no sharding (all tasks in one
shard 0), no probe gating, just dispatch → wait → finalize.

---

## 11. Common patterns reference

### Pagination (cursor-based)

```go
type ListOpts struct {
    ProjectID int64
    Cursor    string
    Limit     int
}

func (s *Store) List(ctx context.Context, opts ListOpts) ([]Item, string, error) {
    if opts.Limit == 0 || opts.Limit > 200 { opts.Limit = 50 }

    var afterID int64
    if opts.Cursor != "" {
        afterID = decodeCursor(opts.Cursor)
    }

    const q = `
        SELECT * FROM items
        WHERE project_id = $1 AND id > $2 AND deleted_at IS NULL
        ORDER BY id ASC
        LIMIT $3`

    var items []Item
    if err := s.db.SelectContext(ctx, &items, q, opts.ProjectID, afterID, opts.Limit+1); err != nil {
        return nil, "", err
    }

    nextCursor := ""
    if len(items) > opts.Limit {
        nextCursor = encodeCursor(items[opts.Limit-1].ID)
        items = items[:opts.Limit]
    }
    return items, nextCursor, nil
}
```

### Audit logging

```go
type AuditEntry struct {
    UserID     int64
    Action     string                 // "release.created", "domain.transition.requested→approved"
    TargetKind string                 // "release", "domain", "agent"
    TargetID   string                 // UUID
    Detail     map[string]any
}

func (a *Auditor) LogTx(ctx context.Context, tx *sqlx.Tx, e AuditEntry) error {
    detailJSON, _ := json.Marshal(e.Detail)
    _, err := tx.ExecContext(ctx, `
        INSERT INTO audit_logs (user_id, action, target_kind, target_id, detail)
        VALUES ($1, $2, $3, $4, $5)`,
        e.UserID, e.Action, e.TargetKind, e.TargetID, detailJSON)
    return err
}
```

### Graceful shutdown

```go
// cmd/server/main.go
func main() {
    // ... setup ...

    srv := &http.Server{Addr: ":8080", Handler: router}

    go func() {
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            logger.Fatal("server failed", zap.Error(err))
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    logger.Info("shutting down")

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        logger.Error("forced shutdown", zap.Error(err))
    }
}
```

### Login identifier = username

Per ADR-0001 D1 (preserved through ADR-0003), the login identifier is
`username`, not email. The platform is for internal operators; no email is
collected. Password recovery is out of band (operator manual reset by an admin).

```go
// store/postgres/user.go
const getUserByUsername = `
    SELECT id, uuid, username, password_hash, display_name, status
    FROM users
    WHERE username = $1 AND deleted_at IS NULL`
```

NEVER add a `GetByEmail` method. The frontend login form uses
`username + password`.
