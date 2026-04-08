# CLAUDE_CODE_INSTRUCTIONS.md — Master Guide for Claude Code

> **Aligned with PRD + ADR-0003 (2026-04-09).** This file is the entry point.
> Read this first, then consult the referenced documents as needed.

---

## What You're Building

A **Domain Lifecycle & Deployment Platform** — an enterprise HTML + Nginx
release operations system. The platform manages 10+ projects and 1万+ domains,
builds **immutable artifacts** in the control plane, and distributes them
through **Pull Agents** (Go binaries) running on each Nginx server, with
canary, rollback, probe verification, alerting, and full audit.

**This is an enterprise-grade SaaS application.** Every feature must be
production-ready: proper error handling, audit logging, input validation,
graceful degradation, and comprehensive tests.

> **Out of scope (parked, not abandoned)**: GFW failover, mainland-China
> reachability monitoring, prefix-based subdomain auto-generation, standby
> domain pool with warmup/promotion, automated DNS+CDN switching. These are
> a future vertical that will be built ON TOP of this platform; see
> ADR-0003 D11. Do **not** implement them as part of Phase 1-4.

---

## Document Map

Read these documents in this order when starting a new task:

| Document | When to Read | Content |
|----------|-------------|---------|
| **CLAUDE.md** | Always (should be auto-loaded) | Tech stack, project structure, coding standards, three state machines, critical rules |
| **PHASE1_TASKLIST.md** | **First task of any Phase 1 session** | Authoritative work order for Phase 1: 12 task cards (P1.1–P1.12) with owner model, scope in/out, dependencies, acceptance criteria |
| **ARCHITECTURE.md** | When working on cross-cutting features, agent protocol, artifact pipeline, or deployment topology | 4-layer architecture, subsystem details, agent protocol, queue layout, deployment topology |
| **DATABASE_SCHEMA.md** | When creating migrations or writing queries | Complete schema (P1-P4 tagged), index strategy, conventions |
| **DEVELOPMENT_PLAYBOOK.md** | When implementing any feature | Step-by-step patterns: API endpoints, state transitions, providers, asynq tasks, artifact build, agent task types, probes, migrations, Vue pages |
| **FRONTEND_GUIDE.md** | When adding or modifying any Vue page | Design system rules: shared components, tokens, status colors |
| **TESTING.md** | When writing or modifying tests | Test patterns, mock strategies, coverage requirements |
| **docs/adr/0003-...** | **Always** — current ground-truth ADR | The 2026-04-09 pivot from GFW failover to PRD-aligned generic release platform |
| **docs/adr/0001-...** | Historical only — DO NOT use as ground truth | **Superseded by ADR-0003.** Describes the GFW failover system that was never built |
| **docs/adr/0002-...** | Historical only — DO NOT use as ground truth | **Superseded by ADR-0003.** Methodology (single write path, idempotency) was preserved; GFW-specific decisions are dead |
| **PRD (canonical)** | When in doubt about what to build | `/Users/ahern/Documents/AI-tools/Domain Lifecycle & Deployment Platform（域名生命週期與發布運維平台）.md` — the source of truth for product scope |

> **When the PRD conflicts with any other document, the PRD wins.**
> When ADR-0003 conflicts with any other document, ADR-0003 wins.
> ADR-0001 and ADR-0002 are historical and **never** ground truth.

---

## Model Selection Policy

> Which Claude model should run which task. This is the single source of
> truth — if `PHASE1_TASKLIST.md` assigns an owner model to a card, it is
> derived from the rules below.

Claude Code sessions should pick a model based on the **risk and
reversibility** of the work, not on "how big" it feels. Size is a poor
proxy for safety; blast radius is the right one.

### Use **Opus** when

The work touches **safety-critical, non-local, hard-to-reverse** logic.
A bug here silently corrupts state, leaks across subsystems, or can only
be caught by running the system under load. Specifically:

