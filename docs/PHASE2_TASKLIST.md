# PHASE2_TASKLIST.md — Phase 2 Work Order

> **Created 2026-04-12.** This document is the authoritative work order for
> Phase 2 of the Domain Lifecycle & Deployment Platform.
>
> **Pre-requisite**: Phase 1 complete (all 12 task cards delivered). Read
> CLAUDE.md, ARCHITECTURE.md §1-§5, PHASE1_TASKLIST.md (for context on what
> exists), and this document before starting any P2 task.
>
> **Audience**: Claude Code sessions (Sonnet for most tasks, Opus for
> bottleneck tasks marked with `(Opus)`).

---

## Phase 2 — Definition of Scope

Phase 2 upgrades the flat single-shard release pipeline from Phase 1 into a
**production-grade multi-shard release system** with rollback, dry-run preview,
per-host concurrency control, and fleet management UI.

### What "Phase 2 done" looks like (acceptance demo)

```
1. Admin creates a Release for project=demo with 100 domains across 3 host groups
2. System splits the release into 3 shards (one per host group)
3. Admin previews the release via Dry-run → sees HTML diff + nginx conf diff
4. Admin triggers execution with max_concurrency=5
5. Shard 1 dispatches: agent_tasks created, agents pull tasks, execute, report
6. Shard 1 succeeds → Shard 2 auto-proceeds → Shard 3 auto-proceeds
7. Release finalizes to succeeded; all domain_tasks and agent_tasks are "succeeded"
8. Admin triggers rollback on one shard → previous artifact redeployed → shard = rolled_back
9. Admin views Agent fleet page: drain an agent → agent finishes current tasks,
   accepts no new ones → agent status = disabled
10. Domain registered → auto-approved → DNS records created via Cloudflare provider
    → domain state = provisioned → operator transitions to active
```

### What is OUT of Phase 2 (do not implement)

| Subsystem | Phase | Reason |
|---|---|---|
| Canary policy (% threshold gating, auto-pause) | Phase 3 | Requires probes to gate |
| Probe L1 / L2 / L3 | Phase 3 | Requires reliable release execution first |
| Alert engine + dedup + notify channels | Phase 3 | Requires probes to generate signals |
| Agent canary upgrade | Phase 3 | Requires fleet management from this phase |
| Approval flow execution | Phase 4 | Schema in place; auto-approve still in code |
| Nginx artifact as separate gated type | Phase 4 | Phase 2 still treats html+nginx together |
| HA / cross-region | Phase 4 | Single instance sufficient |
| API rate limiting | Phase 3+ | Not critical before external access |
| GFW failover vertical | Unscheduled | ADR-0003 D11 |

---

## Dependency Graph

```
                    P2.1 (Release dispatch pipeline — real task execution)
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
          P2.2          P2.3         P2.4
        Multi-shard    Rollback    Dry-run/Diff
        splitting      execution
              │            │
              └──────┬─────┘
                     ▼
                   P2.5
              Per-host concurrency
                     │
                     ▼
                   P2.6
            Agent fleet mgmt UI
                     │
                     ▼
                   P2.8
              E2E integration test

        P2.7 (DNS provider — independent, can run any time after P1)
```

### Critical path

`P2.1 → P2.2 → P2.5 → P2.6 → P2.8`

### Parallelization rules

- P2.2, P2.3, P2.4 may all run in parallel after P2.1 (they touch disjoint packages)
- P2.5 depends on P2.2 (shard dispatch must work before concurrency limits matter)
- P2.6 depends on P2.5 (UI needs the backend capabilities)
- P2.7 (DNS provider) is fully independent — can run at any point
- P2.8 is the final integration task; depends on all others

---

## Task Cards

Each card specifies: **owner model**, **scope (in)**, **scope (out)**,
**dependencies**, **deliverables**, **acceptance criteria**, and the **docs
to read first**.

---

### P2.1 — Release dispatch pipeline: real task execution **(Opus)**

**Status**: ✅ COMPLETED 2026-04-12

**Owner**: **Opus** — bottleneck task (task dispatch is the most critical
correctness path in Phase 2)
**Depends on**: Phase 1 complete (P1.8 release state machine, P1.9 agent
binary, P1.10 agent control plane, P1.11 asynq worker)
**Reads first**: CLAUDE.md §"Task Queue Patterns", §"Agent Protocol",
Critical Rules #1, #6, #7; ARCHITECTURE.md §3 "Artifact Pipeline", §4 "Agent
Protocol"

**Context**: Phase 1 created the release state machine and agent task pipeline
but left `release:dispatch_shard` and `release:finalize` as stubs. The agent
binary (`cmd/agent`) has a working 9-phase deployment pipeline. This task wires
them together end-to-end: release execution creates real `domain_tasks` and
`agent_tasks` rows, agents pull and execute them, and the release finalizes
based on actual results.

**Scope (in)**:

- `release:plan` handler (upgrade from stub):
  - Fetch release + its scopes (domain list)
  - For Phase 2.1: create a **single shard** (shard_index=0) containing all
    domains — multi-shard splitting is P2.2
  - Create `domain_tasks` rows (one per domain in scope, type=`deploy_html`)
  - Transition release: `pending → planning → ready`
  - Enqueue `release:dispatch_shard` for the shard

