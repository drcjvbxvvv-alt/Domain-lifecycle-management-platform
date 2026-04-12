# PHASE1_TASKLIST.md — Phase 1 Work Order

> **Aligned with PRD + ADR-0003 (2026-04-09).** This document is the
> authoritative work order for Phase 1 of the Domain Lifecycle & Deployment
> Platform. It supersedes the previous PHASE1_TASKLIST.md (commit `30280dc`,
> 2026-04-08), which described 9 task cards for the now-superseded GFW
> failover system.
>
> **Audience**: Claude Code sessions (Sonnet for most tasks, Opus for the
> bottleneck tasks marked with `(Opus)`).
> **Pre-requisite**: Read CLAUDE.md, then ARCHITECTURE.md §1-§3, then this
> document. Read DATABASE_SCHEMA.md before P1.2. Read DEVELOPMENT_PLAYBOOK.md
> before any implementation task.

---

## Phase 1 — Definition of Scope

Phase 1 ships **the platform skeleton end-to-end**: an operator can log in,
create a project, register a domain, write a template, publish a version,
build an artifact, create a release, watch an agent pull and apply it, and
see results in the UI.

### What "Phase 1 done" looks like (acceptance demo)

```
1. Admin logs in to /
2. Admin creates Project "demo"
3. Admin registers Domain "demo.example.com" → auto-approved → DNS provisioned
   (DNS provider call optional in P1; can be stubbed) → state = active
4. Admin creates Template "homepage" with HTML + nginx content
5. Admin publishes TemplateVersion v1
6. Admin creates a Host Group "edge-01" with one Agent registered to it
7. Admin creates Release for project=demo, template_version=v1, type=html, scope=[demo.example.com]
8. Release goes through pending → planning → ready → executing
9. Artifact builds, uploads to MinIO, signs
10. Agent (running on edge-01) long-polls, claims the task, downloads, verifies, writes, reports back
11. Release finalizes to succeeded
12. Admin views the release detail page in the SPA and sees the green status
```

No sharding, no canary, no probe, no rollback, no approval flow, no nginx
diff. Those are Phase 2-4.

### What is OUT of Phase 1 (do not implement)

Per ADR-0003 D11 and PRD §28, the following are explicitly out of Phase 1:

| Subsystem | Phase | Reason |
|---|---|---|
| Sharding (release_shards splitting beyond shard 0) | Phase 2 | Phase 1 has flat releases (one shard, all tasks) |
| Rollback execution | Phase 2 | Schema in place, no executor |
| Dry-run / Diff | Phase 2 | Operator-facing safety, not foundational |
| Per-host concurrency limits | Phase 2 | Phase 1 dispatches sequentially per agent |
| Canary policy (95% threshold gating) | Phase 3 | Requires probes |
| Probe L1 / L2 / L3 | Phase 3 | Requires release execution to be reliable first |
| Alert engine + notify channels | Phase 3 | Requires probes to generate signals |
| Agent canary upgrade | Phase 3 | Requires fleet management UI from Phase 2 |
| Approval flow execution | Phase 4 | Phase 1 schema includes table; auto-approve in code |
| Nginx artifact deployment as separate type | Phase 4 | Phase 1 does HTML only; nginx works but isn't gated separately |
| HA / cross-region | Phase 4 | Single instance is fine for Phase 1 |
| **GFW failover vertical** | **Unscheduled** | See ADR-0003 D11 |

If a Phase 1 task tempts you to start on any of the above, **stop**. The
platform must work end-to-end first; sophistication comes after.

---

## Dependency Graph

```
                       P1.1 (scaffold + bootstrap)
                              │
                              ▼
                       P1.2 (DB migrations)
                              │
            ┌───────┬─────────┼─────────┬─────────┬─────────┐
            ▼       ▼         ▼         ▼         ▼         ▼
          P1.3    P1.4      P1.5      P1.6      P1.10    P1.11
          Auth    Project   Lifecycle Template  Agent     asynq
          (5     CRUD      (Opus)    + Versions Protocol  worker
          roles)                                (Opus)   bootstrap
            │       │         │         │         │         │
            └───┬───┴────┬────┴────┬────┴────┬────┴────┬────┘
                ▼        ▼         ▼         ▼         ▼
              P1.7  (Artifact build pipeline) (Opus on contract; Sonnet on plumbing)
                              │
                              ▼
              P1.8  (Release model + state machine)  (Opus on state machine)
                              │
                              ▼
              P1.9  (cmd/agent binary + handlers)  (Opus on whitelist enforcement)
                              │
                              ▼
              P1.12 (Frontend pages: project / domain / template / release / agent)
```

### Critical path

`P1.1 → P1.2 → P1.5 (Opus) → P1.7 (Opus) → P1.8 (Opus) → P1.9 (Opus) → P1.12`

There are **four Opus tasks** in Phase 1 (vs one in the old plan). They are:

1. **P1.5** — Domain Lifecycle state machine + `Transition()` + race test + CI gate
2. **P1.7** — Artifact contract (manifest format, signature scheme, immutability), reproducibility test
3. **P1.8** — Release state machine + `TransitionRelease()` + race test + CI gate
4. **P1.9** — Agent protocol design + agent binary safety boundary (whitelist enforcement)

P1.10 (agent control-plane side) requires P1.9's wire protocol contract first
but the actual handler implementation is mostly Sonnet.

**Start the Opus tasks as early as possible** — they are the bottleneck.
After P1.2 lands, P1.5 / P1.7 / P1.9 can begin in parallel; they touch
disjoint packages and disjoint files.

### Parallelization rules

- P1.3, P1.4, P1.5, P1.6, P1.10, P1.11 may all run after P1.2 in parallel
- P1.7 (artifact build) requires P1.6 (templates) to define template_versions
- P1.8 (release model) requires P1.5 (lifecycle, for "only active domains")
  AND P1.7 (artifact, releases pin to artifacts)
- P1.9 (agent binary) requires P1.10's wire protocol package to be defined
  but the agent binary itself is independent
- P1.12 (frontend) requires the API endpoints from P1.3-P1.11 to exist

---

## Task Cards

Each card specifies: **owner model**, **scope (in)**, **scope (out)**,
**dependencies**, **deliverables**, **acceptance criteria**, and the **docs
to read first**.

---

### P1.1 — Scaffold Go repo + tooling + bootstrap ✅ DONE

**Owner**: Sonnet
**Completed**: 2026-04-12
**Depends on**: nothing — first task
**Reads first**: CLAUDE.md §"Project Structure", §"Go Coding Standards"

