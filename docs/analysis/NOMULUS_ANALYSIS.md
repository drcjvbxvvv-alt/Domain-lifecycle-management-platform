# Nomulus (Google) Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/google/nomulus (master branch)
> **Files analyzed**: `Domain.java`, `DomainBase.java`, `GracePeriod.java`,
> `GracePeriodBase.java`, `StatusValue.java`, `DomainTransferData.java`
> **Purpose**: Extract domain lifecycle state machine design and EPP protocol
> patterns for our platform's lifecycle module enhancement.

---

## 1. Architecture Overview

Nomulus is a **TLD registry** — it IS the authoritative source that registrars
interact with via EPP protocol. This is fundamentally different from our
platform (which manages domains WE OWN across registrars).

**Key distinction**:
- Nomulus = registry (like Verisign for .com)
- Our platform = registrant-side asset + deployment management

Despite this, Nomulus provides the definitive reference for:
- Domain lifecycle states (EPP standard)
- Grace period mechanics
- Transfer flow
- Status flags and their semantics

---

## 2. Domain Entity Fields (From Actual Source)

### DomainBase.java — Core Fields

```java
// Identity
String domainName;                        // FQDN
String tld;                               // extracted TLD
String repoId;                            // unique repository ID

// Contacts (FK to Contact entity by repo ID)
String adminContact;
String billingContact;
String techContact;
String registrantContact;

// DNS
Set<VKey<Host>> nsHosts;                  // nameserver references
Set<DomainDsData> dsData;                 // DNSSEC delegation signer data
Set<String> subordinateHosts;             // in-bailiwick hosts (glue records)

// Registration timing
Instant registrationExpirationTime;       // when registration expires
Instant lastTransferTime;                 // last successful transfer
Instant autorenewEndTime;                 // when autorenew stops (if set)

// Auth
DomainAuthInfo authInfo;                  // EPP auth code (transfer secret)

// Grace periods (separate entities, cascade ALL)
Set<GracePeriod> gracePeriods;            // currently active grace periods

// Transfer (embedded)
DomainTransferData transferData;          // pending transfer details

// Billing
VKey<BillingRecurrence> autorenewBillingEvent;  // recurring charge reference
VKey<Autorenew> autorenewPollMessage;
VKey<OneTime> deletePollMessage;

// Trademark
LaunchNotice launchNotice;                // TMCH claims notice
String smdId;                             // signed mark data ID
String idnTableName;                      // IDN table used

// Package/Bulk
VKey<AllocationToken> currentBulkToken;   // bulk pricing token

// Lifecycle phase
LordnPhase lordnPhase;                   // LORDN sunrise phase tracking
```

### Inherited from EppResource

```java
// Common EPP resource fields (from base class)
String currentSponsorRegistrarId;         // registrar who "owns" this domain
Instant creationTime;
String creationRegistrarId;               // who originally created it
Instant lastEppUpdateTime;
String lastEppUpdateRegistrarId;
Instant deletionTime;                     // soft-delete marker
Set<StatusValue> statusValues;            // EPP status flags
```

---

## 3. EPP Status Values (Full Enum)

```java
public enum StatusValue {
    // Client-settable (registrar can toggle)
    CLIENT_DELETE_PROHIBITED,
    CLIENT_HOLD,                // domain NOT in DNS
    CLIENT_RENEW_PROHIBITED,
    CLIENT_TRANSFER_PROHIBITED,
    CLIENT_UPDATE_PROHIBITED,

    // Server-settable (registry/admin can toggle)
    SERVER_DELETE_PROHIBITED,
    SERVER_HOLD,                // domain NOT in DNS (registry-imposed)
    SERVER_RENEW_PROHIBITED,
    SERVER_TRANSFER_PROHIBITED,
    SERVER_UPDATE_PROHIBITED,

    // Automatic/computed
    INACTIVE,                   // no nameservers delegated → not in DNS
    LINKED,                     // virtual — has dependent objects
    OK,                         // no other statuses → everything normal
    PENDING_CREATE,             // (unused in Nomulus)
    PENDING_DELETE,             // deletion in progress (redemption/pending-delete)
    PENDING_TRANSFER,           // transfer request pending approval
    PENDING_UPDATE,             // (unused in Nomulus)
}
```

