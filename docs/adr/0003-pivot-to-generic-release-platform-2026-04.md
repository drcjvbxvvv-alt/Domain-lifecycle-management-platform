# ADR-0003: Pivot from GFW failover system to generic HTML+Nginx release platform

- **Status**: Accepted
- **Date**: 2026-04-09
- **Scope**: `CLAUDE.md`, `docs/ARCHITECTURE.md`, `docs/DATABASE_SCHEMA.md`, `docs/DEVELOPMENT_PLAYBOOK.md`, `docs/PHASE1_TASKLIST.md`, `docs/CLAUDE_CODE_INSTRUCTIONS.md`, `README.md`
- **Supersedes**: ADR-0001, ADR-0002 (both retained as historical record, marked superseded)

---

## Context

Through 2026-04-08, the project documents (CLAUDE.md, ARCHITECTURE.md, ADR-0001,
ADR-0002, PHASE1_TASKLIST.md) collectively described an architecture purpose-built
for one specific business problem:

> Manage 12,000+ domains across 10 projects, ensuring continuous reachability
> from mainland China with **< 2 min detection** and **< 5 min automated failover**
> when the Great Firewall blocks them.

This architecture had a very specific shape:

- **`prefix_rules`** as the central organizing concept (DNS provider, CDN provider,
  nginx template, HTML template all derived from a subdomain prefix)
- **`main_domains` + `subdomains`** hierarchical model
- **`main_domain_pool`** with `pending → warming → ready → promoted` lifecycle for
  pre-warmed standby domains
- **`internal/switcher`** auto-failover engine with Redis + Postgres double lock
  (ADR-0002 D1)
- **`internal/probe`** + `cmd/scanner` deployed to mainland China nodes for GFW
  block detection (DNS poisoning, TCP block, SNI block, HTTP hijack)
- **`pkg/svnagent`** Python client for SVN-based deployment
- **L1 60-second probe cycle** with majority confirmation, TTL=60 hard rule, 2 of 3
  CN nodes for confirmation

ADR-0001 and ADR-0002 deepened this design with 13 + 6 decisions (state machine
single write path, prefix soft-freeze, switch lock fallback, CDN idempotency,
queue priorities, pool lifecycle).

On 2026-04-09 the user confirmed during an alignment conversation that **none of
the above requirements were ever specified by the user**. The "12,000 domains",
"GFW failover", "< 2 min detection", "< 5 min failover", "standby pool",
"prefix-based subdomain" concepts were all introduced by an earlier AI session
that extrapolated from a more general "domain management platform" prompt and
did not validate its assumptions with the user.

The user then provided the actual product requirements document at:

> `/Users/ahern/Documents/AI-tools/Domain Lifecycle & Deployment Platform（域名生命週期與發布運維平台）.md`

This document describes a **completely different system** — a generic enterprise
HTML + Nginx release platform with a Pull Agent execution model, artifact-based
deployment, and a domain lifecycle module that is *prerequisite to* (not part of)
the deployment system.

This ADR records the decision to pivot the entire project to the new PRD,
declares the previous design superseded, and defines what is preserved versus
discarded.

---

## Decisions

### D1 — The new PRD is the single source of truth

Effective immediately, the file
`/Users/ahern/Documents/AI-tools/Domain Lifecycle & Deployment Platform（域名生命週期與發布運維平台）.md`
is the canonical product specification. Where any other document conflicts with
the PRD, the PRD wins.

The previous CLAUDE.md / ARCHITECTURE.md / DATABASE_SCHEMA.md /
DEVELOPMENT_PLAYBOOK.md / PHASE1_TASKLIST.md are rewritten in this commit batch
to align with the PRD. ADR-0001 and ADR-0002 are not deleted but are marked as
superseded by this ADR; they remain in the repository as historical record of
what the system *would have looked like* under the previous direction.

**Why**: Multiple sources of truth diverging by 6+ critical dimensions (deployment
mechanism, agent model, data model, HTML scope, storage layer, domain lifecycle
location) is unsustainable. One canonical document must win.

### D2 — Architecture: 4-layer Control Plane / Task & Data / Execution / Pull Agent

Per PRD §2, the new architecture has four layers:

1. **Control Plane** (`cmd/server`) — UI, REST API, AuthN/AuthZ, project /
   domain / template / release management, agent management, scheduler