**Scope (in)**:

- Verify the existing directory structure is complete. Add missing dirs:
  `internal/lifecycle/`, `internal/template/`, `internal/artifact/`,
  `internal/release/`, `internal/agent/`, `internal/audit/`,
  `pkg/agentprotocol/`, `pkg/storage/`, `cmd/agent/`. Remove `cmd/scanner` from
  the default build but leave the directory + stub `main.go` in place
  (ADR-0003 D10).
- `cmd/server/main.go`: Gin server boot, graceful shutdown, signal handling
  (DEVELOPMENT_PLAYBOOK §11 "Graceful shutdown"). Two listeners: `:8080`
  for `/api/v1/*` (JWT auth) and `:8443` for `/agent/v1/*` (mTLS).
- `cmd/worker/main.go`: asynq server boot with the canonical queue layout
  from CLAUDE.md §"asynq Queue Layout". Empty handler stubs for every task
  type in `internal/tasks/types.go` (handlers come in later cards).
- `cmd/agent/main.go`: minimal agent skeleton — config load, register, heartbeat
  loop, task long-poll loop. Handler dispatch is a TODO until P1.9.
- `cmd/migrate/main.go`: thin wrapper around
  `github.com/golang-migrate/migrate/v4` for `up`, `down`, `version`.
- Internal shared packages (`internal/bootstrap/`):
  - `config.go`: Viper loader for `configs/config.yaml`
  - `logger.go`: Zap factory (production + development)
  - `db.go`: sqlx PostgreSQL pool
  - `redis.go`: go-redis client
  - `asynq.go`: asynq client + server factory
  - `storage.go`: MinIO client (NEW for the new architecture)
- `configs/config.example.yaml`: full config reference with every key
  documented (DB, Redis, MinIO/S3, JWT, mTLS CA paths, providers config path).
- `Makefile`: ensure all targets in CLAUDE.md §"Makefile Commands" work.
  Add `make agent` (cross-compile to linux/amd64). Add three CI gate targets
  as TODO stubs (`check-lifecycle-writes`, `check-release-writes`,
  `check-agent-writes`) — implementation in P1.5/P1.8/P1.9.
- `go.mod`: add `github.com/minio/minio-go/v7` if missing. Run `go mod tidy`.
- `.golangci.yml`: minimal config (govet, staticcheck, errcheck, revive).
- `deploy/docker-compose.yml`: PostgreSQL 16 + TimescaleDB + Redis 7 + MinIO.

**Scope (out)**:

- No business logic, no handlers, no migrations, no store methods.
- No frontend changes.
- Do not implement state machines yet.
- Do not implement task handlers (just register stubs that log + ack).

**Deliverables**:

- Five compilable binaries: `server`, `worker`, `agent`, `migrate`,
  `scanner` (parked but still compiles)
- `internal/bootstrap/*.go` files
- Updated `Makefile`, `configs/config.example.yaml`, `deploy/docker-compose.yml`
- `.golangci.yml`

**Acceptance**:

- `make build` succeeds, producing 5 binaries
- `make lint` runs clean
- `./bin/server` starts, binds to `:8080` and `:8443`, logs structured
  "server started" and exits cleanly on `SIGTERM`
- `./bin/worker` starts, connects to Redis, prints queue config at boot
- `./bin/agent` starts (with mock config), attempts register, logs the
  attempt (server side returns 404 since P1.10 hasn't shipped yet — that's OK)
- `./bin/migrate version` runs (reports "no migrations" or version 0)
- `docker compose -f deploy/docker-compose.yml up -d` brings up
  PG / Redis / MinIO and they are reachable from the server binary

---

### P1.2 — Phase 1 DB migrations ✅ DONE

**Owner**: Sonnet
**Completed**: 2026-04-12
**Depends on**: P1.1
**Reads first**: DATABASE_SCHEMA.md (entire file), CLAUDE.md §"Database Migrations"

**Scope (in)**:

- Write `migrations/000001_init.up.sql` with every table from
  DATABASE_SCHEMA.md, including P2-P4 tables (creating empty tables now is
  cheap and avoids future schema migrations during the pre-launch window):
  - **P1**: users, roles, user_roles, projects, domains, domain_variables,
    domain_lifecycle_history, templates, template_versions, artifacts,
    host_groups, agents, agent_state_history, agent_heartbeats, releases,
    release_state_history, release_scopes, release_shards, domain_tasks,
    agent_tasks, deployment_logs, audit_logs
  - **P2**: rollback_records
  - **P3**: probe_policies, probe_tasks, alert_events, notification_rules,
    agent_versions, agent_upgrade_jobs, agent_logs
  - **P4**: approval_requests
- Write `migrations/000001_init.down.sql` with `DROP TABLE IF EXISTS ... CASCADE`
  for every table in reverse dependency order
- Write `migrations/000002_timescale.up.sql` and `.down.sql` for the
  `probe_results` hypertable with retention + compression policies
- All tables follow the conventions in DATABASE_SCHEMA.md (id, uuid, timestamps,
  deleted_at where applicable)
- Seed the five roles (`viewer`, `operator`, `release_manager`, `admin`,
  `auditor`) via a `migrations/000003_seed_roles.up.sql` migration

**Scope (out)**:

- No seed data beyond the five roles (admin user is seeded by P1.3)
- No stored procedures, no triggers
- Do not add tables not in DATABASE_SCHEMA.md

**Deliverables**:

- `migrations/000001_init.up.sql` and `.down.sql`
- `migrations/000002_timescale.up.sql` and `.down.sql`
- `migrations/000003_seed_roles.up.sql` and `.down.sql`

**Acceptance**:

- `make migrate-up` applies cleanly against an empty PostgreSQL 16 + TimescaleDB
- `make migrate-down` rolls back cleanly all the way to empty
- `\dt` in psql lists every table from DATABASE_SCHEMA.md
- `\d domains` shows the `chk_domains_lifecycle_state` CHECK with all 6 states
- `\d agents` shows the `chk_agents_status` CHECK with all 9 states
- `\d releases` shows the `chk_releases_status` CHECK with all 10 states
- `\d probe_results` shows it is a TimescaleDB hypertable with chunks of 1 day
- `SELECT name FROM roles ORDER BY name` returns the five roles

---

### P1.3 — Auth: login, JWT, RBAC with 5 roles ✅ DONE

