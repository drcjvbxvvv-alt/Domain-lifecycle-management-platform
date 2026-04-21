# OctoDNS Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/octodns/octodns (main branch)
> **Files analyzed**: `source/base.py`, `provider/base.py`, `provider/plan.py`,
> `zone.py`, `record/base.py`, `manager.py`
> **Purpose**: Extract DNS sync engine design (plan/apply, safety thresholds)
> for our platform's future DNS record management phase.

---

## 1. Architecture: Source → Target Unidirectional Sync

```
Sources (desired state)     Targets (actual state)
   YAML files                  Cloudflare API
   Another DNS provider        Route53 API
   Database                    PowerDNS API
         │                           │
         ▼                           ▼
     Manager.sync()
         │
         ├── populate desired from sources
         ├── populate existing from each target
         ├── zone.changes(desired) → [Create, Update, Delete]
         ├── Plan(changes) → safety check
         └── target.apply(plan) → execute API calls
```

---

## 2. Provider Interface (Exact from Source)

### BaseSource — Minimal Interface

```python
class BaseSource:
    SUPPORTS = set()  # e.g., {'A', 'AAAA', 'CNAME', 'MX', 'TXT'}

    def populate(self, zone, target=False, lenient=False):
        """Load records into zone object.
        When target=True, return bool indicating zone exists at provider."""
        raise NotImplementedError
```

### BaseProvider — Extends Source with Plan/Apply

```python
class BaseProvider(BaseSource):
    def __init__(self, id,
                 apply_disabled=False,
                 update_pcent_threshold=0.3,  # 30%
                 delete_pcent_threshold=0.3,  # 30%
                 strict_supports=True):
        ...

    def plan(self, desired, processors=[], lenient=False):
        """10-step workflow: load existing → diff → return Plan"""
        ...

    def apply(self, plan):
        """Execute plan. Calls self._apply(plan) which subclass implements."""
        if self.apply_disabled:
            return 0
        self._apply(plan)
        return len(plan.changes)

    # --- Subclass must implement ---
    def _apply(self, plan):
        raise NotImplementedError

    # --- Subclass can override ---
    def _include_change(self, change):
        """Return False to suppress a change (filter false positives)."""
        return True

    def _extra_changes(self, existing, desired, changes):
        """Return additional provider-specific changes."""
        return []
```

### Key Design: `plan()` 10-Step Workflow

1. Create empty Zone for existing state
2. `self.populate(existing, target=True)` — load current from provider
3. Copy desired zone (safe mutation)
4. `_process_desired_zone(desired)` — remove unsupported record types
5. `_process_existing_zone(existing)` — normalize existing
6. Run processors on existing zone
7. Run processors on both zones
8. `existing.changes(desired, self)` — compute diff
9. Filter via `_include_change()`, add via `_extra_changes()`
10. Return `Plan(existing, desired, changes, exists, thresholds)`

---

## 3. Plan & Safety Thresholds (Exact from Source)

```python
class Plan:
    MAX_SAFE_UPDATE_PCENT = 0.3   # 30% of existing records
    MAX_SAFE_DELETE_PCENT = 0.3   # 30% of existing records
    MIN_EXISTING_RECORDS = 10     # safety only kicks in above this count
```

### `raise_if_unsafe()` Logic

```python
def raise_if_unsafe(self):
    # Only check if zone has >= 10 existing records
    if len(existing.records) < MIN_EXISTING_RECORDS:
        return  # small zone, no safety needed

    existing_count = len(existing.records)

    updates = count(c for c in changes if isinstance(c, Update))
    if updates / existing_count > update_pcent_threshold:
        raise TooMuchChange(...)  # requires --force

    deletes = count(c for c in changes if isinstance(c, Delete))
    if deletes / existing_count > delete_pcent_threshold:
        raise TooMuchChange(...)  # requires --force

    if any root NS changes on existing zone:
        raise RootNsChange(...)   # always requires --force
```

**Exceptions**:
- `TooMuchChange` — percentage threshold exceeded
- `RootNsChange` — NS records at zone apex being modified

---

## 4. Zone Model (Exact from Source)

```python
class Zone:
    def __init__(self, name, sub_zones,
                 update_pcent_threshold=None,
                 delete_pcent_threshold=None):
        self.name = name  # must end with '.' (e.g., "example.com.")
        self.records = {}  # dict grouped by node name
        self.sub_zones = sub_zones  # delegated sub-zones to exclude

    def add_record(self, record, replace=False, lenient=False):
        # Validates: CNAME cannot coexist with other types at same node
        # Rejects records belonging to sub-zones (unless NS/DS at boundary)
        ...

    def changes(self, desired, target):
        """Compute Create/Update/Delete by comparing self (existing) to desired."""
        # Records matched by (name, type) tuple
        # Same (name,type) in both → Update if values differ
        # In desired but not existing → Create
        # In existing but not desired → Delete (unless zone.KeepUnknown)
        ...
```

### Zone Validation Rules

1. CNAME at a node = no other records at that node
2. Records in configured sub-zones are rejected (delegated)
3. NS/DS at sub-zone boundary are allowed (delegation records)
4. Zone name must end with `.`

---

## 5. Record Model (Exact from Source)