2. **Task & Data Layer** — PostgreSQL (business + metadata), Redis (queue broker,
   short-lived state), MinIO/S3 (immutable artifacts), TimescaleDB (probe results
   time-series — preserved from previous design but repurposed)
3. **Execution Plane** (`cmd/worker`) — asynq workers running release executors,
   artifact build pipelines, probe dispatchers, notify dispatchers
4. **Node Executor / Pull Agent** (`cmd/agent`) — single Go binary deployed to
   each Nginx server. Registers with the control plane, heartbeats, pulls tasks,
   downloads artifacts, verifies checksum/signature, writes files, runs
   `nginx -t`, conditionally reloads, reports results

This replaces the previous "Go monolith + Python SVN agent + CN scanner nodes"
topology entirely.

**Why**: Pull-based agents with artifact-based deployment is the industry-standard
pattern for release platforms (Spinnaker, ArgoCD, Octopus Deploy, Harness). It
gives immutability, auditability, observability, and fleet manageability. The
previous SVN-based push model relied on Python `svn-agent` which had no state
machine, no health model, no upgrade story, and no security boundary beyond
"some script on a server we can SSH into".

### D3 — Agent is a Go binary, not a Python script

Per PRD §4, the Pull Agent is implemented in Go as a single static binary
deployed via systemd. This replaces the previous `deploy/svn-agent/` Python
implementation (which was never written but was the target).

The Agent has:

- **State machine**: `registered → online ↔ offline / busy / idle / draining /
  disabled / upgrading / error` (PRD §6.3)
- **Whitelisted actions only** (PRD §5): no shell, no git/svn, no decisions,
  no cross-host operations, no third-party credentials
- **mTLS authentication** with per-agent certificate, rotation, revocation
  (PRD §19.1)
- **Self-upgrade with canary + rollback** (PRD §21)
- **Drain / disable / quarantine** operations from the control plane (PRD §6.6)

**Why**: A Python SVN agent is structurally incapable of meeting the safety,
observability, and fleet-management requirements PRD §6 + §20 + §21 specify.
Go is the only sane choice; the PRD itself recommends it.

### D4 — Artifact-based deployment with MinIO/S3 storage

Per PRD §8, deployments work in terms of **immutable artifacts**:

```
artifacts/
  project-a/
    release-2026-04-09-001/
      manifest.json   # artifact_id, project, release_id, template_version,
                      # domain list, checksum, created_at, source commit
      checksums.txt
      domains/
        a.example.com/index.html, assets/...
        b.example.com/index.html, assets/...
      nginx/
        host-group-01/
          a.example.com.conf
          b.example.com.conf
```

Properties (PRD §8.2): immutable, versioned, auditable, checksum-verifiable,
signature-verifiable, rollback-supporting, probe-correlatable.

The control plane builds artifacts from templates + variables. The agent
downloads artifacts from MinIO/S3, verifies checksum + signature, deploys, and
reports. Rollback = redeploy a previous artifact, never rebuild.

This replaces the previous "render template → SVN commit → SVN agent svn up"
model.

**Why**: Artifact immutability is a powerful invariant that the SVN model lacks.
"What is currently deployed" becomes a single hash, not "whatever revision the
target machine happened to check out". Rollback becomes O(redeploy) instead of
O(rerun-the-pipeline).

### D5 — HTML and Nginx are governed separately

Per PRD §10, HTML and Nginx releases are first-class but distinct:

- `releases.release_type IN ('html', 'nginx', 'full')`
- HTML can be released frequently
- Nginx releases are conservative, require diff review, require higher RBAC role
- Both go through the artifact pipeline; the artifact format embeds the type

The previous design treated `nginx_template` and `html_template` as fields on
`prefix_rules`, deployed together via one release flow. That conflation is a
category error and is removed.

**Why**: HTML and Nginx have different change frequencies, different risk
profiles, different rollback semantics, and different audit requirements.
Treating them as one is correct only for the GFW use case where every change
touches both. For a generic platform, separation is correct.

### D6 — Domain Lifecycle is a separate module, not inlined

Per PRD §17, the Domain Lifecycle module manages domain registration / approval
/ provisioning *as a prerequisite* to the deployment system, with its own state
machine:

```
requested → approved → provisioned → active → disabled
                                       │
                                       ▼
                                    retired (terminal)
```

Only `active` domains can be the target of a release.

This replaces the previous `main_domains.status` machine
(`inactive → deploying → active → degraded → switching → blocked → retired`)
which conflated domain identity with deployment state.