### Status Semantics

| Status | DNS Effect | Modification Effect |
|--------|-----------|---------------------|
| `*_HOLD` | Domain removed from zone (not resolvable) | — |
| `*_DELETE_PROHIBITED` | — | Cannot be deleted |
| `*_RENEW_PROHIBITED` | — | Cannot be renewed |
| `*_TRANSFER_PROHIBITED` | — | Cannot be transferred |
| `*_UPDATE_PROHIBITED` | — | Cannot modify contacts/NS |
| `INACTIVE` | Not in zone (no NS) | — |
| `OK` | Normal resolution | Normal operations allowed |
| `PENDING_DELETE` | Removed from zone | No modifications allowed |
| `PENDING_TRANSFER` | Normal resolution | Limited modifications |

### Client vs Server

- `CLIENT_*` — set by the sponsoring registrar (e.g., "lock this domain")
- `SERVER_*` — set by the registry/admin (e.g., "freeze for legal dispute")
- Both can coexist; SERVER overrides CLIENT conceptually

---

## 4. Grace Period System (Exact from Source)

### GracePeriodBase.java — Fields

```java
String domainRepoId;           // FK to domain
GracePeriodStatus type;        // ADD, RENEW, AUTO_RENEW, TRANSFER, REDEMPTION, PENDING_DELETE
Instant expirationTime;        // when this grace period ends
String clientId;               // registrar ID (who triggered it)
VKey<BillingEvent> billingEvent;         // associated one-time charge
VKey<BillingRecurrence> billingRecurrence; // associated recurring charge
```

### GracePeriodStatus Enum

| Status | Duration (ICANN standard) | Trigger | Effect |
|--------|--------------------------|---------|--------|
| `ADD` | 5 days | After initial registration | Delete → full refund |
| `RENEW` | 5 days | After explicit renewal | Delete → refund renewal fee |
| `AUTO_RENEW` | 30-45 days | After automatic renewal | Delete → refund auto-renew fee |
| `TRANSFER` | 5 days | After transfer completes | Delete → refund transfer fee |
| `REDEMPTION` | 30 days | After non-grace deletion | Can restore (for a fee) |
| `PENDING_DELETE` | 5 days | After redemption expires | Final countdown, no restore |

### How Grace Periods Work

```
Registration ──[5 days]──→ ADD grace ends ──→ normal state
                ↓ (delete during grace)
              Full refund, domain released immediately

Expiry ──→ AUTO_RENEW grace [30 days] ──→ grace ends ──→ normal
               ↓ (delete during grace)
             Refund auto-renew fee

Delete (no active grace) ──→ REDEMPTION [30 days] ──→ PENDING_DELETE [5 days] ──→ released
                                    ↓ (restore)
                                  Restored (fee charged)
```

### Key Design: Grace Periods are Separate Entities

Grace periods are NOT a state on the domain — they are **separate objects
attached to the domain** (one-to-many, cascade ALL). A domain can have
MULTIPLE active grace periods simultaneously (e.g., just renewed AND just
updated contacts).

---

## 5. Transfer Flow (Exact from Source)

### DomainTransferData Fields (Embedded in Domain)

```java
// Inherited from BaseTransferObject:
TransferStatus transferStatus;       // PENDING, APPROVED, CANCELLED, REJECTED, AUTO_APPROVED
String gainingClientId;              // registrar gaining the domain
String losingClientId;               // registrar losing the domain
Instant transferRequestTime;         // when transfer was requested
Instant pendingTransferExpirationTime; // when auto-approve kicks in

// Domain-specific:
Trid transferRequestTrid;            // EPP transaction IDs
Period transferPeriod;               // renewal extension (usually 1 year)
Instant transferredRegistrationExpirationTime; // new expiry after transfer
VKey<BillingEvent> serverApproveBillingEvent;
VKey<BillingRecurrence> serverApproveAutorenewEvent;
VKey<BillingCancellation> billingCancellationId;
```

