# ADR-0002: Pre-implementation adjustments — April 2026

- **Status**: Accepted
- **Date**: 2026-04-08
- **Scope**: `CLAUDE.md`, `docs/ARCHITECTURE.md`, `docs/DATABASE_SCHEMA.md`,
  `docs/DEVELOPMENT_PLAYBOOK.md`, `docs/CLAUDE_CODE_INSTRUCTIONS.md`, `README.md`
- **Builds on**: ADR-0001 (2026-04-08)

---

## Context

Before writing any production code, a second architecture review of the
ADR-0001 output surfaced six gaps that ADR-0001 did not cover:

1. **Silent-bug class: double switch.** The auto-switch flow holds a Redis
   distributed lock `switch:lock:{main_domain_id}` with a 600s TTL. If Redis
   is lost (AOF corruption, instance restart with stale dump, hardware fault),
   two workers on the same `main_domain_id` can acquire the lock
   simultaneously and concurrently rewrite DNS records. ADR-0001 D11
   explicitly tolerated Redis loss as "brief re-alert storms", but the switch
   lock is **not** in the tolerable category — it guards an externally visible
   mutation (DNS + CDN).

2. **No enforcement of the state machine.** `CLAUDE.md` requires all status
   transitions to go through `CanTransition()`, but nothing prevents any
   package (`internal/release`, `internal/switcher`, `internal/pool`) from
   writing `UPDATE main_domains SET status = ...` directly. Pre-launch we can
   still make the state machine the single write path by design; post-launch
   this becomes a much more expensive refactor.

3. **Silent-bug class: prefix_rule drift.** `subdomains` snapshots the
   `dns_provider` / `cdn_provider` / `nginx_template` / `html_template`
   fields at creation time. If an operator later edits a `prefix_rules` row,
   **existing subdomains keep the old values, new subdomains get the new
   values**, and nothing in the system notices. Operators will discover this
   during their first incident, 2 a.m., probably with a coffee in hand.

4. **CDN CloneConfig idempotency is unspecified.** Auto-switch step "CDN
   CloneConfig old → new" is the most critical operation, and asynq retries
   it up to 3 times. If the first attempt half-succeeds (destination domain
   created, config partially copied, then API times out), the second attempt
   must be able to reconcile — otherwise the destination CDN ends up in an
   inconsistent state that only manual cleanup can fix.

5. **asynq queue priorities exist in one doc only.** `CLAUDE_CODE_INSTRUCTIONS.md`
   documents `critical/dns/cdn/deploy/default` queues with priorities
   10/6/6/4/2 and serial-per-server concurrency for `deploy`, but
   `ARCHITECTURE.md` never mentions it. A worker process reading only
   `ARCHITECTURE.md` will not honour this configuration, and the `critical`
   priority that lets auto-switch preempt normal DNS work disappears.

6. **Pool `promoted` has no documented exit.** ADR-0001 D4 renamed the pool
   state machine to `pending → warming → ready → promoted → blocked →
   retired`, but did not describe what happens when a `promoted` domain
   subsequently gets blocked. Does it go straight to `blocked` (then what
   about `main_domains.status`)? Can a `blocked` pool row be un-blocked if
   the block was transient? Can it be re-warmed?

This ADR records the six decisions taken on 2026-04-08 to close these gaps
**before any implementation starts**, so that the initial migration and the
first production code can encode them from day one.

---

## Decisions

### D1 — Switch lock: Redis fast path + Postgres row lock fallback

The switcher's critical section is entered only after **both** locks succeed:

1. **Redis fast path** (fail-fast, cheap):
   ```
   SET switch:lock:{main_domain_id} <worker_id> NX PX 600000
   ```
   If the SETNX fails → another worker owns the switch → return immediately.

2. **Postgres row lock** (durable, authoritative):
   ```sql
   BEGIN;
   SELECT id, status FROM main_domains WHERE id = $1 FOR UPDATE;
   -- Re-validate status allows entering 'switching' via CanTransition()
   ```
   If Redis is unreachable: log warning, skip step 1, rely solely on the PG
   row lock — switch still proceeds safely, just slower.

   If Postgres is unreachable: abort the switch. Postgres is ground truth;
   we do not switch without it.

3. **Release order**: delete the Redis key first, then `COMMIT` the Postgres
   transaction. If the process crashes between these two, the Redis TTL (600s)
   will clean up the orphan lock.

**Why**: Redis alone is a single point of failure for an externally visible
mutation. Postgres alone is correct but slower (row lock holds for the entire
switch cycle ≤ 600s). Together: Redis makes the common path fast, Postgres
makes the failure path safe.

**Implementation location**: `internal/switcher/service.go::acquireSwitchLock()`.