- `release:dispatch_shard` handler (upgrade from stub):
  - Fetch shard + its domain_tasks
  - For each domain_task, resolve which agent(s) serve that domain's host_group
  - Create `agent_tasks` rows with:
    - `task_id`: deterministic UUID (`{release_id}-{domain_id}-{agent_id}`)
    - `artifact_url`: pre-signed MinIO URL (120s TTL)
    - `payload`: JSON with domain FQDN, deploy paths, artifact checksum,
      release_id, template_version_id
  - Update shard status: `pending → dispatching → running`
  - Transition release: `ready → executing` (if first shard)
  - Enqueue `release:finalize` with a delay (poll-based or after last task reports)

- `release:finalize` handler (upgrade from stub):
  - Query all `agent_tasks` for the release
  - If all succeeded → transition release to `succeeded`
  - If any failed and no retry left → transition release to `failed`
  - If some still running → re-enqueue self with 10s delay (max 360 retries = 1hr)
  - Update shard `success_count` / `failure_count` / `ended_at`
  - Update `domain_tasks` statuses based on their agent_tasks

- `store/postgres/release.go` additions:
  - `CreateDomainTasks(ctx, tx, []DomainTask) error`
  - `CreateAgentTasks(ctx, tx, []AgentTask) error`
  - `GetDomainTasksByRelease(ctx, releaseID) ([]DomainTask, error)`
  - `GetAgentTasksByShard(ctx, shardID) ([]AgentTask, error)`
  - `UpdateAgentTaskStatus(ctx, taskID, status, error) error`
  - `GetShardStats(ctx, shardID) (ShardStats, error)`

- `internal/agent/service.go` — update `PullNextTask` to return real
  `agent_tasks` rows as `TaskEnvelope`s (currently may return mock data)

- `internal/agent/service.go` — update `ReportTask` to write
  `agent_tasks.status`, `ended_at`, `duration_ms`, `last_error` and update
  the parent `domain_tasks.status`

**Scope (out)**:

- Multi-shard splitting logic (P2.2 — this task creates one flat shard)
- Rollback on failure (P2.3)
- Pre-signed URL rotation for long-running deploys (future)
- Probe verification after deploy (Phase 3)

**Deliverables**:

- Updated worker handlers: `release:plan`, `release:dispatch_shard`,
  `release:finalize`
- New store methods for domain_tasks and agent_tasks CRUD
- Updated agent service: real PullNextTask + ReportTask
- Integration test: create release → plan → dispatch → agent claims + reports
  → finalize = succeeded

**Acceptance**:

- Create a release with 3 domains in scope → `release:plan` creates 3
  domain_tasks + 1 shard
- `release:dispatch_shard` creates agent_tasks with valid pre-signed URLs
- Agent binary pulls a task, executes the 9-phase pipeline, reports success
- `release:finalize` transitions release to `succeeded` after all tasks done
- If an agent reports failure → release transitions to `failed`
- `domain_tasks` and `agent_tasks` status columns are correct at every step
- `go test -race -count=10 ./internal/release/...` passes
- All 4 CI gates green (`check-lifecycle-writes`, `check-release-writes`,
  `check-agent-writes`, `check-agent-safety`)

---

### P2.2 — Multi-shard release splitting

**Status**: ✅ COMPLETED 2026-04-12

**Owner**: Sonnet (state machine already exists; this is splitting logic)
**Depends on**: P2.1 (dispatch pipeline must work for a single shard first)
**Reads first**: CLAUDE.md §"Release State Machine", DATABASE_SCHEMA.md
`release_shards` / `release_scopes` tables, ARCHITECTURE.md §5 "Release
Subsystem"

**Context**: Phase 1 and P2.1 create one flat shard (shard_index=0) per
release. This task implements the shard planner that splits a release's domains
across multiple shards based on host_group, region, or explicit scope rules.

**Scope (in)**:

- `internal/release/planner.go` (new file):
  - `PlanShards(ctx, release, domains, agents) ([]PlannedShard, error)`
  - Splitting strategies:
    - **by_host_group** (default): one shard per host_group; domains assigned
      to the host_group's agents
    - **by_region**: one shard per agent region tag
    - **explicit**: caller provides shard assignments in release creation request
  - Each `PlannedShard` contains: shard_index, domain_ids, agent_ids,
    is_canary (always false in P2; canary logic is Phase 3)
  - Sort shards by index for deterministic execution order

- Update `release:plan` handler to use `PlanShards()` instead of creating
  a single flat shard

- `api/handler/release.go` — extend `CreateReleaseRequest` with optional
  `shard_strategy` field (default: `by_host_group`)

- `store/postgres/release.go` additions:
  - `CreateShards(ctx, tx, []ReleaseShard) error` (bulk insert)
  - `GetShardsByRelease(ctx, releaseID) ([]ReleaseShard, error)`
  - `UpdateShardStatus(ctx, shardID, status) error`

