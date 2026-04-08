# TESTING.md — Testing Strategy & Patterns

> **Aligned with PRD + ADR-0003 (2026-04-09).** Reference this when writing
> or modifying tests.

---

## Test File Organization

```
# Unit tests: same directory as source
internal/lifecycle/service.go     → internal/lifecycle/service_test.go
internal/release/service.go       → internal/release/service_test.go
internal/agent/service.go         → internal/agent/service_test.go
internal/artifact/builder.go      → internal/artifact/builder_test.go
pkg/provider/dns/cloudflare.go    → pkg/provider/dns/cloudflare_test.go
api/handler/release.go            → api/handler/release_test.go
store/postgres/lifecycle.go       → store/postgres/lifecycle_test.go

# Integration tests: build-tagged
store/postgres/release_integration_test.go  (//go:build integration)
pkg/storage/minio_integration_test.go       (//go:build integration)
pkg/provider/dns/cloudflare_integration_test.go (//go:build integration)

# Frontend tests
web/src/views/releases/__tests__/ReleaseList.spec.ts
web/src/stores/__tests__/release.spec.ts
web/src/api/__tests__/release.spec.ts
```

---

## Go Unit Tests

### Table-Driven Tests (mandatory pattern)

```go
func TestCanLifecycleTransition(t *testing.T) {
    tests := []struct {
        name string
        from string
        to   string
        want bool
    }{
        {"requested → approved",      "requested",   "approved",   true},
        {"approved → provisioned",    "approved",    "provisioned", true},
        {"provisioned → active",      "provisioned", "active",      true},
        {"active → disabled",         "active",      "disabled",    true},
        {"disabled → active",         "disabled",    "active",      true},
        {"requested → active (skip)", "requested",   "active",      false},
        {"retired → anything",        "retired",     "active",      false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := CanLifecycleTransition(tt.from, tt.to)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### State-Machine Race Tests (mandatory)

Per CLAUDE.md Critical Rule #1, every state machine `Transition*()` method
MUST have a race test that verifies optimistic concurrency. Run with
`-race -count=50`.

```go
// internal/lifecycle/service_test.go
func TestTransition_RaceCondition(t *testing.T) {
    // Integration-flavored test: needs a real Postgres connection
    db := setupTestDB(t)
    s := NewService(db, zap.NewNop())

    domainID := createTestDomain(t, db, "active")
    ctx := context.Background()

    var winners, losers int32
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            err := s.Transition(ctx, domainID, "active", "disabled",
                "race test", "user:test")
            switch {
            case err == nil:
                atomic.AddInt32(&winners, 1)
            case errors.Is(err, ErrLifecycleRaceCondition):
                atomic.AddInt32(&losers, 1)
            }
        }()
    }
    wg.Wait()

    require.Equal(t, int32(1), winners,
        "exactly one Transition must succeed")
    require.Equal(t, int32(9), losers,
        "nine Transitions must fail with ErrLifecycleRaceCondition")
}
```

The same pattern applies to `TransitionRelease` and `TransitionAgent`.

### Mocking Interfaces

```go
// Define mocks in the test file, NOT in a separate mock package.
// Keep mocks minimal — only implement methods under test.

type mockArtifactStore struct {
    putFn func(ctx context.Context, ref ArtifactRef, body io.Reader, meta Manifest) error
    getFn func(ctx context.Context, ref ArtifactRef) (io.ReadCloser, *Manifest, error)
}

func (m *mockArtifactStore) Put(ctx context.Context, ref ArtifactRef, body io.Reader, meta Manifest) error {
    if m.putFn != nil {
        return m.putFn(ctx, ref, body, meta)
    }
    return errors.New("not implemented")
}

func (m *mockArtifactStore) Get(ctx context.Context, ref ArtifactRef) (io.ReadCloser, *Manifest, error) {
    if m.getFn != nil {
        return m.getFn(ctx, ref)
    }
    return nil, nil, errors.New("not implemented")
}