---

### D2 — `main_domains.status` single write path

`main_domains.status` **must only** be mutated through:

```go
// internal/domain/service.go
func (s *Service) Transition(
    ctx context.Context,
    id int64,
    from string,          // expected current status — optimistic check
    to string,
    reason string,
    triggeredBy string,   // "user:{uuid}" | "system" | "probe:{node}" | "switcher" | "release:{uuid}"
) error
```

`Transition()` is the only place `UPDATE main_domains SET status` appears in
the codebase. It atomically (inside a transaction):

1. `SELECT status FROM main_domains WHERE id = $1 FOR UPDATE`
2. Assert `current == from` — otherwise return `ErrStatusRaceCondition`
3. Assert `CanTransition(from, to)` — otherwise return `ErrInvalidTransition`
4. `UPDATE main_domains SET status = $to, updated_at = NOW() WHERE id = $1`
5. `INSERT INTO domain_state_history (main_domain_id, from_status, to_status, reason, triggered_by)`
6. `COMMIT`

**All** callers (`internal/release`, `internal/switcher`, `internal/pool`,
`internal/domain/deployer.go`, probe-triggered auto-mutations) route through
this method. The release scheduler never writes `UPDATE main_domains`
directly. The switcher never writes `UPDATE main_domains` directly. The
deployer never writes `UPDATE main_domains` directly.