**Why**: A domain's "does it exist and is it ours to manage" lifecycle is
orthogonal to "what's currently deployed to it". The previous design fused these
two concerns because the GFW use case made it convenient. The PRD separates
them, which is the cleaner abstraction.

### D7 — Five RBAC roles, approval flow for production

Per PRD §18, the role model is:

| Role | Powers |
|---|---|
| **Viewer** | Read-only |
| **Operator** | Create/edit non-prod releases, run dry-run |
| **Release Manager** | Approve and trigger prod releases, pause/rollback |
| **Admin** | Manage projects, users, agents, system config |
| **Auditor** | Read-only including audit logs and approval history |

Production releases require approval (PRD §18 — "prod 發布需審批"). Nginx
releases require Release Manager or Admin. Rollback requires recorded reason.

The previous design had only `admin` / `operator` and no approval flow.

**Why**: 5,000+ domain enterprise platforms typically have multiple operators,
release managers, audit/compliance pressure. Two roles are insufficient. The
approval flow is also a hard requirement for any system that touches production
nginx config — it's not optional.

### D8 — Probe is for deployment verification, not GFW detection

Per PRD §15, probing has three tiers:

| Tier | Target | Checks |
|---|---|---|
| **L1** | Every domain | DNS resolves, TCP :443, HTTP status, HTTPS handshake, response time |
| **L2** | Domains in current release | Expected `<meta name="release-version">`, expected title/keyword, content checksum |
| **L3** | Tagged core domains | Business endpoint health, specific API/resource availability |

L2 verifies that *the artifact actually deployed* by checking the embedded
release version meta tag in HTML. Probe is now part of the **release verification
loop** (PRD §11 step 8: "Probe 驗證") rather than a continuous GFW-blocking
detector.

The previous design's mainland-China probe nodes (CT/CU/CM ISPs) and the
"DNS poisoning / TCP block / SNI block / HTTP hijack" detection logic are out
of scope. They will return as part of a future GFW vertical (see D11 below).

**Why**: The PRD's probe is about "did my deployment actually take effect", not
"is this domain reachable from China". Same general concept (HTTP probing),
totally different detection logic and totally different SLA targets.

### D9 — Engineering discipline from the previous design is preserved

The following patterns from the previous design are **portable engineering
discipline** and survive the pivot, applied to the new domain objects:

1. **Single write path for state machines**. ADR-0002 D2 codified
   `internal/domain.Service.Transition()` as the only place that writes
   `main_domains.status`. The same pattern now applies to the three new state
   machines:

   - `internal/lifecycle.Service.Transition()` — only place that writes
     `domains.lifecycle_state`
   - `internal/release.Service.TransitionRelease()` — only place that writes
     `releases.status`
   - `internal/agent.Service.TransitionAgent()` — only place that writes
     `agents.status`

   Each guarded by a `make check-*-writes` CI grep gate, each implemented with
   `SELECT ... FOR UPDATE` and a state-history audit row.

2. **ADR culture**. Every architectural decision continues to be recorded as a
   numbered ADR with Context / Decision / Consequences / Follow-up sections.

3. **Idempotency contracts**. ADR-0002 D4 required CDN `CloneConfig` to be
   idempotent because asynq retries would otherwise leave half-state. The same
   contract now applies to:

   - **Artifact build steps**: same input → same output hash, deterministic
   - **Agent task application**: applying the same task twice yields the same
     filesystem state
   - **Nginx reload**: idempotent within an aggregation window

4. **Race tests with `-race -count=50`**. The Transition() pattern is verified
   under concurrent callers; the same test pattern is mandatory for the three
   new state machines.

5. **Pre-launch migration exception** (ADR-0001 + ADR-0002 explicitly carried
   over). The initial migration may be edited in place during Phase 1 because
   no production data exists yet. This window closes at Phase 1 cutover, after
   which every schema change is a new numbered migration file.

6. **Model Selection Policy** in `docs/CLAUDE_CODE_INSTRUCTIONS.md`. The Opus /
   Sonnet / Haiku decision criteria added on 2026-04-08 (commit `30280dc`) are
   universal and survive the pivot unchanged. The new state machine + agent
   protocol + artifact contract tasks are added to the Opus list.

**Why**: The IP from the previous design is not the GFW-specific architecture —
it's the *engineering discipline*. That discipline applies to any platform.
Throwing it away because the architecture changed would be a category error.

### D10 — `cmd/scanner` is parked, `cmd/agent` is added