### Transfer State Machine

```
Domain (no transfer)
    ↓ gaining registrar sends transfer request
PENDING_TRANSFER (pendingTransferExpirationTime = now + 5 days)
    ├── Losing registrar APPROVES → transfer completes immediately
    ├── Losing registrar REJECTS → transfer cancelled
    ├── Gaining registrar CANCELS → transfer cancelled
    └── Timeout (5 days) → AUTO_APPROVED → transfer completes
```

On transfer completion:
1. `currentSponsorRegistrarId` = gaining registrar
2. `registrationExpirationTime` += transfer period (usually 1 year)
3. New TRANSFER grace period created (5 days)
4. Billing events created for gaining registrar
5. `lastTransferTime` updated

---

## 6. Time Projection Pattern

Nomulus has a critical pattern: `cloneDomainProjectedAtTime(Instant now)`.
This computes the domain state at any point in time by:

1. Checking if grace periods have expired → remove them
2. Checking if autorenew should have fired → add billing events
3. Checking if pending transfer should have auto-approved → complete transfer
4. Recomputing status values based on current state

This is needed because Nomulus doesn't run cron jobs to update domain state —
it lazily computes the "correct" state whenever the domain is accessed.

---

## 7. Applicability to Our Platform

### What We Should Adopt

| Nomulus Pattern | Our Adaptation | Why |
|---|---|---|
| Grace period concept | `grace_end_date DATE` on domain | Track "domain expired but still recoverable" window |
| Transfer status field | `transfer_status VARCHAR(32)` on domain | Track ongoing transfers between registrars |
| Status flags (selective) | `status_flags JSONB` on domain | Subset: `hold`, `locked`, `expired`, `transfer_prohibited` |
| `lastTransferTime` | `last_transfer_at TIMESTAMPTZ` on domain | Audit trail |
| Expiry as the clock | `expiry_date` drives lifecycle transitions | Auto-transition to `expired` state when date passes |

### What We Should NOT Adopt

| Nomulus Pattern | Why Not |
|---|---|
| Full EPP status enum (17 values) | We're not a registry; most are irrelevant |
| `CLIENT_*` vs `SERVER_*` separation | We don't have a client/server trust model |
| Grace periods as separate entities | Overkill — we only need to know WHEN grace ends |
| `BillingEvent` / `BillingRecurrence` / `BillingCancellation` | We track costs simply (fee schedule + cost history) |
| `cloneDomainProjectedAtTime()` lazy evaluation | We use explicit state transitions via lifecycle SM |
| `DomainAuthInfo` (EPP auth code) | We don't do EPP transfers programmatically (yet) |
| `LaunchNotice` / `smdId` / TMCH | Trademark sunrise is registry-only concern |
| `subordinateHosts` (glue records) | We don't manage parent zone delegation |

### Refined Lifecycle State Machine (Post-Nomulus Analysis)

Our existing 6-state machine is correct for our use case. Nomulus confirms
we should ADD these orthogonal concerns as **fields**, not states:

```
Lifecycle States (existing, unchanged):
  requested → approved → provisioned → active → disabled → retired

Orthogonal Fields (NEW, from Nomulus):
  ┌─────────────────────────────────────────────────────┐
  │ transfer_status: null | pending | completed | failed │
  │ expiry_status:   null | expiring | expired | grace  │
  │ hold:            false | true (removed from DNS)    │
  │ locked:          false | true (transfer prohibited) │
  └─────────────────────────────────────────────────────┘
```

**Key insight**: Nomulus uses STATUS FLAGS + GRACE PERIODS as orthogonal
dimensions, not as states in a single state machine. A domain can be
simultaneously:
- lifecycle_state = `active`
- transfer_status = `pending`
- expiry_status = `expiring` (30 days out)
- locked = false

This is the RIGHT model — don't collapse everything into one state machine.

---

## 8. Proposed Schema Changes (From This Analysis)

Add these fields to the `domains` table:

```sql
-- Transfer tracking (from Nomulus transfer model, simplified)
transfer_status         VARCHAR(32),        -- null, 'pending', 'completed', 'failed'
transfer_gaining_registrar VARCHAR(128),    -- who is receiving the domain
transfer_requested_at   TIMESTAMPTZ,
transfer_completed_at   TIMESTAMPTZ,

-- Expiry lifecycle (from Nomulus grace period model, simplified)
-- expiry_date already exists
-- grace_end_date already exists
expiry_status           VARCHAR(32),        -- null, 'expiring_30d', 'expiring_7d', 'expired', 'grace', 'redemption'

-- Status flags (from Nomulus StatusValue, simplified subset)
hold                    BOOLEAN NOT NULL DEFAULT false,  -- domain removed from DNS
locked                  BOOLEAN NOT NULL DEFAULT true,   -- transfer_lock (already proposed)

-- Audit
last_transfer_at        TIMESTAMPTZ,
last_renewed_at         TIMESTAMPTZ,
```

### Computed `expiry_status` Logic

This should be computed by a periodic worker task (not lazy like Nomulus):

```go
func computeExpiryStatus(d *Domain, now time.Time) string {
    if d.ExpiryDate.IsZero() {
        return ""  // no expiry tracked
    }
    daysUntilExpiry := d.ExpiryDate.Sub(now).Hours() / 24
    
    switch {
    case d.GraceEndDate != nil && now.After(*d.GraceEndDate):
        return "redemption"  // past grace, may be unrecoverable
    case now.After(d.ExpiryDate) && d.GraceEndDate != nil:
        return "grace"       // expired but still in grace period
    case now.After(d.ExpiryDate):
        return "expired"     // expired, no grace configured
    case daysUntilExpiry <= 7:
        return "expiring_7d" // urgent
    case daysUntilExpiry <= 30:
        return "expiring_30d" // warning
    default:
        return ""            // normal
    }
}
```

---

## 9. Transfer Flow for Our Platform

Unlike Nomulus (which processes EPP transfer commands), our platform TRACKS
transfers that happen externally (at the registrar). Our flow:

```
Operator initiates transfer at registrar (external action)
    ↓
Operator records transfer in platform:
  POST /api/v1/domains/:id/transfer
  { "gaining_registrar_account_id": 5, "notes": "moving to Cloudflare" }
    ↓
Platform sets:
  transfer_status = 'pending'
  transfer_gaining_registrar = "Cloudflare Registrar"
  transfer_requested_at = NOW()
    ↓
Transfer completes (operator confirms OR auto-detected via registrar API):
  POST /api/v1/domains/:id/transfer/complete
    ↓
Platform sets:
  transfer_status = 'completed'
  transfer_completed_at = NOW()
  registrar_account_id = new account
  last_transfer_at = NOW()
  transfer_status = null (cleared after completion)
```

This is purely tracking/audit — the platform doesn't initiate EPP transfers.

---

## 10. Summary

Nomulus confirms our architecture is on the right track and adds these
refinements:

1. **Status flags are orthogonal to lifecycle state** — don't collapse
   `hold`, `locked`, `transfer_pending` into the lifecycle state machine.
   Keep them as independent boolean/enum fields.

2. **Grace period is just a date** — we don't need a separate entity.
   `grace_end_date` on the domain + a computed `expiry_status` field is
   sufficient.

3. **Transfer is a trackable event** — add `transfer_status` +
   `transfer_*` fields for audit. Our platform tracks (not initiates)
   transfers.

4. **Expiry drives automated actions** — a periodic worker should compute
   `expiry_status` and trigger notifications (30-day warning, 7-day urgent,
   expired alert).

5. **EPP semantics we DON'T need**: auth codes, grace period billing
   refunds, pending-create/pending-update, trademark sunrise, subordinate
   hosts. These are registry concerns.

---

## References

- Source: `github.com/google/nomulus/core/src/main/java/google/registry/model/domain/`
- EPP RFC 5731: https://tools.ietf.org/html/rfc5731
- EPP RFC 3915 (Grace Period Mapping): https://tools.ietf.org/html/rfc3915