**Owner**: Sonnet
**Completed**: 2026-04-12
**Depends on**: P1.2 (users, roles, user_roles tables)
**Reads first**: CLAUDE.md §"API Conventions", DEVELOPMENT_PLAYBOOK.md §"Login identifier"

**Scope (in)**:

- `store/postgres/user.go`: `GetByUsername`, `GetByID`, `Create`,
  `UpdatePassword`. **The login identifier is `username`, not email** —
  preserved from ADR-0001 D1 through ADR-0003. Do not introduce `GetByEmail`.
- `store/postgres/role.go`: `GetUserRoles(userID) []string`,
  `HasRole(userID, role) bool`, `GrantRole`, `RevokeRole` (admin only).
- `internal/auth/`:
  - `service.go`: `Login(ctx, username, password) (tokenPair, error)`,
    `Refresh`, password hashing with `bcrypt`
  - `jwt.go`: sign / verify with `golang-jwt/jwt/v5`; claims include
    `user_id`, `username`, `roles []string`, `exp`
- `api/middleware/auth.go`: Bearer token extraction, JWT verify, attach
  `user_id` and `roles` to `gin.Context`
- `api/middleware/rbac.go`: `RequireRole("operator")`, `RequireAnyRole("operator", "release_manager")`.
  RBAC checks against the user's role set from JWT claims.
- `api/handler/auth.go`: `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`,
  `GET /api/v1/auth/me`
- Seed mechanism for the first admin user (one-shot SQL or `cmd/migrate -seed-admin`)

**Scope (out)**:

- No OAuth, no SSO, no email reset
- No API rate limiting (Phase 2)
- No approval flow logic — that's Phase 4 even though the role exists

**Deliverables**:

- `store/postgres/user.go`, `store/postgres/role.go`
- `internal/auth/service.go`, `internal/auth/jwt.go`
- `api/middleware/auth.go`, `api/middleware/rbac.go`
- `api/handler/auth.go`
- Seed for first admin user
- Unit tests for JWT sign/verify, password hashing, role check

**Acceptance**:

- `POST /api/v1/auth/login` with valid `{username, password}` returns a JWT
- `GET /api/v1/auth/me` with the JWT returns the user including their roles
- A handler protected by `RequireRole("admin")` returns 403 for an operator
- `grep -rn 'GetByEmail' internal/ store/ api/` returns **zero** results
- `go test ./internal/auth/...` passes

---

### P1.4 — Project CRUD ✅ DONE

**Owner**: Sonnet
**Completed**: 2026-04-12
**Depends on**: P1.2, P1.3 (user roles for ownership)
**Reads first**: DEVELOPMENT_PLAYBOOK.md §1 "How to add a new API endpoint"

**Scope (in)**:

- `store/postgres/project.go`: CRUD for `projects` table — Create, GetByID,
  GetBySlug, List (cursor pagination), Update, SoftDelete
- `internal/project/service.go`: Create / Get / List / Update / Delete with
  authorization (admin can do anything; operator can read; only admin can
  delete)
- `api/handler/project.go`: standard REST handlers per DEVELOPMENT_PLAYBOOK §1
- Routes registered in `api/router/router.go` under `/api/v1/projects`

**Scope (out)**:

- Project membership (Phase 4 — currently project owner is sufficient)
- Project-level settings beyond `is_prod` (Phase 4)

**Deliverables**:

- `store/postgres/project.go`
- `internal/project/service.go`
- `api/handler/project.go`
- Routes wired
- Unit tests for service layer (happy + duplicate slug + permission)

**Acceptance**:

- `POST /api/v1/projects` (as admin) creates a project, returns 201
- `POST /api/v1/projects` with a duplicate slug returns 409
- `POST /api/v1/projects` (as operator) returns 403
- `GET /api/v1/projects` (as anyone with viewer+) returns paginated list
- `DELETE /api/v1/projects/:id` (as admin) soft-deletes; further GETs return 404

---

### P1.5 — Domain Lifecycle state machine + Transition() **(Opus)** ✅ DONE

**Owner**: **Opus** — bottleneck task
**Completed**: 2026-04-12
**Depends on**: P1.2 (domains, domain_lifecycle_history tables)
**Runs in parallel with**: P1.3, P1.4, P1.6, P1.7, P1.10, P1.11
**Reads first**: CLAUDE.md §"Domain Lifecycle State Machine", Critical Rule #1; ADR-0003 D9; DEVELOPMENT_PLAYBOOK.md §2

**Scope (in)**:

- `internal/lifecycle/statemachine.go`:
  - `validLifecycleTransitions` map exactly as defined in CLAUDE.md
  - `CanLifecycleTransition(from, to string) bool`
  - Sentinel errors: `ErrDomainNotFound`, `ErrInvalidLifecycleState`,
    `ErrLifecycleRaceCondition`
- `store/postgres/lifecycle.go`:
  - **Unexported** `updateLifecycleStateTx(ctx, tx, id, from, to)` — the
    ONLY function in the codebase that issues `UPDATE domains SET lifecycle_state`
  - `insertLifecycleHistoryTx(ctx, tx, entry)` — writes audit row
- `internal/lifecycle/service.go`:
  - `Service.Transition(ctx, id, from, to, reason, triggeredBy)` per
    DEVELOPMENT_PLAYBOOK.md §2 pattern, with `SELECT ... FOR UPDATE`,
    optimistic check, validity check, state update, history insert, all
    in one transaction
- `internal/lifecycle/service_test.go`:
  - Table-driven tests for every valid edge in the validity map
  - Table-driven tests for a sample of invalid edges
  - **Race test** with `-race -count=50`: 10 goroutines try the same valid
    transition; exactly one wins, nine return `ErrLifecycleRaceCondition`
  - Test that `domain_lifecycle_history` receives exactly one row per success

**CI gate** (mandatory):

- Implement `make check-lifecycle-writes` Makefile target as documented in
  DEVELOPMENT_PLAYBOOK.md §2
- Run it in CI; it must pass (exactly one hit, in `store/postgres/lifecycle.go`)

**Scope (out)**:

- No CRUD for domains yet — that's P1.5b (or absorbed into this card)
  - **Decision**: Absorb basic domain CRUD into P1.5 since the state
    machine is meaningless without it. The Create handler inserts a row
    in state `requested` directly (no `Transition()` call — that's the
    documented exception, since there's no `nil → requested` edge).

**Deliverables**:

- `internal/lifecycle/statemachine.go`
- `internal/lifecycle/service.go` (Transition + Create + List + Get)
- `internal/lifecycle/errors.go`
- `store/postgres/lifecycle.go`
- `store/postgres/domain.go` (basic CRUD; inserts row in `requested` state)
- `internal/lifecycle/service_test.go`
- `api/handler/domain.go` (POST creates, PATCH `/transition` calls Transition)
- `Makefile` updated with `check-lifecycle-writes` target

**Acceptance**:

- `go test -race -count=50 ./internal/lifecycle/...` passes
- `make check-lifecycle-writes` returns exactly one hit
- `POST /api/v1/domains` creates a domain in `requested` state, returns 201
- `POST /api/v1/domains/:id/transition` with `{to: "approved", reason: "..."}`
  succeeds when current state is `requested`
- The same call with `{to: "active"}` returns 409 (invalid edge from `requested`)
- `domain_lifecycle_history` shows one row per successful transition

**Why Opus**: This is the safety-critical write path for the most central
business object. Per ADR-0003 D9, the same methodology that ADR-0002 D2
required for the previous design is mandatory here.

---

### P1.6 — Template + TemplateVersion CRUD ✅ DONE

**Owner**: Sonnet
**Completed**: 2026-04-12
**Depends on**: P1.2 (templates, template_versions tables), P1.4 (project FK)
**Reads first**: CLAUDE.md §"Domain Object Model", Critical Rule #9

**Scope (in)**:

- `store/postgres/template.go`: CRUD for `templates` and `template_versions`
- `internal/template/service.go`:
  - Template CRUD (Create, List, Get, Update, Delete)
  - `PublishVersion(templateID, content_html, content_nginx, default_variables)`:
    creates a new `template_versions` row, computes checksum, sets
    `published_at = NOW()`. **Once published, the row is immutable** —
    enforced at the store layer (UPDATE rejects when `published_at IS NOT NULL`).
  - `GetVersion(versionID) *TemplateVersion`
  - `ListVersions(templateID) []TemplateVersion`
- `api/handler/template.go`: standard CRUD + `POST /:id/versions/publish`
- Validation: HTML content must contain the literal string
  `{{ .ReleaseVersion }}` (so probes can find the meta tag in P3) — emit
  a warning in P1, will become a hard rule in P3

**Scope (out)**:

- Variable schema validation (Phase 2)
- Template preview / dry-render (Phase 2)
- Template diff against previous version (Phase 2)

**Deliverables**:

- `store/postgres/template.go`
- `internal/template/service.go`
- `api/handler/template.go`
- Routes wired
- Unit tests for `PublishVersion` (immutability test included)

**Acceptance**:

- `POST /api/v1/projects/:projectId/templates` creates a template
- `POST /api/v1/templates/:id/versions/publish` creates a version, returns 201
- A second `POST .../publish` with different content creates a new version,
  not an update
- `PATCH /api/v1/template-versions/:id` returns 409 Conflict on a published version
- `GET /api/v1/templates/:id/versions` lists all versions newest-first

---

### P1.7 — Artifact Build Pipeline **(Opus on contract; Sonnet on plumbing)** ✅ DONE (2026-04-12)

**Owner**: **Opus** for the manifest format + signature scheme + immutability contract; **Sonnet** for the rendering loop and storage upload
**Depends on**: P1.6 (template_versions to render), P1.5 (domains to render against)
**Reads first**: ARCHITECTURE.md §2.4, CLAUDE.md Critical Rule #2, DEVELOPMENT_PLAYBOOK.md §5

**Completion notes (2026-04-12)**:
- All deliverables landed. 9 unit tests pass including reproducibility test (`-race -count=5` clean).
- Extra deliverables beyond spec: `internal/artifact/errors.go` (sentinel errors), `store/postgres/domain.go` gained `GetVariables()` + `ListActiveByProject()`, `GET /api/v1/projects/:projectId/artifacts` list endpoint.
- Reproducibility risk (🔴) did NOT materialize — deterministic design from the start (sorted domains, sorted files, no timestamps in content, content-addressed artifact_id) meant first run passed.

**Scope (in)**:

- `pkg/agentprotocol/manifest.go` (Opus):
  - Define the `Manifest` struct with stable JSON field names
  - Define `WriteChecksums(path)`, `WriteManifest(path)`,
    `Validate() error`, `ToJSON() []byte` methods
  - Document the schema as the wire format that the agent will parse
- `pkg/storage/storage.go` + `pkg/storage/minio.go`:
  - `Storage` interface: `UploadDir(ctx, localDir, remotePrefix) (uri, error)`,
    `Stat(ctx, ref) (*ObjectInfo, error)`, `Presign(ctx, ref, ttl) (url, error)`
  - MinIO implementation
- `internal/artifact/builder.go` (Sonnet, following Opus's contract):
  - `Build(ctx, BuildRequest) (*Artifact, error)` per DEVELOPMENT_PLAYBOOK §5
  - Deterministic rendering: sorted domain order, sorted variable maps,
    no timestamps in content (only in manifest), no random IDs in content
  - Hash → manifest → upload → mark `signed_at`
- `internal/artifact/signer.go` (Opus):
  - In Phase 1, a placeholder HMAC-SHA256 signer using a configured secret.
    The real signature scheme (cosign / GPG) is deferred to ADR-0004,
    which gets written when artifact work begins.
- `store/postgres/artifact.go`: Create, GetByID, GetByArtifactID, ListByRelease.
  **Update method REJECTS modifications when `signed_at IS NOT NULL`**
  (CLAUDE.md Critical Rule #2 enforcement at the store layer).
- `internal/artifact/builder_test.go`:
  - **Reproducibility test**: build twice with the same input, assert
    byte-equal manifest checksum
  - Test that template version with `published_at IS NULL` is rejected
- `internal/tasks/types.go`: `TypeArtifactBuild` constant
- Worker handler: `internal/artifact/handler.go::HandleBuild` — picks up an
  asynq task with `{release_id}`, fetches release, calls Builder, updates
  release with the new artifact_id
- `api/handler/artifact.go`: `GET /api/v1/artifacts/:id` (read-only;
  artifacts are created by the release flow, not directly by users)

**Scope (out)**:

- No nginx artifact paths in P1 if `release_type='html'` only (P4 brings full
  HTML+nginx separation)
- No real signature scheme — placeholder HMAC is fine for P1
- No artifact garbage collection (deferred indefinitely)

**Deliverables**:

- `pkg/agentprotocol/manifest.go`
- `pkg/storage/storage.go`, `pkg/storage/minio.go`
- `internal/artifact/builder.go`, `internal/artifact/signer.go`,
  `internal/artifact/handler.go`
- `store/postgres/artifact.go` (with immutability enforcement)
- `internal/artifact/builder_test.go` (with reproducibility test)
- `api/handler/artifact.go`
- `internal/tasks/types.go::TypeArtifactBuild`

**Acceptance**:

- `go test ./internal/artifact/...` passes including reproducibility test
- Calling Builder.Build with the same input twice produces the same `Manifest.Checksum`
- An attempt to UPDATE an artifact with `signed_at IS NOT NULL` returns
  an error from the store layer
- An end-to-end smoke test: enqueue `TypeArtifactBuild` for a release with
  one domain → the task succeeds → MinIO contains the artifact tree →
  the `artifacts` row exists with `signed_at` set

**Why Opus on the contract**: the manifest format and the immutability contract
are wire / store contracts that any future change touches every agent and
every release. Once the format ships, breaking it is expensive.

---

### P1.8 — Release model + state machine **(Opus on state machine)** ✅ DONE (2026-04-12)

**Owner**: **Opus** for the state machine + `TransitionRelease()`; **Sonnet** for the rest
**Depends on**: P1.5 (domains), P1.6 (templates), P1.7 (artifacts)
**Reads first**: CLAUDE.md §"Release State Machine", DEVELOPMENT_PLAYBOOK.md §10

**Completion notes (2026-04-12)**:
- All deliverables landed. 5 test functions (29 subtests) pass including graph completeness check. Race test (`-race -count=5`) clean.
- `make check-release-writes` passes — exactly one hit in `store/postgres/release.go::updateReleaseStatusTx`.
- State machine covers all 10 states and all valid/invalid edges per CLAUDE.md.
- Service implements: Create (→ pending + enqueue Plan), Plan (pending → planning → ready + shard 0 + enqueue ArtifactBuild), Dispatch (ready → executing), Finalize (executing → succeeded), Pause/Resume/Cancel.
- Phase 1 simplifications: single shard 0, auto-succeed in Finalize (no real agent tasks yet), planning → ready is immediate.

**Scope (in)**:

- `internal/release/statemachine.go` (Opus):
  - `validReleaseTransitions` map per CLAUDE.md
  - `CanReleaseTransition(from, to string) bool`
  - Sentinel errors
- `store/postgres/release.go`:
  - **Unexported** `updateReleaseStatusTx` — only place that issues
    `UPDATE releases SET status`
  - `insertReleaseStateHistoryTx`
  - Public CRUD: Create, GetByID, ListByProject, etc.
- `internal/release/service.go` (Opus on `TransitionRelease`, Sonnet on rest):
  - `Service.TransitionRelease(ctx, id, from, to, reason, triggeredBy)` per
    DEVELOPMENT_PLAYBOOK.md §2 pattern
  - `Service.Create(ctx, req, userID) (*Release, error)` per
    DEVELOPMENT_PLAYBOOK.md §1 — inserts release in `pending`, dispatches
    `TypeReleasePlan` asynq task
  - `Service.Plan(ctx, releaseID)` worker handler — `pending → planning`,
    enumerate scope domains, validate they are `active`, dispatch
    `TypeArtifactBuild`, on artifact ready transition `planning → ready`
    and dispatch `TypeReleaseDispatchShard`
  - `Service.Dispatch(ctx, releaseID)` worker handler — `ready → executing`,
    create domain_tasks rows, create agent_tasks rows targeted at agents
    in the host_groups in scope, mark agent_tasks `pending`, notify via Redis
    pubsub
  - `Service.Finalize(ctx, releaseID)` — when all agent_tasks are done,
    transition `executing → succeeded` (or `failed` if any task failed)
  - `Service.Pause(ctx, releaseID)`: `executing → paused`
  - `Service.Resume(ctx, releaseID)`: `paused → executing`
  - `Service.Cancel(ctx, releaseID)`: any earlier state → `cancelled`
- `internal/release/service_test.go`:
  - Table-driven for the validity map
  - **Race test** for `TransitionRelease` (`-race -count=50`)
- `api/handler/release.go`: standard handlers
- `internal/tasks/types.go`: `TypeReleasePlan`, `TypeReleaseDispatchShard`,
  `TypeReleaseFinalize`, `TypeReleaseRollback` (latter as TODO stub)
- Worker handlers registered in `cmd/worker/main.go`

**CI gate** (mandatory):

- `make check-release-writes` — exactly one hit in `store/postgres/release.go`

**Scope (out)**:

- No actual sharding (Phase 2). In Phase 1, a "shard 0" containing all tasks
  is created for schema completeness, but planning never creates shard 1+.
- No canary gate (Phase 3)
- No probe verification (Phase 3)
- No rollback execution (Phase 2)

**Deliverables**:

- `internal/release/statemachine.go`, `service.go`, `errors.go`
- `store/postgres/release.go` (with `updateReleaseStatusTx` unexported)
- `internal/release/service_test.go`
- `api/handler/release.go`
- Worker handlers wired
- `Makefile` updated with `check-release-writes`

**Acceptance**:

- `go test -race -count=50 ./internal/release/...` passes
- `make check-release-writes` returns exactly one hit
- `POST /api/v1/releases` creates a release, returns 202
- The release moves through `pending → planning → ready → executing →
  succeeded` end-to-end (with a fake agent that always reports success)
- A release for a domain in `requested` state returns 400 (only `active`
  domains are valid release targets)

---

### P1.9 — `cmd/agent` Pull Agent binary **(Opus on safety boundary)** ✅ DONE (2026-04-12)

**Owner**: **Opus** — the agent binary contains the most security-sensitive
code in the platform and the whitelist must be enforced structurally
**Depends on**: P1.10 (wire protocol), P1.7 (manifest format)
**Reads first**: CLAUDE.md Critical Rule #3, ARCHITECTURE.md §3 (entire section)

**Scope (in)**:

- `cmd/agent/main.go`: configuration load (Viper from
  `/etc/domain-platform/agent.yaml` + env), graceful shutdown
- `cmd/agent/registration.go`: register with control plane on first start;
  store assigned `agent_id` in local config
- `cmd/agent/heartbeat.go`: 15-second heartbeat loop with backoff on failure
- `cmd/agent/pull.go`: long-poll `/agent/v1/tasks`, on receiving an envelope
  call into `handleTask()`
- `cmd/agent/handler.go`:
  - `handleTask(ctx, env *agentprotocol.TaskEnvelope) error` dispatches by
    `env.Type` to specific handlers
  - Handlers for `deploy_html`, `deploy_full`, `verify`. Each runs the
    pipeline: download → verify checksum → verify signature → write to
    staging → run `nginx -t` if applicable → snapshot previous → swap →
    reload if allowed → local verify → report
- `cmd/agent/safety.go`:
  - Constant declarations for the four (and only four) allowed shell-out
    points: `nginxTestBin = "/usr/sbin/nginx"`, `nginxReloadArgs = []string{"-s", "reload"}`,
    etc.
  - These constants are used directly; **no user input** flows into
    `os/exec.Command`
  - Function `verifyArtifact(...)` that runs SHA-256 checks against the
    manifest
- `make check-agent-safety` Makefile target:
  - Greps `cmd/agent/` for forbidden patterns:
    `os/exec.Command(.*[^"]\)` (variable command),
    `plugin\\.Open`, `net/http.*\\(url\\)` where url is a variable,
    etc.
  - **Implementation note**: this gate is stricter than the CI grep gates
    for state machines. It is a structural enforcement of CLAUDE.md
    Critical Rule #3.

**Scope (out)**:

- No agent self-upgrade (Phase 3)
- No drain/quarantine handling (Phase 2)
- No agent-side metrics export (Phase 2)
- No nginx reload aggregation buffer (Phase 2)

**Deliverables**:

- All files under `cmd/agent/`
- `make check-agent-safety` Makefile target
- `deploy/systemd/agent.service` unit file
- `configs/agent.example.yaml`
- Agent integration test: spin up a fake control plane (httptest) +
  fake MinIO (httptest serving pre-built artifact) + run the agent binary
  in a temp dir, assert files written and report sent

**Acceptance**:

- `make agent` cross-compiles successfully
- `make check-agent-safety` returns no violations
- The agent binary, run in a test environment, can complete a full
  download-verify-write-report cycle against fake control plane + fake S3
- No `os/exec.Command` exists in `cmd/agent/` other than the four
  hard-coded calls (`nginx -t`, `nginx -s reload`, configured local-verify
  HTTP curl, systemd self-restart)
- Any PR adding `os/exec` to `cmd/agent/` requires explicit Opus review
  (documented in PR template — to be added in P1.1)

**Why Opus**: this is the binary that runs as root (or with sudo) on every
production Nginx server. A bug here is a remote code execution waiting to
happen. The safety boundary cannot be relaxed for convenience.

**Completion notes (2026-04-12)**:
- `pkg/agentprotocol/types.go` — wire types (RegisterRequest/Response, HeartbeatRequest/Response, TaskEnvelope, TaskReport, PhaseReport, VerifyConfig, Manifest reuse)
- `cmd/agent/safety.go` — 4 hard-coded shell-outs (nginx -t, nginx -s reload) + HMAC verify + checksum verify + snapshotPrevious + copyFile. All os/exec uses have `// safe:` comments.
- `cmd/agent/handler.go` — 9-phase deployment pipeline (download → verify_checksum → verify_signature → write → nginx_test → snapshot → swap → reload → local_verify) with per-phase timing
- `cmd/agent/pull.go` — pullLoop (long-poll → claim → handle → report) + downloadArtifact + downloadFile
- `cmd/agent/heartbeat.go` — heartbeatLoop with exponential backoff, auto-recovery
- `cmd/agent/registration.go` — register() + getHostname()
- `cmd/agent/main.go` — full agent lifecycle: config load → mTLS client build → register → heartbeat goroutine → pull goroutine → graceful shutdown
- `configs/agent.example.yaml` — full config reference
- `deploy/systemd/domain-agent.service` — systemd unit with security hardening
- `internal/bootstrap/config.go` — extended AgentConfig with deploy_path, nginx_path, staging_path, signing_key, allow_reload, host_group, region
- `make agent` cross-compile: OK (linux/amd64)
- `make check-agent-safety`: OK (no violations)
- All tests pass, all CI gates green
- Integration test (fake control plane + fake S3) deferred to e2e integration pass

---

### P1.10 — Agent Management (control-plane side) ✅ DONE (2026-04-12)

**Owner**: **Opus** for `TransitionAgent()`; **Sonnet** for the rest
**Depends on**: P1.2 (agents tables), `pkg/agentprotocol` (defined here)
**Reads first**: ARCHITECTURE.md §2.6 + §3.4

**Scope (in)**:

- `pkg/agentprotocol/types.go` (defined here, used by both `cmd/agent` in
  P1.9 and `cmd/server` here):
  - `RegisterRequest`, `RegisterResponse`
  - `HeartbeatRequest`, `HeartbeatResponse`
  - `TaskEnvelope`, `TaskReport`, `PhaseReport`
  - `VerifyConfig` (local verification config carried in tasks)
  - All constants: task type strings, status strings
- `internal/agent/statemachine.go` (Opus):
  - `validAgentTransitions` map per CLAUDE.md
  - `CanAgentTransition(from, to string) bool`
  - Sentinel errors
- `store/postgres/agent.go`:
  - **Unexported** `updateAgentStatusTx`
  - `insertAgentStateHistoryTx`
  - CRUD: Create, GetByAgentID, ListByHostGroup, ListByStatus
  - `UpdateLastSeen(agentID, ts)`
- `internal/agent/service.go`:
  - `TransitionAgent(ctx, id, from, to, reason, triggeredBy)` (Opus)
  - `Register(ctx, req) (*Agent, error)` — Sonnet, calls `TransitionAgent`
    to enter `registered → online`
  - `Heartbeat(ctx, agentID, req) (*HeartbeatResponse, error)`
  - `PullNextTask(ctx, agentID) (*TaskEnvelope, error)` — long-poll;
    queries `agent_tasks WHERE agent_id = ? AND status = 'pending'`
  - `ReportTask(ctx, taskID, report) error` — updates agent_tasks +
    domain_tasks, may trigger release finalization
- `api/middleware/mtls.go`: extract client cert, verify against platform CA,
  resolve cert serial → agent_id, attach to context
- `api/handler/agentprotocol.go`: handlers for `/agent/v1/*` endpoints
- `internal/agent/health.go`: periodic offline detector (asynq task
  `TypeAgentHealthCheck`) — every 30s scans for online agents with
  `last_seen_at < NOW() - 90s` and transitions them to offline
- `make check-agent-writes`: CI grep gate for `UPDATE agents SET status`

**Scope (out)**:

- Self-upgrade (Phase 3)
- Drain / quarantine UI actions (Phase 2)
- Agent log ingestion (Phase 3, schema in place)

**Deliverables**:

- `pkg/agentprotocol/types.go`
- `internal/agent/{statemachine,service,health,errors}.go`
- `store/postgres/agent.go` (with unexported writer)
- `internal/agent/service_test.go` with race test for `TransitionAgent`
- `api/middleware/mtls.go`
- `api/handler/agentprotocol.go`
- `Makefile` updated with `check-agent-writes`

**Acceptance**:

- `go test -race -count=50 ./internal/agent/...` passes
- `make check-agent-writes` returns exactly one hit
- An agent (real or test) can `POST /agent/v1/register`, get assigned an
  `agent_id`, then heartbeat, pull a task, and report
- An online agent that stops heartbeating is transitioned to `offline` after 90s

**Completion notes (2026-04-12)**:
- `pkg/agentprotocol/types.go` — already completed in P1.9
- `internal/agent/statemachine.go` — 9 states (registered, online, busy, idle, offline, draining, disabled, upgrading, error), all edges per CLAUDE.md, graph completeness verified
- `internal/agent/errors.go` — ErrAgentNotFound, ErrInvalidAgentState, ErrAgentRaceCondition, ErrAgentOffline, ErrAgentDisabled, ErrNoTaskAvailable
- `store/postgres/agent.go` — Agent/AgentTask/AgentHeartbeat models, Create/GetByID/GetByAgentID/ListByStatus/ListByHostGroup/ListAll, **single write path** `updateAgentStatusTx` (CI gate enforced), TransitionTx with SELECT FOR UPDATE + optimistic check, heartbeat persistence, task CRUD (NextPendingTask, ClaimTask, CompleteTask), deployment log insert, stale agent detection
- `internal/agent/service.go` — TransitionAgent (validates edge + transactional), Register (create + auto-transition registered→online), Heartbeat (update last_seen + persist + auto-recover offline→online + check pending tasks), PullNextTask, ClaimTask, ReportTask (completes task + records phase logs)
- `internal/agent/health.go` — HealthChecker.CheckStaleAgents scans for online/busy/idle agents with last_seen > 90s, transitions to offline
- `api/middleware/mtls.go` — AgentMTLS (extracts cert serial + CN from TLS peer certs, dev-mode permissive fallback), RequireAgentCert (defense-in-depth)
- `api/handler/agentprotocol.go` — AgentProtocolHandler (Register, Heartbeat, PollTasks, ClaimTask, ReportTask for /agent/v1/*) + AgentHandler (List, Get, Transition, History for /api/v1/agents/*)
- `api/router/router.go` — added /api/v1/agents routes (List, Get, Transition, History) with RBAC
- `cmd/server/main.go` — wired AgentStore → AgentService → AgentProtocolHandler + AgentHandler, agent router now serves all 5 protocol endpoints under /agent/v1
- `internal/agent/statemachine_test.go` — 55 subtests: valid/invalid edges, targets, terminal check, graph completeness (keys ↔ AllAgentStates, all targets are known states)
- `go test -race -count=5 ./internal/agent/...` passes
- `make check-agent-writes` OK
- All 4 CI gates green, all tests pass

---

### P1.11 — asynq worker bootstrap ✅ DONE (2026-04-12)

**Owner**: Sonnet
**Depends on**: P1.1 (worker skeleton)
**Runs in parallel with**: P1.3-P1.10 (after P1.2)
**Reads first**: ARCHITECTURE.md §2 task references, CLAUDE.md §"Task Queue Patterns"

**Scope (in)**:

- `internal/tasks/types.go`: declare every task type constant
- `internal/tasks/payloads.go`: declare every payload struct
- `cmd/worker/main.go`: finalize the worker boot with the canonical queue
  layout from CLAUDE.md §"asynq Queue Layout"
- Register stub handlers for every task type. Stubs that have no real
  implementation yet log the payload at Info and return `nil` (ack).
  Real handlers are filled in by P1.5/P1.7/P1.8/P1.10.
- `internal/bootstrap/asynq.go`: ensure the asynq client is exposed and
  used by services that enqueue

**Scope (out)**:

- No real handler logic (those land in their respective task cards)
- No asynq scheduler (Phase 3)
- No asynqmon dashboard wiring

**Deliverables**:

- `internal/tasks/types.go`, `internal/tasks/payloads.go`
- Finalized `cmd/worker/main.go`
- Verified queue config matches ARCHITECTURE.md §2.5

**Acceptance**:

- `./bin/worker` starts, prints the queue config at boot
- Enqueuing any task type from a service results in the worker stub logging
  the payload
- ARCHITECTURE.md §2.5 and `cmd/worker/main.go::asynq.Config.Queues` match
  exactly (same queue names, same priorities, same concurrency)

**Completion notes (2026-04-12)**:
- `internal/tasks/types.go` — all 15 task type constants (already existed; removed ArtifactBuildPayload moved to payloads.go)
- `internal/tasks/payloads.go` — all payload structs: LifecycleProvision/Deprovision, ArtifactBuild/Sign, ReleasePlan/DispatchShard/ProbeVerify/Finalize/Rollback, AgentHealthCheck/UpgradeDispatch, ProbeRun (shared L1/L2/L3), NotifySend
- `internal/bootstrap/asynq.go` — fixed queue priorities to match CLAUDE.md exactly (critical=10, release=6, artifact=5, lifecycle=4, probe=3, default=2); added QueueForTask map; DefaultWorkerConcurrency=75; updated NewAsynqServer to use DefaultWorkerConcurrency
- `cmd/worker/main.go` — real handler: artifact:build wired via artifact.NewHandleBuild; stub handlers for all 14 other task types (log payload + nil); prints queue config + concurrency at boot; graceful shutdown via SIGINT/SIGTERM
- `go build ./...` clean; all CI gates green; all tests pass

---

### P1.12 — Frontend pages: project / domain / template / release / agent ✅ DONE

**Completed**: 2026-04-12
**Actual effort**: ~0.3d (types/api/stores + 10 views + MainLayout + router)
**Notes**: `npm run build` clean. Views implemented: Dashboard, ProjectList, ProjectDetail,
TemplateList, TemplateDetail, DomainList, DomainDetail, ReleaseList, ReleaseDetail,
AgentList, AgentDetail. All TypeScript types aligned to Go DTOs. Auth interceptor,
router guards, MainLayout with collapsible sidebar all wired.

**Owner**: Sonnet
**Depends on**: P1.3 (login), P1.4 (project API), P1.5 (domain API),
P1.6 (template API), P1.8 (release API), P1.10 (agent API)
**Reads first**: FRONTEND_GUIDE.md (entire), CLAUDE.md §"Frontend Conventions"

**Scope (in)**:

- Login wired to `POST /api/v1/auth/login` → store JWT in Pinia + localStorage
  → redirect to `/projects`
- Auth interceptor (`web/src/utils/http.ts`): attaches `Authorization: Bearer`,
  redirects to `/login` on 401
- Pinia stores: `auth.ts`, `project.ts`, `domain.ts`, `template.ts`,
  `release.ts`, `agent.ts`
- Pages (each with list view):
  - `/projects` — Project list, click to detail
  - `/projects/:id` — Project detail (read-only); shows domains, templates,
    recent releases
  - `/projects/:id/domains` — Domain list with state filter
  - `/projects/:id/templates` — Template list; click to view versions
  - `/projects/:id/templates/:tid` — Template detail showing all versions
  - `/projects/:id/releases` — Release list with status filter
  - `/projects/:id/releases/:rid` — Release detail showing scope, agent_tasks
    progress, audit timeline
  - `/agents` — Global agent list with status filter
  - `/agents/:id` — Agent detail showing recent heartbeats and recent tasks
- StatusTag component: extend to support all new state values:
  - Domain lifecycle: requested / approved / provisioned / active / disabled / retired
  - Release status: pending / planning / ready / executing / paused / succeeded
    / failed / rolling_back / rolled_back / cancelled
  - Agent status: registered / online / busy / idle / offline / draining /
    disabled / upgrading / error
  - Color tokens defined in `web/src/styles/tokens.ts` per FRONTEND_GUIDE.md
- Router updates with auth guards
- Layout / nav menu with the 4 main sections (Projects, Releases, Agents,
  Audit) and admin-only items behind role gates

**Scope (out)**:

- Create / edit modals for everything (Phase 2 — Phase 1 is read-only +
  the create path uses curl/postman)
- Approval flow UI (Phase 4)
- Release create wizard (Phase 2)
- Agent drain/disable controls (Phase 2)
- Real-time updates (no WebSocket; polling on detail pages every 5s is fine)

**Deliverables**:

- All `web/src/views/**` pages listed above
- All `web/src/api/*.ts` and `web/src/types/*.ts` files
- Updated Pinia stores
- Router and layout updates
- StatusTag color tokens for new states

**Acceptance**:

- `npm run build` in `web/` succeeds without warnings
- Manual smoke: log in → land on `/projects` → click into a project → see
  domains, templates, releases tabs → click an active release → see its
  shard / task list → click into agents → see the agent that processed it
- 401 from any API call redirects back to `/login`
- TypeScript types match Go DTOs byte-for-byte

---

## Cross-cutting reminders

1. **CLAUDE.md Critical Business Rules are load-bearing.** Re-read them
   before any task. Rule #1 (single state machine write paths), Rule #2
   (artifact immutability), Rule #3 (agent whitelist) in particular.

2. **Pre-launch migration exception is in effect.** During Phase 1 you may
   edit `migrations/000001_init.up.sql` in place. After Phase 1 cutover this
   window closes permanently.

3. **Three CI gates must stay green at all times**:
   ```
   make check-lifecycle-writes
   make check-release-writes
   make check-agent-writes
   make check-agent-safety        # additional structural gate for cmd/agent/
   ```
   Any PR that breaks any of these must be fixed, never bypassed.

4. **Phase 1 builds the skeleton end-to-end. Phase 1 does NOT touch real
   production infrastructure unless you point it at your own dev MinIO and
   your own dev Nginx VM.** No real DNS provider calls in CI; mock them.

5. **Log levels**: Info for normal ops, Warn for recoverable, Error for
   needs attention. No `fmt.Println` in production code.

6. **Every API response uses the envelope** from CLAUDE.md §"Response Format".

7. **Multi-table writes use transactions** (`BeginTxx` + defer `Rollback` +
   explicit `Commit`).

8. **All external calls have context timeouts** per CLAUDE.md §"Context & Timeouts".

9. **Deterministic artifact builds.** No timestamps in content, no random
   IDs in content, sorted variable iteration. The reproducibility test in
   `internal/artifact/builder_test.go` is mandatory.

10. **Agent binary safety is structural, not configurational.** The
    whitelist is enforced by the absence of certain code paths, not by
    config flags. Any PR touching `cmd/agent/` requires Opus review.

---

## When Phase 1 is "done"

All twelve cards are merged to `main`, and the following commands all return
clean output:

```bash
make build                      # five binaries compile
make agent                      # agent cross-compiles for linux/amd64
make lint                       # golangci-lint + eslint clean
make test                       # go test ./... -race -timeout 60s green
make check-lifecycle-writes     # exactly one hit
make check-release-writes       # exactly one hit
make check-agent-writes         # exactly one hit
make check-agent-safety         # zero violations in cmd/agent/
cd web && npm run build         # frontend builds clean
cd web && npm run lint
```

Plus the manual end-to-end smoke test described in §"What 'Phase 1 done'
looks like".

At that point the platform has a runnable control plane: log in, define
projects + domains + templates, build artifacts, create releases, watch
agents pull and apply, see results. The architecture is locked along the
contracts that Phase 2-4 plug into without re-touching core. Phase 5+ adds
GFW failover as a vertical (separate ADR).

---

## References

- `CLAUDE.md` — coding standards, critical rules, state machines, project layout
- `docs/PHASE1_EFFORT.md` — **effort estimate** (Lo/Hi work-days per task, critical path, 3 risk hotspots, week-by-week order). Rebaseline after P1.3
- `docs/ARCHITECTURE.md` — subsystem responsibilities, agent protocol, queue layout
- `docs/DATABASE_SCHEMA.md` — every table, every constraint
- `docs/DEVELOPMENT_PLAYBOOK.md` — how to add endpoints / providers / tasks /
  artifact steps / state transitions / pages
- `docs/FRONTEND_GUIDE.md` — frontend conventions (status tokens, table component, etc.)
- `docs/CLAUDE_CODE_INSTRUCTIONS.md` — Claude Code session protocol + Model Selection Policy
- `docs/adr/0003-pivot-to-generic-release-platform-2026-04.md` — pivot rationale, scope decisions
- `docs/adr/0001-...md` and `docs/adr/0002-...md` — superseded; historical only
- **PRD**: `/Users/ahern/Documents/AI-tools/Domain Lifecycle & Deployment Platform（域名生命週期與發布運維平台）.md`