- Shard execution order in `release:dispatch_shard`:
  - Dispatch shards sequentially by shard_index (shard 0 → wait for
    completion → shard 1 → ...)
  - On shard success: enqueue next shard's `release:dispatch_shard`
  - On shard failure: transition release to `failed` (no auto-rollback in P2;
    that's P2.3)

- Update `release:finalize` to aggregate across all shards

**Scope (out)**:

- Canary shard (is_canary=true + threshold gating) — Phase 3
- Parallel shard execution (all shards execute sequentially in P2)
- Custom shard sizing / weighted distribution
- Auto-pause on shard failure threshold

**Deliverables**:

- `internal/release/planner.go` with `PlanShards()`
- Updated `release:plan` handler
- Updated `release:dispatch_shard` handler (sequential shard chain)
- Updated `release:finalize` handler (multi-shard aggregation)
- Updated release creation API (shard_strategy field)
- Unit tests for planner: 3 host_groups → 3 shards, region split, explicit
  assignment
- Integration test: release with 3 host_groups → 3 shards → sequential
  dispatch → succeeded

**Acceptance**:

- Create release with domains across 3 host_groups → plan creates 3 shards
- Shards execute sequentially: shard 0 completes → shard 1 starts → ...
- If shard 1 fails → shard 2 never starts, release = `failed`
- `GET /api/v1/releases/:id` response includes shard breakdown with per-shard
  status and counts
- `PlanShards` is deterministic: same input → same shard assignment
- `go test ./internal/release/...` passes (including planner tests)

**Delivered**:
- `internal/release/planner.go` — `PlanShards()` + `planByHostGroup()` with deterministic host_group_id-sorted shard assignment; ungrouped domains collect in trailing shard
- `store/postgres/agent.go` — `GetAgentTaskStatsByShard(ctx, shardID)` for per-shard stats
- `store/postgres/release.go` — `GetNextPendingShard(ctx, releaseID, afterIndex)` for sequential shard chaining
- `internal/release/service.go`:
  - `CreateInput.ShardStrategy` field
  - `ReleasePlanPayload.ShardStrategy` — strategy carried from API to worker
  - `ReleaseFinalizePayload.ShardID` — per-shard finalization context
  - `Plan()` — replaced P2.1 single-shard block with `PlanShards()`; creates one shard + domain_tasks per planned shard
  - `MarkReady()` — changed from "dispatch all shards" to "dispatch shard 0 only"; subsequent shards are chained by `Finalize()`
  - `DispatchShard()` — finalize payload now includes `ShardID`
  - `Finalize()` — three paths: isRollback (existing), shardID>0 (per-shard P2.2), shardID==0 (legacy); per-shard path dispatches next shard on success or fails release on failure
- `internal/release/handler.go` — updated `HandlePlan` and `HandleFinalize` to pass new params
- `api/handler/release.go` — `CreateReleaseRequest.ShardStrategy` field; passed through to `CreateInput`

---

### P2.3 — Rollback execution **(Opus)**

**Status**: ✅ COMPLETED 2026-04-12

**Owner**: **Opus** — correctness-critical (rollback must not make things worse)
**Depends on**: P2.1 (dispatch pipeline — rollback reuses the same dispatch
mechanism)
**Reads first**: CLAUDE.md Critical Rules #2, #6; DATABASE_SCHEMA.md
`rollback_records` table; ARCHITECTURE.md §5 "Rollback"

**Context**: The `rollback_records` table exists from Phase 1 migration. The
agent binary already snapshots previous files to `.previous/{release_id}/`
(P1.9, phase 6 "snapshot"). This task implements the control-plane side:
creating rollback records, resolving the target artifact, dispatching rollback
tasks to agents, and tracking completion.

**Scope (in)**:

- `internal/release/rollback.go` (new file):
  - `Service.Rollback(ctx, RollbackInput) (*RollbackRecord, error)`
    - `RollbackInput`: release_id, scope (`release` | `shard` | `domain`),
      scope_target_id (shard_id or domain_id), reason, triggered_by
    - Validates: release must be in `succeeded`, `failed`, or `executing` state
    - Resolves `target_artifact_id`: the artifact that was deployed *before*
      this release (query `releases` table for the previous succeeded release
      in the same project)
    - Creates `rollback_records` row
    - Transitions release: current_state → `rolling_back`
    - Creates `domain_tasks` with `task_type='rollback'`
    - Creates `agent_tasks` pointing to the previous artifact
    - Enqueues `release:rollback` asynq task

- `release:rollback` worker handler (upgrade from stub):
  - Dispatch rollback tasks to agents (same mechanism as deploy, but
    `task_type=rollback`)
  - Agent receives rollback task → restores from `.previous/{release_id}/`
    (agent already supports this in handler.go)
  - On all tasks complete: transition release to `rolled_back`, update
    `rollback_records.completed_at` and `success`

- `api/handler/release.go` — new endpoint:
  - `POST /api/v1/releases/:id/rollback` — body: `{scope, scope_target_id, reason}`
  - Requires `release_manager` or `admin` role

- `store/postgres/rollback.go` (new file):
  - `CreateRollbackRecord(ctx, record) error`
  - `UpdateRollbackRecord(ctx, id, completedAt, success) error`
  - `GetRollbacksByRelease(ctx, releaseID) ([]RollbackRecord, error)`
  - `GetPreviousSucceededRelease(ctx, projectID, beforeReleaseID) (*Release, error)`

- Agent-side: verify `cmd/agent/handler.go` handles `task_type=rollback`
  correctly — it should restore from `.previous/` snapshot rather than
  downloading a new artifact. If not implemented, add a rollback code path
  that:
  1. Checks `.previous/{target_release_id}/` exists
  2. Swaps current → staging, previous → current
  3. Runs `nginx -t` + `nginx -s reload`
  4. Reports success/failure

**Scope (out)**:

- Automatic rollback on failure (Phase 3 — requires probe to confirm failure)
- Cross-release rollback (rolling back to an artifact from 3 releases ago)
- Rollback approval flow (Phase 4)
- Partial domain-level rollback within a shard (scope=`domain` is supported
  in schema but implementation deferred — only `release` and `shard` scopes
  are implemented in P2)

**Deliverables**:

- `internal/release/rollback.go`
- `store/postgres/rollback.go`
- Updated `release:rollback` worker handler
- API endpoint `POST /api/v1/releases/:id/rollback`
- Agent rollback code path (if not already present)
- Unit tests: rollback from succeeded, rollback from failed, rollback scope
  validation, previous artifact resolution
- Integration test: deploy succeeds → rollback → agents restore previous files
  → release = rolled_back

**Acceptance**:

- `POST /api/v1/releases/:id/rollback` on a succeeded release → creates
  rollback_record, transitions to `rolling_back`
- Agents receive rollback tasks, restore `.previous/` files, report success
- Release transitions to `rolled_back` after all rollback tasks complete
- `rollback_records` row has `completed_at` and `success=true`
- Rollback on a `pending` or `cancelled` release returns 409
- Only `release_manager` or `admin` can trigger rollback (operator gets 403)
- `go test -race -count=10 ./internal/release/...` passes

**Delivered**:
- `internal/release/rollback.go` — `Service.Rollback()` + `Service.ExecuteRollback()` with state validation, previous-succeeded-release lookup, rollback_record creation, and rollback agent task dispatch
- `store/postgres/rollback.go` — `RollbackStore` with `Create()`, `Complete()`, `GetByRelease()` methods
- `store/postgres/release.go` — added `GetLastSucceeded(ctx, projectID, beforeID)` query
- `internal/release/errors.go` — added `ErrRollbackNotAllowed`, `ErrNoPreviousRelease`
- `internal/release/service.go` — extended `Finalize()` with `isRollback bool` + `rollbackRecordID int64`; transitions `rolling_back → rolled_back` on success
- `internal/release/handler.go` — `HandleRollback` struct replacing the stub `release:rollback` worker handler
- `api/handler/release.go` — `POST /api/v1/releases/:id/rollback` (202 Accepted)
- `api/router/router.go` — route registered with `release_manager`/`admin` RBAC
- `cmd/agent/handler.go` — `handleRollback()` + `restoreFromSnapshot()` implementing the 4-phase restore (restore from `.previous/`, nginx -t, reload, local verify)
- `pkg/agentprotocol/types.go` — added `TargetReleaseID` to `TaskEnvelope`
- Frontend: rollback button + `ConfirmModal` in `ReleaseDetail.vue`; `canRollback` computed (failed|paused states)

---

### P2.4 — Dry-run / Diff preview

**Status**: ✅ COMPLETED 2026-04-12

**Owner**: Sonnet
**Depends on**: P2.1 (needs the artifact build pipeline to produce artifacts
that can be diffed)
**Reads first**: CLAUDE.md §"Artifact build pipeline", ARCHITECTURE.md §3
"Artifact Pipeline"

**Context**: Operators need to see what a release will change before executing
it. Dry-run builds the artifact without deploying, then diffs it against the
currently deployed files. This is a safety feature that prevents surprises.

**Scope (in)**:

- `internal/release/dryrun.go` (new file):
  - `Service.DryRun(ctx, releaseID) (*DryRunResult, error)`
    - Release must be in `ready` state (artifact already built)
    - Fetches the new artifact's file manifest from MinIO
    - Fetches the previous release's artifact manifest (the currently deployed
      version)
    - Computes per-file diff:
      - `added`: files in new but not in previous
      - `removed`: files in previous but not in new
      - `modified`: files in both but with different checksums
      - `unchanged`: files in both with same checksum
    - For `modified` files: fetch both versions' content and produce a unified
      diff (use `github.com/sergi/go-diff` or equivalent)
    - Returns `DryRunResult` with summary counts + per-file diffs