- **Any of the three state machines and their `Transition()` write paths**
  (CLAUDE.md Critical Rule #1, ADR-0003 D9):
  - `internal/lifecycle/statemachine.go` + `Service.Transition()` +
    `store/postgres/lifecycle.go::updateLifecycleStateTx` +
    `make check-lifecycle-writes` CI gate
  - `internal/release/statemachine.go` + `Service.TransitionRelease()` +
    `store/postgres/release.go::updateReleaseStatusTx` +
    `make check-release-writes` CI gate
  - `internal/agent/statemachine.go` + `Service.TransitionAgent()` +
    `store/postgres/agent.go::updateAgentStatusTx` +
    `make check-agent-writes` CI gate
- **The Pull Agent binary safety boundary** (CLAUDE.md Critical Rule #3,
  ADR-0003 D3). Any change to `cmd/agent/`, especially anything touching
  `os/exec`, file I/O outside `{deploy_path}`, network calls to non-fixed
  URLs, or the four hard-coded shell-out points. The `make
  check-agent-safety` gate must stay green.
- **Artifact format and immutability contract** (CLAUDE.md Critical Rule #2,
  ADR-0003 D4). The `pkg/agentprotocol/manifest.go` schema, the signature
  scheme, the store-layer enforcement of `signed_at` immutability, and the
  reproducibility test in `internal/artifact/builder_test.go`.
- **Agent ↔ Control Plane wire protocol** in `pkg/agentprotocol/types.go`.
  Any field rename, removal, or semantic change requires changing both
  `cmd/agent` and `cmd/server` in the same PR — Opus territory.
- **DNS provider interface signatures** (not the concrete implementations —
  see Sonnet below). Any change to `pkg/provider/dns/provider.go` freezes
  the shape of every current and future provider.
- **Initial schema decisions and migration ordering.** During Phase 1 the
  pre-launch edit-in-place exception is in effect; after cutover every
  migration is permanent. Schema decisions belong to Opus during Phase 1
  and **always** to Opus after cutover.
- **ADR authoring.** New ADRs or revisions to existing ones.
- **Cross-cutting refactors** that touch ≥ 3 subsystems at once.
- **Race conditions, transaction boundaries, and lock ordering.** Any
  place where "two goroutines do this at the same time" is a legitimate
  question — release dispatch, agent task claim, state transitions, etc.
- **Any task that requires holding 2+ of CLAUDE.md / an ADR / the schema /
  the playbook in mind simultaneously** to avoid drift.

### Use **Sonnet** when

The work is **well-specified, locally scoped, and mechanically reversible**.
The spec already exists in CLAUDE.md, `DATABASE_SCHEMA.md`, or
`DEVELOPMENT_PLAYBOOK.md`; the job is to translate that spec into code
without inventing new patterns. Specifically:

- Implementing a new API endpoint that follows `DEVELOPMENT_PLAYBOOK.md` §1
  (handler → service → store → router → tests).
- Writing store methods (`sqlx` queries, struct scanning, transactions)
  whose schema is already defined in `DATABASE_SCHEMA.md`.
- Writing migrations whose SQL is already specified in `DATABASE_SCHEMA.md`.
  (Deciding **what** the schema should be → Opus. Typing it out → Sonnet.)
- Implementing **concrete** DNS providers (cloudflare.go, aliyun.go, …)
  once the interface is frozen — the vendor SDK calls are the hard part,
  not the architecture.
- Writing `asynq` task handlers whose payload shape and queue are already
  decided.
- Implementing the artifact build pipeline's rendering loop (the Builder
  type itself — Opus owns the contract; Sonnet writes the loop).
- Implementing the agent control-plane handlers (`Register`, `Heartbeat`,
  `PullNextTask`, `ReportTask`) once the wire protocol package is frozen.
- Registering routes, wiring middleware stacks, boilerplate composition
  in `cmd/*/main.go`.
- Vue 3 views, API clients, Pinia stores, TypeScript DTO mirroring.
- Table-driven unit tests and contract-test helpers.
- Auth: JWT sign/verify, password hashing, RBAC middleware — standard
  patterns with well-known pitfalls.

### Use **Haiku** when

The work is **surface-level mechanical** — no design decisions, no
reading of business rules:

- Code formatting, import reorganization, lint fixes.
- Renaming a variable consistently across files.
- Adding missing `// Package foo ...` doc comments.
- Typo fixes in docs.
- Moving a file and updating import paths.

### Escalation rule

If a Sonnet session discovers mid-task that it's about to:

1. Touch anything in the **Opus list** above, or
2. Add an `UPDATE domains SET lifecycle_state` outside
   `store/postgres/lifecycle.go::updateLifecycleStateTx`, or
3. Add an `UPDATE releases SET status` outside
   `store/postgres/release.go::updateReleaseStatusTx`, or
4. Add an `UPDATE agents SET status` outside
   `store/postgres/agent.go::updateAgentStatusTx`, or
5. Add an `os/exec.Command(...)` to `cmd/agent/` that isn't one of the
   four hard-coded calls, or
6. Invent a new pattern not in CLAUDE.md or the playbook, or
7. Make a decision that feels like "I'm guessing what the user wants
   here" on an architectural question,

...it should **stop, summarize what it found, and ask the user to switch
to Opus** rather than forging ahead. Half-done tasks are cheap to resume;
silently wrong architecture is expensive to unwind.

Conversely, if an Opus session is burning context on mechanical work
(long stretches of "implement this exact SQL from the schema doc"), it
should suggest the user **switch back to Sonnet** for the typing-heavy
parts. Opus is the scarce resource — spend it on decisions, not
translation.

### Anti-patterns (do not do these)