The existing `cmd/scanner` directory and stub `main.go` are left in place but
removed from the default `make build` target. They will be the entry point for
the future GFW vertical (D11) when that work is taken up.

A new `cmd/agent` directory is added containing the Go Pull Agent binary
described in D3. It is cross-compiled for `linux/amd64` (Makefile target
`make agent`) and is the primary new artifact of Phase 1.

`pkg/svnagent` (the Python SVN agent client package) is removed from the
project structure documentation. The directory may remain on disk as a stub
but is not referenced by CLAUDE.md, ARCHITECTURE.md, or any code.

**Why**: Adding `cmd/agent` is unavoidable. Removing `cmd/scanner` would lose a
useful future placeholder. Renaming or repurposing them would create confusion
when GFW work resumes.

### D11 — GFW failover is explicitly out of scope, parked as a future vertical

The entire GFW failover system — `internal/switcher`, `internal/pool`,
`internal/probe`'s GFW-specific detection, prefix-based subdomain auto-generation,
the `main_domain_pool` table, the `switch_history` table, the standby pool
warmup workflow, the auto-switch dispatch logic, and the L1-cycle 60-second
probe SLA — is **removed from Phase 1 through Phase 4**.

When (and if) GFW failover is needed in the future, it will be designed as a
**vertical on top of the new platform**:

- Switcher becomes a special `release_type: failover`
- Standby pool becomes pre-built artifacts in MinIO with a
  `domain_backup_pool` table that points at them
- GFW-specific probe becomes an L4 tier (or a separate `probe_kind: gfw`)
  in the existing probe system
- Mainland China probe nodes become a special agent group with `kind: probe`

This vertical will require its own ADR (ADR-0004 or later, when the work is
scheduled). It is **not** Phase 5 of the current plan — it is unscheduled.

**Why**: Carrying GFW concepts as "we'll get to it eventually" inside Phase 1-4
work would constantly tempt the design back toward the old shape. Explicitly
parking it forces every Phase 1-4 decision to be made on its own merits,
without the GFW use case as gravitational pull.

### D12 — ADR-0001 and ADR-0002 are marked superseded, not deleted

Both ADRs receive a SUPERSEDED header pointing to this ADR. Their status changes
from `Accepted` to `Superseded by ADR-0003 (2026-04-09)`. Their bodies are
preserved unchanged so the historical reasoning remains visible.

**Why**: ADRs are historical records by definition. Deleting them would be
revisionist. Marking them superseded is the standard practice from
[Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record).

Future readers who wonder "why is the codebase missing a switcher?" can read
ADR-0001 + ADR-0002 to understand the previous design, then read ADR-0003 to
understand why it changed.

---

## Consequences

### Positive

- **One source of truth**. The PRD is canonical. CLAUDE.md / ARCHITECTURE.md /
  schema / playbook all derive from it.
- **Industry-standard architecture**. Artifact + Pull Agent + Release is a
  validated pattern with abundant prior art. Onboarding engineers from other
  release-platform teams becomes feasible.
- **Generic foundation**. The new platform supports many use cases (HTML
  publishing, nginx config rollouts, future verticals) without architectural
  contortion.
- **Engineering discipline preserved**. The single-write-path / ADR / race-test
  / idempotency / Model Selection patterns from the previous design are kept,
  applied to new objects. Not lost.
- **GFW vertical remains possible**. The new platform doesn't preclude GFW
  failover; it just makes it a vertical on top instead of the entire system.
- **Honest history**. ADR-0001 + ADR-0002 + this ADR collectively document
  exactly what we believed when, and why we changed our minds.

### Negative / trade-offs

- **The 2026-04-08 alignment work is partially obsolete**. ADR-0001 (13 decisions)
  and ADR-0002 (6 decisions + 3 execution items) put significant thought into a
  system that no longer exists. That thought is not entirely wasted — D9 above
  ports the methodology — but the specific GFW-centric decisions are dead.
- **PHASE1_TASKLIST.md from commit `30280dc` (2026-04-08) is partially obsolete**.
  Roughly 30% of the 9 task cards are directly portable (P1.1 scaffold, P1.7
  asynq worker, P1.8 partial — DNS provider only). The other 70% rewrite around
  new domain objects.
- **The platform is bigger**. The PRD's Phase 1 covers Project / Domain /
  Template / Artifact / Release / Agent — six top-level concerns versus the
  previous Phase 1's three (Project / Prefix Rules / Domain). Phase 1 task count
  grows from 9 to ~12.