- `DryRunResult` struct:
  ```go
  type DryRunResult struct {
      ReleaseID     int64             `json:"release_id"`
      ArtifactID    int64             `json:"artifact_id"`
      PreviousID    *int64            `json:"previous_artifact_id,omitempty"`
      Summary       DiffSummary       `json:"summary"`
      Files         []FileDiff        `json:"files"`
  }
  type DiffSummary struct {
      Added      int `json:"added"`
      Removed    int `json:"removed"`
      Modified   int `json:"modified"`
      Unchanged  int `json:"unchanged"`
  }
  type FileDiff struct {
      Path      string `json:"path"`
      Status    string `json:"status"` // added, removed, modified, unchanged
      Diff      string `json:"diff,omitempty"` // unified diff for modified files
      SizeOld   int64  `json:"size_old,omitempty"`
      SizeNew   int64  `json:"size_new,omitempty"`
  }
  ```

- `api/handler/release.go` — new endpoint:
  - `GET /api/v1/releases/:id/dry-run` — returns `DryRunResult`
  - Requires `operator` role or above

- `pkg/storage/storage.go` — add method if missing:
  - `ListObjects(ctx, prefix) ([]ObjectInfo, error)` — list files in an
    artifact's S3 prefix
  - `GetObjectContent(ctx, key) ([]byte, error)` — fetch file content for diff