- **"I'll use Opus because this task is important."** Importance is not
  the criterion; reversibility and cross-cutting impact are. Writing the
  frontend login page is important but not Opus-worthy.
- **"I'll use Sonnet because the state machine is only 30 lines."** Line
  count is not the criterion. 30 lines of state machine + 1 race test +
  1 CI gate is Opus work regardless of size.
- **"Claude is Claude, the model doesn't matter."** For tasks on the Opus
  list, the cost of a subtle bug ≫ the cost difference between models.
  For tasks on the Sonnet list, Opus is wasted budget.
- **"I'll just add `os/exec.Command` to the agent because it's easier."**
  No. The agent's safety boundary is structural, not configurational.
  Find another way, or escalate to Opus.

---

## Phase Roadmap

Phases match PRD §28 with Phase 1 expanded to include the platform
foundation. Detailed task cards live in `PHASE1_TASKLIST.md` for Phase 1;
Phase 2-4 will get their own task lists when Phase 1 cuts over.

| Phase | Headline scope | Key gates |
|---|---|---|
| **Phase 1** | Project / Domain Lifecycle / Template / Artifact build / Basic Release / Agent (register / heartbeat / pull / report) | End-to-end demo: log in → create project → register domain → publish template → create release → agent applies → see in UI |
| **Phase 2** | Sharding / Rollback / Dry-run / Diff / Per-host concurrency limit / Agent management UI (drain / disable) | Multi-shard releases work; rollback works; agent fleet manageable |
| **Phase 3** | Gray release (canary) / Probe L1+L2+L3 / Alert engine with dedup / Agent canary upgrade | Releases gated by probe; alerts dedupe; agents self-upgrade |
| **Phase 4** | Domain lifecycle approval flow / Nginx artifact deployment as separate type / High availability | Prod releases require approval; nginx releases require Release Manager; HA |
| **(Future, unscheduled)** | GFW failover vertical (separate ADR) | N/A |

The detailed Phase 1 dependency graph and task cards are in
`docs/PHASE1_TASKLIST.md`. **Read that document before starting any Phase 1
implementation work.**

---

## Critical Rules (Never Violate)

> The full list with rationale lives in `CLAUDE.md` §"Critical Business Rules".
> This is the short pointer.