- **MinIO/S3 dependency added**. The previous design needed only PostgreSQL +
  Redis. The new design needs MinIO (or any S3-compatible store) for artifacts.
  This is an additional infra component to operate.
- **mTLS adds operational complexity**. Per-agent certificates with rotation
  and revocation is non-trivial. The previous design avoided this by simply not
  having an agent control protocol.
- **Loss of GFW SLA story (intentional)**. The 65-second worst-case detection
  and the 5-minute switch SLA were the most rigorously specified parts of the
  previous design. They are gone. If GFW failover comes back as a vertical,
  those SLAs need to be re-derived in the new architecture's terms.

### Neutral

- **Login flow stays as-is**. The username (not email) login built in
  `af8edbc` and the centered brand layout from `778892f` are universal and
  ship unchanged.
- **Frontend design system stays as-is**. `FRONTEND_GUIDE.md` is universal;
  only the example status names need updating.
- **Tech stack stays Go + Gin + sqlx + asynq + PostgreSQL + Redis + Vue 3 +
  Naive UI + Vite + Pinia + Caddy + Zap + Viper + JWT**, with MinIO added.
  No language or framework swap.

---

## Implementation plan (this ADR's commit batch)

The pivot is shipped as one commit batch covering the following file changes:

1. **Create** `docs/adr/0003-pivot-to-generic-release-platform-2026-04.md`
   (this file)
2. **Mark superseded**: insert SUPERSEDED header in
   `docs/adr/0001-architecture-revision-2026-04.md` and
   `docs/adr/0002-pre-implementation-adjustments-2026-04.md`
3. **Rewrite**: `CLAUDE.md`
4. **Rewrite**: `docs/ARCHITECTURE.md`
5. **Rewrite**: `docs/DATABASE_SCHEMA.md`
6. **Rewrite**: `docs/DEVELOPMENT_PLAYBOOK.md`
7. **Rewrite**: `docs/PHASE1_TASKLIST.md`
8. **Update**: `docs/CLAUDE_CODE_INSTRUCTIONS.md` (Document Map + Phase sections;
   Model Selection Policy preserved)
9. **Update**: `docs/FRONTEND_GUIDE.md` (status name examples only)
10. **Rewrite**: `README.md`
11. **Update**: `Makefile` (drop scanner from default build, add agent target)

No source code is modified. The four `cmd/*/main.go` files are empty stubs and
remain unchanged. The frontend `web/` is unchanged. `go.mod` is unchanged
(MinIO Go SDK will be added as part of P1.7 implementation work, not as part
of this documentation pivot).

---

## Follow-up work (not part of this ADR)

- Add `github.com/minio/minio-go/v7` to `go.mod` when starting P1.7 (artifact
  storage interface + MinIO implementation).
- Define the Agent ↔ Control Plane wire protocol in detail (P1.9 in the new
  task list — Opus task).
- Define the artifact manifest format and signature scheme (P1.8 in the new
  task list — Opus task).
- Define the Domain Lifecycle, Release, and Agent state machines with
  transition matrices and CI grep gates (P1.5, P1.6, P1.10 in the new task
  list — all Opus tasks).
- Choose an artifact signing scheme. Options: cosign (Sigstore), GPG, or a
  custom HMAC. Decision deferred to a separate ADR-0004 once Phase 1 starts
  artifact work.
- Future ADR-XXXX: GFW failover vertical, when scheduled.

---

## References

- **PRD (canonical)**:
  `/Users/ahern/Documents/AI-tools/Domain Lifecycle & Deployment Platform（域名生命週期與發布運維平台）.md`
- **ADR-0001** (superseded): `docs/adr/0001-architecture-revision-2026-04.md` —
  the 13 decisions taken on 2026-04-08 to clean up the GFW-failover design
- **ADR-0002** (superseded): `docs/adr/0002-pre-implementation-adjustments-2026-04.md` —
  the 6 pre-implementation decisions taken on 2026-04-08 (single write path,
  switch lock, prefix soft-freeze, CDN idempotency, queue priorities, pool
  lifecycle)
- **PHASE1_TASKLIST.md** (commit `30280dc`, 2026-04-08) — the previous Phase 1
  plan, now rewritten in this commit batch
- **Project Brain knowledge**: `decision-af6e22bc` — pivot decision recorded
  on 2026-04-09