```python
class Record:
    def __init__(self, zone, name, data, source=None):
        self.zone = zone
        self.name = name        # short label
        self.ttl = data['ttl']
        self.octodns = data.get('octodns', {})  # metadata

    def __hash__(self):
        return hash(f'{self.name}:{self._type}')  # identity = name + type

    # Subclasses: ARecord, CnameRecord, MxRecord, TxtRecord, etc.
    # Each has type-specific value handling
```

### Record Metadata (`octodns` field)

```yaml
octodns:
  ignored: true              # skip entirely
  excluded: [cloudflare]     # skip for specific targets
  included: [route53]        # only apply to specific targets
  lenient: true              # skip validation
  healthcheck:               # provider-specific health config
    host: example.com
    path: /health
```

This per-record metadata controls sync behavior at record level.

---

## 6. Manager — Sync Orchestration (Exact from Source)

```python
class Manager:
    def sync(self, eligible_zones=[], eligible_sources=[], eligible_targets=[],
             dry_run=True, force=False, checksum=None):
```

### Full Sync Workflow

1. Load zone configs from YAML
2. Resolve sources, targets, processors per zone
3. **Parallel**: submit `_populate_and_plan()` per zone to ThreadPoolExecutor
4. Each zone: populate desired from sources → run processors → for each
   target call `target.plan(desired)`
5. Collect all plans
6. Sort plans: children zones first (longer names → shorter names)
7. Run plan outputs (logging, JSON, markdown)
8. **Checksum**: if enabled, compute SHA-256 of all plan data
9. **Safety**: `plan.raise_if_unsafe()` for each plan (unless `force=True`)
10. **If dry_run**: stop here, return 0
11. **If checksum provided**: verify matches (prevents stale plan execution)
12. **Apply**: for each plan, call `target.apply(plan)`
13. Return total change count

### Key Safety Mechanisms

| Mechanism | Purpose | Override |
|-----------|---------|---------|
| `dry_run=True` (default) | No changes unless explicitly opted in | `dry_run=False` |
| Percentage thresholds (30%) | Prevent accidental mass changes | `--force` flag |
| `MIN_EXISTING_RECORDS=10` | Don't apply thresholds to tiny zones | — |
| Root NS protection | Prevent accidental NS delegation changes | `--force` flag |
| Checksum verification | Ensure plan hasn't drifted since review | provide matching hash |
| `always-dry-run` per zone | Some zones can never be auto-applied | manual override in config |
| Children-first ordering | Sub-zones processed before parents | — |
| `apply_disabled` per provider | Provider in read-only mode | config change |

---

## 7. Applicability to Our Platform

### What We Should Adopt (Phase B: DNS Record Management)

| OctoDNS Pattern | Our Adaptation |
|---|---|
| `populate()` → single method to load state | `Provider.GetZoneRecords()` (already in our DNSControl-based interface) |
| `plan()` → diff + safety check | `Provider.PlanChanges()` with built-in thresholds |
| `apply()` → execute changes | `Provider.ApplyCorrections()` |
| Safety thresholds (30% update/delete) | Configurable per `dns_providers` row |
| `MIN_EXISTING_RECORDS=10` | Same — don't gate small zones |
| Checksum for stale-plan detection | Hash plan → store → verify at apply time |
| `dry_run=True` default | All DNS changes go through preview first |
| Per-record metadata (ignored/excluded) | `dns_records.managed = false` for unmanaged records |
| Zone-level `KeepUnknown` | Domain setting: `purge_unmanaged_records BOOLEAN` |

### Proposed Safety Config for Our Platform

```sql
-- On dns_providers table
ALTER TABLE dns_providers ADD COLUMN sync_config JSONB DEFAULT '{
    "update_threshold_pct": 30,
    "delete_threshold_pct": 30,
    "min_existing_records": 10,
    "always_dry_run": false,
    "apply_disabled": false
}';
```

### Proposed Plan/Apply API Flow

```
POST /api/v1/domains/:id/dns/plan
  → Calls provider.GetZoneRecords() (existing)
  → Compares with desired state in DB
  → Returns Plan (creates, updates, deletes) + safety check result
  → Stores plan hash in Redis (TTL 1 hour)

POST /api/v1/domains/:id/dns/apply
  { "plan_hash": "sha256:..." }
  → Verifies hash matches stored plan (not stale)
  → Executes corrections via provider
  → Records changes in audit log
```

### What We Should NOT Adopt

| OctoDNS Pattern | Why Not |
|---|---|
| YAML-as-source-of-truth | We use DB + API, not config files |
| ThreadPoolExecutor for parallel zones | We use asynq for async work |
| Processors (middleware chain) | Over-engineering for our scope |
| Sub-zone delegation tracking | Not our concern at this phase |
| `always-dry-run` per zone | Our RBAC handles access control |

---

## 8. Summary

OctoDNS provides the definitive reference for **safe DNS sync operations**.
The core pattern is simple:

```
desired state (source) + existing state (target) → diff → safety check → apply
```

The safety mechanisms (percentage thresholds, checksum verification, dry-run
default) are battle-tested by GitHub at scale. We should adopt them wholesale
for our Phase B DNS record management feature.