**Enforcement**:
- Code review rule (documented in `CLAUDE.md` Critical Business Rule #8).
- Optional CI gate: `grep -r 'UPDATE main_domains SET status' --include='*.go'`
  should return exactly one hit: the query constant inside
  `store/postgres/domain.go::updateStatusTx`, which is only called by
  `internal/domain.Service.Transition`.

**Why**: The state machine is already defined in `internal/domain/statemachine.go`
and every decision document says "use it", but there is no mechanical
enforcement. Encoding the rule before any caller exists means later code
cannot accidentally take the shortcut. Audit history is also guaranteed to
be complete — every transition writes exactly one `domain_state_history` row.

---

### D3 — `prefix_rules` are soft-frozen; drift is resolved by explicit rebuild

Once a `prefix_rules` row has been referenced by any `subdomains` row, its
runtime-affecting fields are **soft-frozen**:

- Frozen fields: `dns_provider`, `cdn_provider`, `nginx_template`, `html_template`
- Editable fields: `purpose` (human-readable label only)
- Immutable always: `(project_id, prefix)` — the composite key

An `UPDATE prefix_rules SET dns_provider = ...` request is allowed, but the
service layer MUST:

1. Detect that existing `subdomains` rows reference this rule
2. Require the operator to also start a **rebuild release** in the same
   request
3. Reject the request with HTTP 409 `prefix_rule_drift_requires_rebuild`
   if the rebuild is not attached

A **rebuild release** is a new `releases` row with `kind = 'rebuild'` (see
DATABASE_SCHEMA.md change). Functionally it is a deploy release with the
same canary + shard machinery, but its input is "all subdomains referencing
a changed prefix_rule" instead of "a chosen domain set". The flow:

1. Operator PUTs `/api/v1/projects/:id/prefix-rules/:prefix` with new values
   + `{rebuild: true}`
2. Service layer creates a `releases` row, `kind='rebuild'`, populated with
   the affected domains
3. Service layer updates the `subdomains` rows AND the `prefix_rules` row in
   the same transaction — so reads see the new values immediately
4. The rebuild release re-renders nginx conf, commits to SVN, reloads, and
   probe-verifies, using the standard canary threshold
5. If the rebuild release fails canary, the operator must either rollback
   (restoring the old prefix_rule + old subdomain values from conf snapshots)
   or roll forward (fix the template and retry)

**Why**: The three alternatives all have worse failure modes:

- *Option A — immutable prefix_rules*: operators cannot fix a bad template
  choice without creating a new prefix and migrating domains. Too rigid.
- *Option B — auto-propagate silently*: high blast radius (one PUT could
  affect thousands of domains with no review or rollback plan).
- *Option C — JOIN at runtime* (no snapshot on subdomains): forces every
  deploy path to re-read prefix_rules, and loses the ability to audit "what
  config was actually deployed for this domain on date X".

Soft-freeze + explicit rebuild forces operators to acknowledge blast radius
(by choosing canary size), gives the change full audit coverage through the
standard release pipeline, and leaves the normal deploy path unchanged.

**Migration impact**: Add `releases.kind VARCHAR(20) NOT NULL DEFAULT 'deploy'`
with `CHECK (kind IN ('deploy', 'rebuild'))`.

---

### D4 — CDN CloneConfig idempotency is mandatory

Every `cdn.Provider` implementation's `CloneConfig(ctx, src, dst) error`
method MUST be idempotent: calling it twice with the same `(src, dst)` has
the same observable result as calling it once.

Concretely:

1. Before copying, the implementation MUST check whether `dst` already exists
   on the provider side:
   - If not: create + copy + return
   - If yes, with identical config: return nil (treat as success)
   - If yes, with different config: overwrite or delete-then-recreate, at the
     implementation's discretion, but must converge
2. Partial failures must be self-healing on retry:
   - "Destination created, SSL cert not yet attached" → retry attaches cert
   - "Destination created, origin not yet set" → retry sets origin

Providers that cannot support this (because their API does not expose "get
current config") MUST implement it via `GetDomainStatus + ListRules` before
`CloneConfig`, and **must declare themselves non-idempotent at registration
time** — the registry rejects them for use in the auto-switch path.

**Why**: asynq retries the switch task up to 3 times. A half-succeeded
`CloneConfig` that cannot be completed on retry will:
- Burn all 3 attempts
- Escalate to P0 "auto-switch failed"
- Leave the destination CDN in a state only manual `cdnctl` can clean up

The idempotency requirement moves this concern into the provider layer,
which is the only place that knows the vendor's API surface.

**Implementation location**: each `pkg/provider/cdn/*.go` file. Test
coverage requirement: each provider MUST have a unit test
`TestCloneConfig_Idempotent` that calls CloneConfig twice in sequence
against the same mock server and asserts identical final state.

---

### D5 — asynq queue priority is documented in ARCHITECTURE.md (cross-referenced from CLAUDE_CODE_INSTRUCTIONS.md)

The queue layout from `CLAUDE_CODE_INSTRUCTIONS.md` Phase 2.4 is hereby
canonical and reflected in `ARCHITECTURE.md §2.3`:

| Queue | Tasks | Priority weight | Concurrency |
|---|---|---|---|
| `critical` | `switch:execute`, `probe:verify` | 10 | 20 |
| `dns` | `dns:*` | 6 | 10 |
| `cdn` | `cdn:*` | 6 | 10 |
| `deploy` | `svn:*`, `agent:*`, `nginx:*` | 4 | 5 (serial per server) |
| `default` | `template:*`, `pool:*` | 2 | 10 |

`strict: false` (weighted, not strict-priority) — this is important: strict
priority would starve pool warmup under high DNS load.

**Why**: The priority configuration is load-bearing for the auto-switch SLA.
If `switch:execute` ends up in the same queue as routine DNS work, a large
release burning through DNS quota will starve the switcher under exactly
the condition when the switcher is most needed (widespread blocking often
correlates with large traffic spikes). Documenting this in `ARCHITECTURE.md`
(the reference document for cross-cutting concerns) ensures `cmd/worker/main.go`
cannot be built with an inconsistent queue layout.

**Implementation location**: `cmd/worker/main.go::asynq.Config.Queues`.

---

### D6 — Pool `promoted` lifecycle is complete

The pool state machine extends beyond the naive terminal:

```
pending ──→ warming ──→ ready ──→ promoted
   ▲           │          │          │
   │           ▼          ▼          ▼
   └─────── pending (retry)      blocked
                                     │
                              ┌──────┴──────┐
                              ▼             ▼
                          retired       pending
                         (terminal)    (operator
                                        un-block,
                                        restart warming)
```

**Semantics**:

- `pending`: in the backlog, not yet being prepared.
- `warming`: warmup worker has picked it up; DNS/CDN being configured.
  On failure → back to `pending` with `warmup_attempts++` and
  `warmup_last_error` set (exponential backoff: 1min, 5min, 15min, then stops
  retrying and requires manual re-queue).
- `ready`: DNS + CDN + probe verification all pass. Eligible for promotion.
- `promoted`: a switcher has swapped it in. There is now exactly one
  corresponding `main_domains` row with the same `domain` string. The pool
  row and the `main_domains` row are kept in sync by the switcher — any
  transition of the `main_domains` row (e.g. to `blocked`) is mirrored to
  the pool row through a call to `pool.Service.OnMainDomainBlocked()`.
- `blocked`: the domain was promoted but has since been blocked. **This is
  not terminal.** Operator review decides next step:
  - Permanently burned (e.g. hit GFW keyword list) → `retired`
  - Transient block (e.g. DNS provider outage) → `pending` + increment
    `warmup_attempts`, re-enter the warming flow from scratch
- `retired`: terminal. The domain string is not reused; a future warmup of
  the *same string* would require a new pool row with the same `domain`
  value, which is rejected by the `uq_pool_domain` unique index. This is
  intentional: "retired" means "never use this domain again".

**Invariant**: At any point in time, for every `pool.status = 'promoted'` row,
there is exactly one `main_domains` row with `main_domains.domain = pool.domain`
and `main_domains.status NOT IN ('retired')`. This invariant is the
switcher's responsibility to maintain; any code path that violates it must
be treated as a bug.

**Why**: Without `promoted → blocked → pending` reentrancy, a transiently
blocked domain would be permanently lost from the pool, wasting the cost of
its original warmup. Without a documented `promoted → blocked` at all, the
switcher has no legitimate way to represent the post-switch state of a pool
row, and operators would see stale `promoted` rows forever.

**Implementation location**: `internal/pool/service.go::OnMainDomainBlocked()`,
`internal/pool/service.go::Unblock()`, `internal/pool/service.go::Retire()`.

---

## Execution items (not decisions, but must ship before Phase 1 cutover)

E1. **Load-test the 60s L1 cycle on a 1C/1G probe box with 12K synthetic
    domains** before Phase 1 goes live. (This item was already listed in
    ADR-0001 D6 follow-up work; re-stated here so the Phase 1 cutover
    checklist captures it.)

E2. **Add a CI grep gate** that rejects any PR containing
    `UPDATE main_domains SET status` outside of `store/postgres/domain.go`.
    (Enforces D2.)

E3. **Write a disaster drill script** that simulates Redis loss during a
    switch and verifies D1's Postgres fallback path actually prevents
    double switching. Run this drill once before cutover, then quarterly.

---

## Consequences

### Positive

- Auto-switch becomes safe under Redis loss, closing the single largest
  silent-correctness gap in the Phase 1 deployment.
- The state machine is mechanically enforced instead of being "a convention
  everyone agrees to follow".
- `prefix_rules` drift is impossible without an audited, canary-gated
  release — matching the safety level of every other production change.
- CDN provider contracts have a clear idempotency bar; implementers know
  exactly what to test.
- Queue priority is canonical in `ARCHITECTURE.md`, so `cmd/worker/main.go`
  has a single source of truth.
- Pool operations after a block are well-defined — no more stale `promoted`
  rows and no more "what do I do with this?" tickets.

### Negative / trade-offs

- D1 adds ~5ms to the switcher hot path (Postgres `SELECT FOR UPDATE` round
  trip). Acceptable: a switch already takes ≥ 60s in normal operation.
- D2 forces every existing (imagined) status-mutating call site to route
  through a single method — this is pre-launch, so the cost is documentation
  only, but we are committing to this discipline for the life of the
  project.
- D3 makes `prefix_rules` edits more ceremonious: "just update this field"
  becomes "create a rebuild release". For an internal operator console
  managing 12K production domains, this ceremony is correct.
- D3 requires a new `releases.kind` column. Because the project has **not**
  shipped production data yet, we modify the initial migration in place
  instead of writing an `ALTER TABLE` migration. **This continues the
  one-time pre-launch exception** from ADR-0001. It does NOT re-open that
  exception window after Phase 1 cutover.
- D4 adds a unit test requirement to every CDN provider; slight increase in
  implementation effort per provider, offset by catastrophic-bug avoidance.
- D6 adds two new pool service methods (`OnMainDomainBlocked`, `Unblock`,
  `Retire`) that did not exist in ADR-0001's scope.

---

## Follow-up work (not part of this ADR)

- Implement `internal/domain.Service.Transition()` and update
  `store/postgres/domain.go` to expose *only* `updateStatusTx` for use by
  `Transition()`.
- Implement `internal/switcher/service.go::acquireSwitchLock()` with both
  Redis fast path and Postgres fallback.
- Implement `internal/pool/service.go::OnMainDomainBlocked()` and wire it
  into the switcher's success path.
- Write the D4 idempotency unit test template so every future CDN provider
  PR starts with a failing test.
- Add the D5 queue config to `cmd/worker/main.go` skeleton at Week 1.
- Execute E1 (60s load test) no later than the end of Week 5.

---

## References

- `CLAUDE.md` (2026-04-08 revision) — Critical Business Rules #8 and #9
- `docs/ARCHITECTURE.md` (2026-04-08 second revision) — §2.1, §2.3, §2.5, §2.6, §2.7, §3
- `docs/DATABASE_SCHEMA.md` (2026-04-08 second revision) — `releases.kind`
- `docs/DEVELOPMENT_PLAYBOOK.md` (2026-04-08 second revision) — §6, §7
- `docs/CLAUDE_CODE_INSTRUCTIONS.md` (2026-04-08 revision)
- `docs/adr/0001-architecture-revision-2026-04.md` — parent ADR