// Usage in test
func TestReleaseService_Plan(t *testing.T) {
    artifacts := &mockArtifactStore{
        putFn: func(_ context.Context, _ ArtifactRef, _ io.Reader, _ Manifest) error {
            return nil
        },
    }
    svc := NewService(artifacts, nil, nil, zap.NewNop())

    err := svc.Plan(context.Background(), 42)
    assert.NoError(t, err)
}
```

### Testing HTTP Handlers

```go
func TestReleaseHandler_List(t *testing.T) {
    gin.SetMode(gin.TestMode)

    svc := &mockReleaseService{
        listFn: func(_ context.Context, projectID int64) ([]ReleaseResponse, error) {
            return []ReleaseResponse{
                {UUID: "uuid-1", ReleaseID: "rel_001", Status: "succeeded"},
                {UUID: "uuid-2", ReleaseID: "rel_002", Status: "executing"},
            }, nil
        },
    }
    handler := NewReleaseHandler(svc, zap.NewNop())

    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    c.Request = httptest.NewRequest("GET", "/api/v1/releases?project_id=1", nil)

    handler.List(c)

    assert.Equal(t, http.StatusOK, w.Code)

    var resp Response
    err := json.Unmarshal(w.Body.Bytes(), &resp)
    assert.NoError(t, err)
    assert.Equal(t, 0, resp.Code)
}
```

### Reproducibility Tests for Artifact Builder (mandatory)

Per CLAUDE.md Critical Rule #2 and ARCHITECTURE.md §2.4, the artifact builder
MUST be deterministic. The same input produces byte-identical output.

```go
// internal/artifact/builder_test.go
func TestBuilder_Build_Deterministic(t *testing.T) {
    b := newTestBuilder(t)
    req := BuildRequest{
        TemplateVersionID: 42,
        DomainIDs:         []int64{1, 2, 3},
        ProjectSlug:       "test-project",
        ReleaseID:         "rel_test_001",
        ReleaseType:       "html",
        UserID:            1,
    }

    art1, err := b.Build(context.Background(), req)
    require.NoError(t, err)

    art2, err := b.Build(context.Background(), req)
    require.NoError(t, err)

    require.Equal(t, art1.Checksum, art2.Checksum,
        "two builds with the same input must produce the same checksum")
    require.Equal(t, art1.Manifest, art2.Manifest,
        "manifests must be byte-identical")
}
```

If this test fails, find and remove the source of nondeterminism (random ID
in content, timestamp embedded in HTML, map iteration order, sleep). The
manifest may contain timestamps; the *content files* must not.

### Testing Template Rendering

```go
func TestRenderHTMLTemplate(t *testing.T) {
    src := `<html><meta name="release-version" content="{{ .ReleaseVersion }}"><body>{{ .Title }}</body></html>`

    rendered, err := renderTemplate(src, map[string]any{
        "ReleaseVersion": "art_test_001",
        "Title":          "Hello",
    })
    require.NoError(t, err)
    assert.Contains(t, rendered, `content="art_test_001"`)
    assert.Contains(t, rendered, `<body>Hello</body>`)
}
```

### Testing the Agent Whitelist (CLAUDE.md Rule #3)

```go
// cmd/agent/handler_test.go
func TestHandler_RejectsUnknownTaskType(t *testing.T) {
    a := newTestAgent(t)

    env := &agentprotocol.TaskEnvelope{
        TaskID: "task_001",
        Type:   "shell_exec",   // not in the whitelist
    }
    err := a.handleTask(context.Background(), env)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "unknown task type")
}
```

The structural check (`make check-agent-safety`) is the primary defense;
this test is the secondary one.

---

## Go Integration Tests

Tag with `//go:build integration` — requires running PostgreSQL, Redis, MinIO.

```go
//go:build integration

func TestArtifactStorage_Integration(t *testing.T) {
    storage := setupTestMinIO(t)
    t.Cleanup(func() { cleanupBucket(storage) })

    workdir := writeTestArtifactTree(t)
    uri, err := storage.UploadDir(context.Background(), workdir, "test-project/rel_test_001/")
    require.NoError(t, err)
    require.NotEmpty(t, uri)

    info, err := storage.Stat(context.Background(), ArtifactRef{URI: uri + "manifest.json"})
    require.NoError(t, err)
    require.Greater(t, info.Size, int64(0))
}
```

