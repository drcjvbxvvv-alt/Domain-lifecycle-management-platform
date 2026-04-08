# ADR-0001: Architecture revision — April 2026

- **Status**: Accepted
- **Date**: 2026-04-08
- **Scope**: `docs/ARCHITECTURE.md`, `docs/DATABASE_SCHEMA.md`, `docs/DEVELOPMENT_PLAYBOOK.md`
- **Supersedes**: original versions of the three documents above (pre-2026-04-08)

---

## Context

The original architecture / schema / playbook documents were authored early in the
project and accumulated several inconsistencies by April 2026:

1. Recent commits (`af8edbc`, `778892f`, `59fa29e`, `1b4a5fb`, `e076960`) moved the
   login flow from email → username; no doc reflected it.
2. The probe-node count was described as "6" in the overview and "3" in the deployment
   topology, with the auto-switch flow requiring "all CN nodes" — impossible under
   Phase 1's 3-node plan.
3. The pool state machine (`pending → standby → active → blocked → retired`) reused
   the token `active`, overlapping with the main-domain state machine in CLAUDE.md and
   making log lines ambiguous.
4. L1 probe cycle = 90s made the "< 2 min detection" SLA mathematically infeasible
   once a "2 consecutive failures" confirmation rule was added to suppress noise.
5. The 30–60s DNS propagation assumption in the auto-switch flow implicitly required
   a 60s TTL, but no rule enforced it.
6. Release shard partitioning, canary sizing, and `nginx -t` failure handling were
   under-specified, leaving too much room for divergent implementations.
7. `probe_results` omitted TLS handshake, SNI, cert expiry, and content hash columns
   that the tiered probing design (§2.4) depended on.
8. Redis key design listed only 3 namespaces; reload-buffer locks, switch locks,
   release-pause flags, and fail-streak counters were used implicitly.
9. Phase 1 deployment had **no** documented backup / DR plan despite managing 12K
   production domains.
10. Frontend deployment didn't specify cache-control rules, risking stale SPA
    manifests after Caddy picks up new `web/dist/` output.

This ADR records the 13 decisions taken on 2026-04-08 to resolve all of the above.

---

## Decisions

### D1 — Login identifier = `username`
User accounts are internal operator accounts; no email is collected. `users.email`
becomes `users.username` with a `[a-zA-Z0-9_.-]{3,64}` CHECK. Any future password-reset
flow uses an out-of-band channel (Telegram bot), never email.

**Why**: Matches recent login refactor (`af8edbc`) and reflects reality — this is not a
customer-facing product, so email collection is both useless and a privacy liability.

### D2 — Phase-aware probe-node count
The overview, auto-switch flow, and pool warmup verification all now read
"≥ majority of active CN probe nodes (Phase 1: ≥ 2 of 3; Phase 2: ≥ 4 of 6)" instead
of "all 6 nodes".

**Why**: Phase 1 ships with 3 nodes (one per ISP). Requiring all nodes would make
auto-switch unable to complete even in healthy conditions, because any one unreachable
probe (e.g. during a carrier maintenance window) would veto the switch.

### D3 — DNS record TTL = 60s (hard rule)
All managed DNS records default to TTL = 60. Providers that cannot honor 60s TTL
must reject registration at startup.

**Why**: The auto-switch "wait 30–60s for propagation" step is only achievable with
a 60s TTL. Without an explicit rule this assumption silently broke as soon as a new
provider defaulted to 300s.

### D4 — Pool state machine renamed
`pending → warming → ready → promoted → blocked → retired` replaces the previous
`pending → standby → active → blocked → retired`.

**Why**: Eliminates the token collision with `main_domains.status = active`. The new
`warming` state is also necessary to represent "warmup in progress" (previously
implicit). `promoted` makes the hand-off moment auditable.

### D5 — L1 probe cycle = 60s with two trigger paths
Cycle reduced 90s → 60s. Alerting now uses:
- **Fast path**: all active nodes report the same non-ok status in a single cycle → immediate P1 (~65s worst case).
- **Confirmation path**: majority of nodes, 2 consecutive cycles → P1 (~125s worst case).

**Why**: Makes the "< 2 min detection" SLA mathematically provable instead of wishful.
The dual path is needed because a single slow probe node would otherwise prevent the
fast path from firing.

### D6 — Capacity fallback documented
Architecture §2.4 now includes a capacity note: 12K × 3 / 60s ≈ 200 checks/s/node; if
the Phase-1 1C/1G probe boxes can't hold CPU < 80% during load testing, fall back to
L1 = 90s and renegotiate SLA to < 3 min **before** Phase 1 cutover.

**Why**: Prevents "hit the SLA rule wall on day 1 of production" surprises. Gives
operations an explicit, pre-approved escape hatch.

### D7 — Release is single-project; canary shard = `min(30, 2%)`, hard min 10
A Release row maps to exactly one project. Shard 0 is the canary shard with size
`min(30, 2% of release)`, clamped to ≥ 10. Normal shards follow `shard_size`
(default 200). Domains are partitioned by `hash(main_domain_id) % shard_count` so
that retries land in the same shard.