**Scope (out)**:

- Nginx config syntax validation during dry-run (agent-side only)
- Side-by-side visual diff in frontend (P2.6 will show the raw unified diff;
  a rich diff viewer is Phase 3+)
- Dry-run for rollback (nice-to-have, not P2)

**Deliverables**:

- `internal/release/dryrun.go`
- Updated storage interface (if needed)
- API endpoint `GET /api/v1/releases/:id/dry-run`
- Unit tests: first release (no previous → all files "added"), modified
  template → correct diff, unchanged release → all "unchanged"
- go-diff dependency added to go.mod

**Acceptance**:

- First release for a project → dry-run shows all files as `added`
- Second release with modified template → dry-run shows `modified` files with
  unified diff content
- Dry-run on a `pending` release (artifact not built yet) returns 409
- Dry-run on a `succeeded` release works (retrospective diff)
- Response includes correct `added`/`removed`/`modified`/`unchanged` counts
- `go test ./internal/release/...` passes

**Delivered**:
- `internal/release/dryrun.go` — `Service.DryRun()` computing added/removed/modified/unchanged per-file diff against previous succeeded release's artifact manifest; generates unified diff for text files using `github.com/pmezard/go-difflib`
- `pkg/storage/storage.go` — added `ListObjects()` and `GetObjectContent()` to the `Storage` interface
- `pkg/storage/minio.go` — implemented both methods; added `var _ Storage = (*MinIOStorage)(nil)` compile-time check
- `api/handler/release.go` — `GET /api/v1/releases/:id/dry-run` returning `DryRunResult` JSON
- `api/router/router.go` — route registered (viewer role and above)
- `web/src/types/release.ts` — `DryRunResult`, `DiffSummary`, `FileDiff` TypeScript types
- `web/src/api/release.ts` — `dryRun(id)` API method
- `web/src/stores/release.ts` — `dryRun(id): Promise<DryRunResult | null>` store action
- `web/src/views/releases/ReleaseDetail.vue` — Dry Run button + "Dry Run 預覽" tab with per-file change badges and unified diff `<pre>` blocks

---

### P2.5 — Per-host concurrency control

**Owner**: Sonnet
**Depends on**: P2.2 (multi-shard dispatch must work first)
**Reads first**: CLAUDE.md Critical Rule #7 (nginx reload batching),
ARCHITECTURE.md §4 "Agent Protocol"

**Context**: Phase 1 dispatches all agent_tasks at once (fire-and-forget).
Phase 2 needs to control how many agents execute simultaneously to avoid
overloading infrastructure. This task adds concurrency limits at the
host_group level and implements the nginx reload batching rule.

**Scope (in)**:

- `host_groups` table — add column (via migration or in-place edit if still
  in pre-launch window):
  - `max_concurrency INT NOT NULL DEFAULT 0` — 0 means unlimited
  - `reload_batch_size INT NOT NULL DEFAULT 50`
  - `reload_batch_wait_seconds INT NOT NULL DEFAULT 30`

- `internal/release/dispatcher.go` (new file):
  - `Dispatcher` struct: manages concurrent task dispatch within a shard
  - `Dispatch(ctx, shard, agentTasks, hostGroupConfig) error`
    - Respects `max_concurrency` per host_group: dispatch N tasks, wait for
      completions, dispatch next batch
    - Uses Redis sorted set or channel to track in-flight tasks per host_group
    - Timeout: if a task doesn't report within 5 minutes, mark as `timeout`