Run: `go test -tags=integration ./... -v`

---

## Frontend Tests (Vitest)

### Component Tests

```typescript
// web/src/views/releases/__tests__/ReleaseList.spec.ts
import { mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import ReleaseList from '../ReleaseList.vue'

describe('ReleaseList', () => {
    it('renders release table', async () => {
        const wrapper = mount(ReleaseList, {
            global: {
                plugins: [createTestingPinia({
                    initialState: {
                        release: {
                            releases: [
                                { uuid: '1', release_id: 'rel_001', status: 'succeeded' },
                                { uuid: '2', release_id: 'rel_002', status: 'executing' },
                            ]
                        }
                    }
                })],
            },
            props: { projectId: 1 },
        })

        expect(wrapper.text()).toContain('rel_001')
        expect(wrapper.text()).toContain('rel_002')
    })
})
```

### Store Tests

```typescript
// web/src/stores/__tests__/release.spec.ts
import { setActivePinia, createPinia } from 'pinia'
import { useReleaseStore } from '../release'
import { vi } from 'vitest'

vi.mock('@/api/release', () => ({
    releaseApi: {
        list: vi.fn().mockResolvedValue({
            data: { items: [{ uuid: '1', release_id: 'rel_001', status: 'succeeded' }], total: 1 }
        })
    }
}))

describe('Release Store', () => {
    beforeEach(() => setActivePinia(createPinia()))

    it('fetches releases', async () => {
        const store = useReleaseStore()
        await store.fetchByProject(1)
        expect(store.releases).toHaveLength(1)
        expect(store.releases[0].release_id).toBe('rel_001')
    })
})
```

---

## Coverage Requirements

| Scope | Minimum Coverage | Notes |
|-------|-----------------|-------|
| internal/lifecycle, release, agent | **90%** | State machines are safety-critical |
| internal/artifact | **85%** | Reproducibility test mandatory |
| internal/* (other business logic) | 80% | Core value — must be well-tested |
| pkg/provider/ (DNS adapters) | 60% | Interface compliance + error paths |
| pkg/agentprotocol/ | 80% | Wire format = contract |
| pkg/storage/ | 60% (unit) | Verified end-to-end by integration |
| api/handler/ | 70% | Request parsing + error responses |
| store/ | 50% (unit) | SQL correctness verified by integration |
| **cmd/agent/** | **80%** | Safety boundary verification |
| web/src/stores/ | 70% | State management logic |
| web/src/views/ | 50% | Basic render + interaction |

---

## CI Gates (mandatory in addition to tests)

Per CLAUDE.md Critical Rule #1 and #3:

```bash
make check-lifecycle-writes     # exactly one match in store/postgres/lifecycle.go
make check-release-writes       # exactly one match in store/postgres/release.go
make check-agent-writes         # exactly one match in store/postgres/agent.go
make check-agent-safety         # zero violations in cmd/agent/
```

Any PR breaking any of these must be fixed before merge — never bypassed.

---

## Test Commands

```bash
# Run all unit tests (with -race)
make test
# Equivalent to: go test ./... -race -timeout 60s

# Run with coverage
go test ./... -race -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run integration tests (requires Docker services)
go test -tags=integration ./... -v

# Run a specific state machine test with stress
go test -race -count=50 ./internal/lifecycle/ -run TestTransition_RaceCondition -v

# Run frontend tests
cd web && npm run test
# Equivalent to: vitest run

# Run frontend tests with coverage
cd web && npm run test:coverage

# Lint
make lint
# Equivalent to: golangci-lint run ./... && cd web && npx eslint src/

# CI gates (run before push)
make check-lifecycle-writes && make check-release-writes && \
  make check-agent-writes && make check-agent-safety
```