**Why**: Keeps canary blast radius small (≤ 30 domains) so a template regression
costs at most one manual rollback. Cross-project releases were never safely
implemented and the DB schema has no FK path to support them — codifying the
restriction prevents accidental "just pass multiple project_ids" features.

### D8 — Explicit `nginx -t` failure handling
- `nginx -t` fails on a server → roll back **only that server's** batch, mark every
  DomainTask in that batch `failed` with reason `nginx_test_failed`.
- Other servers in the same shard continue independently.
- If > 20% of servers in a shard fail `nginx -t`, escalate to P1 and pause the shard.
- `failed` DomainTasks do **not** auto-retry; they require an operator re-queue.

**Why**: A template regression will fail identically on retry; auto-retry just burns
asynq capacity and delays the operator page. Per-server rollback (instead of
shard-wide) keeps healthy servers productive during a bad rollout.

### D9 — `probe_results` schema completed
Added columns: `tier`, `block_reason`, `tls_handshake_ok`, `tls_sni_ok`,
`tls_cert_expiry`, `content_hash`. Added `CHECK (tier IN (1,2,3))` and a tier index.

**Why**: The tiered probing design (L1/L2/L3) was written before the schema and the
TLS / content-tamper checks had no columns to write to. Any implementation of L3
(core-domain monitoring) was blocked on this.

### D10 — Redis key namespaces expanded
Documented: `switch:lock:{main_domain_id}`, `release:pause:{project_id}`,
`probe:fail_streak:{probe_node}:{domain}`, and an `asynq:*` reserved namespace.

**Why**: These keys were being invented ad-hoc in each subsystem; the risk of a
name collision between the release scheduler and the switcher was real and silent.

### D11 — Backup & DR plan for Phase 1
Daily `pg_dump` + WAL archiving off-site (RPO ≤ 5 min, ≤ 24h worst case);
`probe_results` hypertable excluded from nightly dump (bounded by 90-day retention);
Redis AOF `everysec`; config/secrets in a separate private repo. RTO ≤ 2h.

**Why**: 12K production domains with zero documented backup was the single largest
operational risk. Separating configs/secrets from the DB dump means neither alone
leaks operational capability if compromised.

### D12 — SPA cache-busting rule
Vite already emits hashed chunks; we explicitly require Caddy to serve `index.html`
with `Cache-Control: no-cache`.

**Why**: Without this, users who opened the console before a deploy keep loading a
stale chunk manifest after deploy and see mysterious 404s on JS chunks that no
longer exist.

### D13 — Canary `canary_shard_size` column + `warmup_attempts/warmup_last_error`
`releases.canary_shard_size INT NOT NULL DEFAULT 30` and
`main_domain_pool.warmup_attempts INT NOT NULL DEFAULT 0 / warmup_last_error TEXT`
added to initial migration.

**Why**: Canary sizing should be per-release (operators occasionally want a very
small canary for risky changes). Warmup attempt/error tracking gives operators a
chance to see *why* a pool entry is stuck in `pending` without digging through
worker logs.

---

## Consequences

### Positive
- Every SLA claim in `ARCHITECTURE.md` now has a mathematical justification.
- The pool state machine is unambiguous in log lines (no more `active` collisions).
- New-hire on-ramp: the three docs are internally consistent, so a new engineer
  can follow them without cross-checking against code for contradictions.
- Disaster recovery has a documented, testable procedure.

### Negative / trade-offs
- L1 cycle 60s increases probe-node CPU from ~130 to ~200 checks/s/node; verified
  acceptable on paper, but Phase 1 load testing must confirm.
- Renaming `pool.status` values is a breaking change in the initial migration.
  Because the project has **not** shipped production data yet, we modify the
  initial migration in place instead of writing an `ALTER TABLE` migration. **This
  is a one-time exception** to the "never modify applied migrations" rule and will
  not happen again after production launch.
- Dropping `users.email` likewise modifies the initial migration; same exception.

### Follow-up work (not part of this ADR)
- Implement the 60s-TTL check in each DNS provider factory.
- Build the `release:pause:{project_id}` read path in the release scheduler.
- Add a `tier` column path in the scanner binary (`cmd/scanner`) — today's scanner
  always writes tier=1 implicitly.
- Load-test the 60s L1 cycle on a 1C/1G box against 12K synthetic domains before
  Phase 1 cutover; if it fails, trigger D6 fallback.

---

## References

- `docs/ARCHITECTURE.md` (2026-04-08 revision)
- `docs/DATABASE_SCHEMA.md` (2026-04-08 revision)
- `docs/DEVELOPMENT_PLAYBOOK.md` (2026-04-08 revision)
- `CLAUDE.md` — project rules and domain state machine
- Commits: `af8edbc`, `778892f`, `59fa29e`, `1b4a5fb`, `e076960`