- Nginx reload batching (Critical Rule #7):
  - When multiple domains on the same host are deployed in the same shard:
    - Write all nginx confs first (no reload)
    - Buffer for `reload_batch_wait_seconds` OR `reload_batch_size` domains,
      whichever comes first
    - Then issue a single `nginx -s reload`
  - Implementation: agent_task payload includes `defer_reload: true` for all
    but the last task in a batch; the last task includes `defer_reload: false`
    (triggers reload)
  - Agent binary must respect `defer_reload` flag — skip phase 8 ("reload")
    when `defer_reload=true`

- `api/handler/host_group.go` (new or update existing):
  - `PUT /api/v1/host-groups/:id` — update concurrency and batch settings
  - `GET /api/v1/host-groups` — list host groups with their settings

- `store/postgres/host_group.go` — CRUD for host_groups including new columns

- `cmd/agent/handler.go` — respect `defer_reload` in task payload:
  - If `payload.defer_reload == true`, skip `nginx -s reload` phase
  - All other phases (write, nginx -t, snapshot, swap) still execute

**Scope (out)**:

- Global concurrency limit across all host_groups (only per-host_group)
- Dynamic concurrency adjustment during execution
- Rate limiting (requests/second) — this is about parallel task count
- Emergency override (skip batching) — Phase 3 with rollback auto-trigger

**Deliverables**:

- `internal/release/dispatcher.go`
- Updated migration or in-place edit for `host_groups` columns
- Updated `release:dispatch_shard` handler to use Dispatcher
- Updated agent binary to respect `defer_reload`
- Host group API endpoints
- Unit tests: dispatch with max_concurrency=2 and 6 tasks → 3 batches;
  reload batching with 3 domains on same host → 1 reload
- Integration test: release dispatch with concurrency=1 → tasks execute
  one at a time

**Acceptance**:

- Host group with `max_concurrency=2`: only 2 agent_tasks are `running`
  simultaneously; others wait in `pending`
- 5 domains on same host in one shard → nginx reloads exactly once (not 5
  times)
- `defer_reload=true` tasks skip the reload phase, `defer_reload=false`
  triggers it
- Agent tasks that don't report within 5 minutes are marked `timeout`
- `PUT /api/v1/host-groups/:id` updates concurrency settings
- `go test ./internal/release/...` passes

---

### P2.6 — Agent fleet management + Release operation UI

**Owner**: Sonnet
**Depends on**: P2.5 (backend capabilities), P2.3 (rollback API), P2.4
(dry-run API)
**Reads first**: FRONTEND_GUIDE.md, VISUAL_DESIGN.md

**Context**: Phase 1 frontend has read-only list/detail views for agents and
releases. Phase 2 adds operational controls: drain/disable/re-enable agents,
trigger releases with shard preview, view dry-run diffs, trigger rollback,
and monitor shard-by-shard execution progress.

**Scope (in)**:

- **Agent fleet page** (`web/src/views/agents/AgentList.vue` — enhance):
  - Filter by: status, host_group, region
  - Bulk actions: drain selected, disable selected
  - Per-agent actions: drain, disable, re-enable (calls
    `POST /api/v1/agents/:id/transition`)
  - Status badges with color coding per VISUAL_DESIGN.md
  - Real-time status refresh (10s polling)

- **Agent detail page** (`web/src/views/agents/AgentDetail.vue` — enhance):
  - Current task display (if busy)
  - Drain / disable / re-enable buttons
  - Heartbeat history timeline
  - Host metrics display (from heartbeat payload)

- **Release creation form** (new component):
  - Select project, template_version, release_type
  - Domain scope selector (multi-select from active domains)
  - Shard strategy selector (by_host_group / by_region / explicit)
  - Preview shard breakdown before creation
  - Submit → `POST /api/v1/releases`

- **Release detail page** (`web/src/views/releases/ReleaseDetail.vue` — enhance):
  - Shard progress visualization: per-shard progress bar with
    succeeded/failed/running/pending counts
  - Dry-run tab: calls `GET /api/v1/releases/:id/dry-run`, displays file diff
    with added/removed/modified indicators and unified diff view
    (use `<n-code>` or monospace pre block for diff content)
  - Action buttons:
    - Execute (ready → executing)
    - Pause / Resume
    - Cancel
    - Rollback (opens modal: select scope, enter reason)
  - Auto-refresh during execution (5s polling)

- **Host group management page** (new view):
  - List host groups with agent count, concurrency settings
  - Edit concurrency / batch settings inline

- `web/src/api/release.ts` — add methods:
  - `dryRun(releaseId)`, `rollback(releaseId, body)`,
    `execute(releaseId)`, `pause(releaseId)`, `resume(releaseId)`

- `web/src/api/agent.ts` — add methods:
  - `transition(agentId, body)`, `drain(agentId)`, `disable(agentId)`,
    `enable(agentId)`

- `web/src/api/hostgroup.ts` (new):
  - `list()`, `update(id, body)`

- `web/src/stores/hostgroup.ts` (new Pinia store)

**Scope (out)**:

- Rich side-by-side diff viewer (Phase 3+ — P2 uses monospace unified diff)
- Real-time WebSocket updates (polling is sufficient for P2)
- Agent terminal / log viewer (Phase 3)
- Drag-and-drop shard assignment
- Dashboard charts / graphs (Phase 3 with probe data)

**Deliverables**:

- Enhanced AgentList.vue and AgentDetail.vue
- Enhanced ReleaseDetail.vue with shard progress + dry-run + rollback
- New release creation form component
- New host group management page
- New API client methods and Pinia store
- `npm run build` clean with zero TypeScript errors

**Acceptance**:

- Agent list: filter by status shows correct subset; drain button →
  agent status changes to "draining"
- Release creation: select domains + shard strategy → preview shows N shards
  → submit creates release
- Release detail: during execution, shard progress bars update every 5s
- Dry-run tab: shows file diffs with correct added/modified/removed indicators
- Rollback modal: select scope + reason → triggers rollback → release
  transitions to "rolling_back"
- Host group page: edit max_concurrency → saved successfully
- All pages follow FRONTEND_GUIDE.md conventions
- `npm run build` succeeds with zero errors

---

### P2.7 — DNS provider implementation (Cloudflare)

**Owner**: Sonnet
**Depends on**: Phase 1 complete (P1.5 lifecycle state machine)
**Runs in parallel with**: any P2 task
**Reads first**: CLAUDE.md §"Provider Abstraction Layer", `pkg/provider/dns/`

**Context**: Phase 1 left `pkg/provider/dns/` as a skeleton with the
`Provider` interface defined but no implementations. The domain lifecycle
`approved → provisioned` transition needs to create DNS records. This task
implements the Cloudflare provider and wires it into the lifecycle provisioning
worker.

**Scope (in)**:

- `pkg/provider/dns/cloudflare.go`:
  - Implement `Provider` interface using Cloudflare API v4
  - Use `github.com/cloudflare/cloudflare-go` SDK
  - Methods: `CreateRecord`, `DeleteRecord`, `ListRecords`, `UpdateRecord`
  - Config: API token (scoped to DNS edit), zone ID mapping

- `pkg/provider/dns/registry.go`:
  - `Register(name, factory)` / `GetProvider(name) (Provider, error)`
  - Register `"cloudflare"` at init

- `lifecycle:provision` worker handler (upgrade from stub):
  - Fetch domain from DB
  - Determine DNS provider + zone from domain config or project config
  - Call `provider.CreateRecord()` to create A/CNAME record
  - On success: transition domain `approved → provisioned`
  - On failure: log error, retry (asynq MaxRetry=3)

- `lifecycle:deprovision` worker handler (upgrade from stub):
  - Fetch domain DNS records via provider
  - Delete records
  - Transition domain to `retired`

- `internal/lifecycle/service.go` — update `Transition()`:
  - When transitioning `approved → provisioned`: enqueue
    `lifecycle:provision` task (instead of doing it inline)
  - When transitioning to `retired`: enqueue `lifecycle:deprovision` task

- Config: `configs/providers.yaml` example with Cloudflare config:
  ```yaml
  dns:
    default_provider: cloudflare
    cloudflare:
      api_token: "${CLOUDFLARE_API_TOKEN}"
      zones:
        example.com: "zone-id-here"
  ```

**Scope (out)**:

- Route53, Google Cloud DNS, or other providers (add as needed; the interface
  makes this trivial)
- DNSSEC management
- DNS health checking / propagation verification
- Wildcard record management

**Deliverables**:

- `pkg/provider/dns/cloudflare.go`
- `pkg/provider/dns/registry.go`
- Updated `lifecycle:provision` and `lifecycle:deprovision` worker handlers
- Updated lifecycle service (enqueue tasks on transitions)
- `configs/providers.yaml` example
- Unit tests with mocked Cloudflare API (test the provider logic, not the
  Cloudflare SDK)
- `cloudflare-go` dependency added to go.mod

**Acceptance**:

- `dns.GetProvider("cloudflare")` returns a working provider instance
- `lifecycle:provision` handler creates a DNS record via Cloudflare API and
  transitions domain to `provisioned`
- `lifecycle:deprovision` handler deletes DNS records and transitions domain
  to `retired`
- With mock provider: domain `approved → provisioned` transition triggers
  async DNS provisioning → domain reaches `provisioned` state
- Provider failure → task retries (MaxRetry=3) → on final failure, domain
  stays in `approved` (not left in inconsistent state)
- `go test ./pkg/provider/dns/...` passes
- `go test ./internal/lifecycle/...` passes

---

### P2.8 — End-to-end integration test + documentation

**Owner**: Sonnet
**Depends on**: P2.1–P2.7 (all Phase 2 tasks)
**Reads first**: All Phase 2 task cards above

**Context**: The final P2 task validates that all Phase 2 components work
together. This is NOT a unit test — it's a scripted end-to-end scenario that
exercises the full release lifecycle including multi-shard dispatch, rollback,
and fleet management.

**Scope (in)**:

- `tests/e2e/phase2_test.go` (new file, build-tagged `//go:build e2e`):
  - **Scenario 1: Multi-shard release**
    1. Create project + 3 host_groups + 3 agents (one per host_group)
    2. Register + approve + provision 9 domains (3 per host_group)
    3. Create template + publish version
    4. Create release with shard_strategy=by_host_group
    5. Assert: 3 shards created
    6. Execute release → shards dispatch sequentially
    7. Simulate agent task reports (succeed all)
    8. Assert: release = succeeded, all domain_tasks = succeeded

  - **Scenario 2: Rollback**
    1. Using the succeeded release from Scenario 1
    2. Trigger rollback (scope=release)
    3. Assert: rollback_record created, release = rolling_back
    4. Simulate agent rollback reports (succeed all)
    5. Assert: release = rolled_back, rollback_record.success = true

  - **Scenario 3: Partial failure + shard stop**
    1. Create new release (same setup)
    2. Execute → shard 0 succeeds, shard 1: one agent fails
    3. Assert: shard 2 never dispatched, release = failed

  - **Scenario 4: Concurrency limit**
    1. Set host_group max_concurrency=1
    2. Create release with 3 domains in that host_group
    3. Assert: only 1 agent_task is `running` at a time

- Update `docs/ARCHITECTURE.md` §5 and §8 to reflect Phase 2 capabilities
  (multi-shard, rollback, concurrency control, dry-run)

- Update `docs/DEVELOPMENT_PLAYBOOK.md` with:
  - How to run e2e tests: `go test -tags e2e ./tests/e2e/ -v`
  - How to test multi-shard releases locally
  - How to configure host_group concurrency

- Create `docs/PHASE2_EFFORT.md` — actual effort tracking (same format as
  PHASE1_EFFORT.md, filled in as tasks complete)

**Scope (out)**:

- Performance/load testing
- Frontend e2e tests (Cypress/Playwright — Phase 3+)
- CI pipeline setup for e2e tests (manual run is sufficient for P2)

**Deliverables**:

- `tests/e2e/phase2_test.go` with 4 scenarios
- Updated ARCHITECTURE.md, DEVELOPMENT_PLAYBOOK.md
- `docs/PHASE2_EFFORT.md`

**Acceptance**:

- `go test -tags e2e ./tests/e2e/ -v -count=1` passes all 4 scenarios
  (requires docker-compose services running)
- Documentation accurately reflects Phase 2 capabilities
- PHASE2_EFFORT.md has actual times filled in for all P2 tasks

---

## Phase 2 Effort Estimate

> **Same caveat as Phase 1**: these are planning tools, not commitments.
> Re-calibrate after P2.1 completes (the largest task).

| # | Task | Owner | Lo | Hi | Risk | Notes |
|---|---|---|---|---|---|---|
| P2.1 | Release dispatch pipeline | **Opus** | 1.5 | 3.0 | 🔴 | Most complex — wires 4 components together |
| P2.2 | Multi-shard splitting | Sonnet | 1.0 | 2.0 | 🟡 | Planner logic + sequential shard chain |
| P2.3 | Rollback execution | **Opus** | 1.0 | 2.0 | 🔴 | Correctness-critical; agent rollback path |
| P2.4 | Dry-run / Diff | Sonnet | 0.5 | 1.5 | 🟢 | Mostly read-only; diff library integration |
| P2.5 | Per-host concurrency | Sonnet | 1.0 | 2.0 | 🟡 | Redis coordination + nginx batch logic |
| P2.6 | Fleet mgmt + Release UI | Sonnet | 2.0 | 3.5 | 🟡 | 6+ pages/components; scope creep risk |
| P2.7 | DNS provider (Cloudflare) | Sonnet | 0.5 | 1.5 | 🟢 | Interface exists; SDK is straightforward |
| P2.8 | E2E integration + docs | Sonnet | 1.0 | 2.0 | 🟡 | Depends on everything else working |

**Task sum**: Lo = 8.5 days / Hi = 17.5 days

**Integration friction**: +2–4 days (multi-shard + rollback + concurrency
interplay will surface edge cases)

| | Work days | Calendar weeks |
|---|---|---|
| **Optimistic** | 10.5 days | ~2.5 weeks |
| **Mid-range** | 16 days | ~3.5 weeks |
| **Pessimistic** | 21.5 days | ~4.5 weeks |

### Risk hotspots

1. **P2.1 dispatch pipeline** 🔴 — First time wiring release → domain_tasks →
   agent_tasks → agent execution → report back → finalize. Many moving parts.
   Mitigation: get a single happy path working first, then add error handling.

2. **P2.5 concurrency + nginx batching** 🟡 — Redis-based coordination is
   fiddly. Mitigation: start with a simple mutex/semaphore, not a sophisticated
   rate limiter.

3. **P2.6 frontend scope creep** 🟡 — Same risk as P1.12. Mitigation: ship
   functional first, polish later. No custom diff viewer — monospace pre block
   is fine.

### Recommended work order

```
Week 1  : P2.7 (DNS, independent) + P2.1 (dispatch pipeline — start early)
Week 2  : P2.1 (finish) + P2.4 (dry-run, low risk)
Week 3  : P2.2 (multi-shard) + P2.3 (rollback) — parallel, disjoint packages
Week 4  : P2.5 (concurrency) + P2.6 (frontend — start)
Week 5  : P2.6 (finish) + P2.8 (e2e integration + docs)
```

---

## Scope Creep Warnings

| Temptation | Truth |
|---|---|
| "P2.2 should support canary shards since the column exists" | Canary gating requires probes (Phase 3). Set `is_canary=false` and move on. |
| "P2.3 rollback should auto-trigger on failure" | Auto-rollback requires probe verification (Phase 3). Manual trigger only in P2. |
| "P2.4 dry-run needs a rich side-by-side diff viewer" | Monospace unified diff in a `<pre>` block is sufficient. Rich diff is Phase 3+. |
| "P2.5 should support dynamic concurrency adjustment" | Static config per host_group is fine. Dynamic = Phase 3 with probe feedback. |
| "P2.6 needs WebSocket for real-time updates" | 5-10s polling is fine. WebSocket is Phase 3+. |
| "P2.7 should support Route53 too" | One provider is enough. The interface makes adding more trivial later. |

---

## References

- `docs/PHASE1_TASKLIST.md` — Phase 1 task cards (what was built)
- `docs/PHASE1_EFFORT.md` — Phase 1 effort tracking
- `docs/ARCHITECTURE.md` — System architecture
- `docs/DATABASE_SCHEMA.md` — Schema reference
- `docs/FRONTEND_GUIDE.md` — Frontend conventions
- `docs/VISUAL_DESIGN.md` — Design system
- `CLAUDE.md` — Tech stack, coding standards, critical business rules
- `docs/adr/0003-pivot-to-generic-release-platform-2026-04.md` — Architecture pivot rationale

---

## Update Log

| Date | Author | Content |
|---|---|---|
| 2026-04-12 | Claude Opus 4.6 + ahern | Initial creation; 8 task cards (P2.1–P2.8) |