1. **State machines have ONE write path each.** Lifecycle / Release / Agent —
   each goes through a single `Transition*()` method. Three CI grep gates
   enforce this. (CLAUDE.md Rule #1)
2. **Artifacts are immutable.** Once `signed_at` is set, no UPDATE allowed.
   Rollback = redeploy a previous artifact_id. (CLAUDE.md Rule #2)
3. **Agent only does whitelisted actions.** Structurally enforced — the
   binary literally cannot run arbitrary commands. `make check-agent-safety`
   gates this. (CLAUDE.md Rule #3)
4. **Releases are scoped to one project.** Cross-project releases are not
   supported. (CLAUDE.md Rule #4)
5. **Production releases require approval.** `is_prod` projects + nginx
   releases need a granted `approval_requests` row. (CLAUDE.md Rule #5;
   Phase 4 enforces; Phase 1 has auto-approve path)
6. **Every artifact deploy snapshots the previous state.** Agent copies
   current files to `.previous/{release_id}/` before swap. (CLAUDE.md Rule #6)
7. **Nginx reload is batched per host.** 30s buffer or 50 domains, whichever
   first. Emergency rollback skips buffer. (CLAUDE.md Rule #7)
8. **Alerts deduplicate.** Same target + type + severity → 1 alert/hour max.
   (CLAUDE.md Rule #8)
9. **`template_versions` are immutable per published version.** Editing a
   template = publishing a new version. Releases pin to versions. (CLAUDE.md
   Rule #9)
10. **mTLS for agents, JWT for management console.** The two auth schemes
    are isolated. An agent cert can't access `/api/v1/*`; a user JWT can't
    access `/agent/v1/*`. (CLAUDE.md Rule #10)
11. **TimescaleDB only for `probe_results`.** Business tables are regular
    PostgreSQL. (CLAUDE.md Rule #11)
12. **Pre-launch migration exception in effect during Phase 1.** Edit
    `000001_init.up.sql` in place; after cutover this window closes
    permanently. (CLAUDE.md Rule #12)

---

## Implementation Priorities (Per Feature)

When building any feature, always implement in this exact order:

```
1. Database migration       (schema first — defines the contract)
2. Store layer              (data access, SQL queries, transactions)
3. Service layer            (business logic + state machine where applicable)
4. asynq task handler       (if the operation is async)
5. API handler              (parse → call service → format response)
6. Route registration       (with correct RBAC middleware)
7. Unit tests               (service layer first, then handler)
8. Vue frontend page        (last — depends on stable API)
```

For state-machine code, the order is:

```
1. Validity map in statemachine.go         (the rules)
2. Sentinel errors in errors.go
3. Unexported updateXxxTx in store layer   (the only write path)
4. Service.Transition*() method            (orchestrates the tx)
5. Race test with -race -count=50          (mandatory)
6. CI grep gate in Makefile                (mandatory)
7. Caller integration                       (now that the path is safe)
```

---

## Key Architectural Decisions (Never Revisit Without Discussion)

| # | Decision | Choice | Reason |
|---|----------|--------|--------|
| 1 | Pagination strategy | Cursor-based (by id, base64 encoded) | Scale-friendly |
| 2 | Enum storage | VARCHAR + CHECK constraint | Avoids ALTER TYPE migration pain |
| 3 | External ID | UUID only in API responses, never BIGSERIAL | Security, prevents enumeration |
| 4 | Audit log location | Service layer, NOT middleware | Middleware doesn't know target UUID |
| 5 | DNS provider abstraction | Required (`pkg/provider/dns`) | Lifecycle module needs vendor flexibility |
| 6 | CDN abstraction | **NOT in this platform** | CDNs sit in front of nginx; managed externally |
| 7 | Artifact storage | MinIO/S3 with content-addressed paths | Immutability + auditability |
| 8 | Agent communication | Pull-based mTLS HTTPS | Industry standard for fleet management |
| 9 | Agent language | Go (single static binary) | Per ADR-0003 D3 + PRD §4 |
| 10 | Single write paths | Three state machines, three CI gates | Per ADR-0003 D9 (methodology from ADR-0002 D2) |
| 11 | Vue routing | History mode | Caddy must have try_files fallback rule |
| 12 | Login identifier | `username` (not email) | Internal operator accounts; ADR-0001 D1 preserved through ADR-0003 |
| 13 | SPA cache | `index.html` served `Cache-Control: no-cache` | Vite hashed chunks + post-deploy stale manifest avoidance; ADR-0001 D12 preserved |

---

## Common Mistakes to Avoid

1. **Don't create God services.** Each service handles one concept.
   `lifecycle.Service` handles domain state. `release.Service` handles
   releases. `agent.Service` handles agent fleet. They call each other
   through interfaces.

2. **Don't put business logic in handlers.** Handlers only: parse request →
   call service → format response. Validation beyond struct tags goes in
   the service.

3. **Don't create DTOs for everything.** Internal service-to-service
   communication uses domain models directly. DTOs only at API boundary.

4. **Don't forget transactions.** Any operation that writes to multiple
   tables MUST use a transaction. Pass `*sqlx.Tx` through the call chain.

5. **Don't ignore the down migration.** Every `up.sql` needs a working
   `down.sql`. Test it.

6. **Don't hardcode provider-specific logic.** If you find yourself writing
   `if provider == "cloudflare"` in business logic, that logic belongs
   inside the provider implementation.

7. **Don't use `time.Sleep` for waiting.** Use tickers, timers, or channels
   for periodic operations. Use context deadlines for timeouts.

8. **Don't skip graceful shutdown.** server / worker / agent all need
   proper signal handling.

9. **Don't bypass `Transition()` methods.** Even "I just need to update
   the status quickly for a test" — write a test fixture that uses
   `Transition()`. Bypassing creates a precedent that the next person
   will follow.

10. **Don't add code paths to `cmd/agent/` that aren't on the whitelist.**
    Need a new agent capability? Discuss it with Opus first; the safety
    boundary is structural.

11. **Don't put timestamps or random IDs in artifact content.** Reproducible
    builds are mandatory. Time/random go in the manifest, never in rendered
    files.

12. **Don't reach for the GFW design.** Switcher, pool, prefix-based
    subdomains, CN probe nodes, double-locking — all dead per ADR-0003 D11.
    If a problem feels like it needs them, you're solving the wrong
    problem (or it's the future GFW vertical, which is unscheduled).

---

## How to Ask for Help

When you encounter ambiguity in the spec, check documents in this order:

1. **PRD** (canonical product source of truth)
2. **CLAUDE.md** (conventions, critical rules, state machines)
3. **ADR-0003** (current architecture decisions)
4. **ARCHITECTURE.md** (subsystem details)
5. **DEVELOPMENT_PLAYBOOK.md** (implementation patterns)
6. **DATABASE_SCHEMA.md** (data model)
7. **PHASE1_TASKLIST.md** (Phase 1 task cards with scope boundaries)

If the answer isn't in any document, make a reasonable decision following
these principles:

- Prefer simplicity over cleverness
- Prefer explicit over implicit
- Prefer composition over inheritance
- Prefer failing fast over silent degradation
- Log the decision as a code comment for future reference
- If the decision is non-trivial or affects multiple subsystems, **stop and
  ask the user before proceeding**, and propose to capture the answer as
  a new ADR
