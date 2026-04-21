# Domain Asset Layer — Architecture Restructuring Design

> **Created**: 2026-04-21
> **Status**: DRAFT — Pending review
> **Scope**: Domain asset management as the foundational data layer
> **Reference projects**: DomainMOD, Google Nomulus, DNSControl, OctoDNS,
> PowerDNS-Admin, OONI Probe, Uptime Kuma

---

## 1. Problem Statement

Current `domains` table is too thin — only stores FQDN + lifecycle_state +
project_id. As the platform's foundational entity (no domain = no nginx conf,
no HTML template, no release), it must evolve into a full asset management
layer that tracks:

- **Ownership**: which registrar, which account, who owns it
- **Financials**: cost, renewal dates, purchase history
- **Infrastructure**: DNS provider, nameservers, SSL certs
- **Metadata**: tags, categories, contacts, notes

---

## 2. Open-Source Reference Analysis

### 2.1 DomainMOD (PHP) — Asset Ledger

**What it does**: Tracks domain portfolio as assets (ownership, cost, expiry).

**Data model**:
```
Registrar (1) ──── (N) RegistrarAccount (1) ──── (N) Domain
Domain ──→ DNS Profile, IP, Hosting, Category, Owner, SSL Cert
Fee = per (Registrar x TLD) combination
```

**Domain fields**: FQDN, TLD, registrar_account_id, expiry_date,
creation_date, total_cost, fee_id, status, privacy, autorenewal, notes.

**Key patterns to borrow**:
- Two-level registrar abstraction (Registrar → Account) — supports multiple
  accounts at the same registrar
- Per-TLD fee tracking at registrar level
- Expiry-based dashboard (30/60/90 day bands)
- Segments (saved filters) for bulk operations
- Bulk import via registrar API integration

### 2.2 Google Nomulus (Java) — Domain Registry

**What it does**: Full EPP-compliant domain registry (Google is the registrar).

**Domain fields**: FQDN, repoId, registrant, admin/tech/billing contacts,
nameservers[], dsData (DNSSEC), gracePeriods[], registrationExpiration,
currentSponsorRegistrar, statusValues (EPP statuses), transferData.

**Key patterns to borrow**:
- Grace period concept (add grace, renew grace, redemption) — useful for
  tracking "domain just expired, still recoverable"
- Transfer state modeling (pendingTransfer + gaining/losing registrar)
- Contact separation (registrant vs admin vs tech)
- DNSSEC data as first-class field

**What NOT to borrow**:
- Full EPP protocol implementation (we are not a registrar)
- Billing event granularity (overkill for asset tracking)
- Complex grace period state machine (simplify to expiry + grace_end_date)

### 2.3 OctoDNS (Python) — Multi-Provider DNS Sync

**What it does**: GitOps-style DNS management — reads desired state from YAML,
syncs to multiple DNS providers unidirectionally.

**Architecture**:
```
YAML source files (desired state)
    → OctoDNS reads source
    → Fetches current state from each target provider
    → Computes diff (plan)
    → Applies changes (create/update/delete API calls)
```

**Key characteristics**:
- **Unidirectional**: source → target. Source is always authoritative; targets
  get overwritten. No bidirectional merge or conflict resolution.
- **Multi-target**: same zone can be pushed to N providers simultaneously
  (Cloudflare + Route53 + etc.)
- **Plan vs Apply**: `--dryrun` computes diff without executing (like
  Terraform plan). Apply pushes changes.
- **Safety mechanisms**:
  - Max change threshold — refuses if deleting > N% of records
  - `min_existing` — abort if target has fewer records than expected (prevents
    accidental zone wipe)
  - Record count sanity checks per zone

**Key patterns to borrow**:
- Source/target separation — platform is source of truth, providers are targets
- Plan/apply workflow for DNS changes (preview before commit)
- Percentage-based safety thresholds on bulk changes
- Zone-as-unit-of-sync granularity

**Difference from DNSControl**:
| Aspect | OctoDNS | DNSControl |
|--------|---------|------------|
| Config | YAML (pure data) | JavaScript DSL (programmable) |
| Sync model | Source → Target (explicit) | Single desired state → diff |
| Multi-provider | First-class source/target roles | Multiple providers per domain |
| Safety | Percentage thresholds | Preview + confirm |

### 2.4 PowerDNS-Admin (Python/Flask) — DNS Web Management UI

**What it does**: Web UI for managing DNS zones and records with RBAC and
audit logging.

**Data model**:
```
User → Account (group) → Zone (domain) → Records
Roles: Administrator, User (per-zone ACL)
```

**Key features**:
- **Zone-level RBAC**: Admins assign zones to accounts/users. Users see only
  their permitted zones.
- **Staged edits + batch apply**: Edits are staged in browser, submitted as
  one batch ("Apply Changes" button) — single API call to backend.
- **Audit/History log**: Stores action description, JSON diff of changes,
  user, timestamp, domain_id. Filterable history page with before/after diffs.
- **Zone templates**: Named templates with pre-defined records (standard MX +
  SPF + DMARC). Applied on zone creation.
- **Record validation**: Type-specific (IP format for A, priority for MX).
  Client-side + server-side.
- **Dashboard**: Zone count, recent activity, DNS query stats, DNSSEC status.

**Key patterns to borrow**:
- Staged-edit + batch-apply pattern (don't fire API calls per keystroke)
- Per-zone RBAC via account/group mapping
- Audit log with JSON diff (before/after state)
- Zone template for new domain bootstrapping
- Inline record editing with type-specific validation

### 2.5 OONI Probe (Go) — Censorship/GFW Detection

**What it does**: Network censorship measurement tool. Detects DNS poisoning,
HTTP blocking, TLS/SNI blocking by comparing probe results against a control
vantage point.

**Detection methodology**:

| Layer | Method | GFW Pattern |
|-------|--------|-------------|
| **DNS** | Query local resolver vs trusted resolver; compare IPs | GFW injects fake IPs (arrives faster than real response) |
| **HTTP** | Compare response body/status from probe vs control | Connection reset after Host header, content injection |
| **TCP** | Test if SYN-ACK completes | RST injection after SYN |
| **TLS/SNI** | TLS handshake with target SNI vs control SNI to same IP | Reset during ClientHello when SNI matches blocklist |

**Measurement data model** (per check):
```
probe_asn, probe_cc, resolver_ip
test_name, test_version, test_start_time
input (target URL/domain)
test_keys: {
    dns_answers, tcp_connect_results,
    tls_handshake_outcome, http_response (body/headers/status)
}
annotations: { blocking_type: "dns" | "tcp_ip" | "http-diff" | "http-failure" }
result: "ok" | "anomaly" | "confirmed"
```

**Blocked vs Down determination**:
- Control measurement from uncensored vantage point
- If control ALSO fails → site is down, not blocked
- If control succeeds but probe fails → likely blocked
- Repeated observations required to confirm (avoid transient false positives)

**Key patterns to borrow** (for future GFW vertical):
- Multi-layer detection (DNS + TCP + TLS + HTTP) — not just one signal
- Control vs measurement comparison to distinguish "blocked" from "down"
- Measurement data model with per-layer results
- Confidence scoring (ok / anomaly / confirmed)
- Known-poisoned-IP database for quick classification

### 2.6 Uptime Kuma (Node.js) — Monitoring + Status Page

**What it does**: Lightweight uptime monitoring with 90+ notification channels,
public status pages, and maintenance windows.

**Monitor types**: HTTP(s) (status + keyword + JSON match), TCP, Ping, DNS
(resolve + check record), Docker container, gRPC, Push (passive heartbeat),
databases (MySQL/PG/Redis/Mongo).

**Monitor data model**:
```
url/hostname, port, type, interval (default 60s),
retryInterval, maxretries (before marking down),
timeout, accepted_statecodes (["200-299"]),
keyword (must appear in body), invertKeyword,
method (GET/POST), headers, body,
dns_resolve_type, dns_resolve_server,
proxyId, tlsInfo (cert expiry check),
tags (key-value labels for grouping)
```

**Notification/alerting**:
- 90+ channels (Telegram, Slack, Discord, PagerDuty, Webhook, Email...)
- Notifications are reusable objects attached to monitors (many-to-many)
- Fires on **state change** (UP→DOWN, DOWN→UP) — inherent dedup
- Optional "resend every X cycles" for ongoing downtime
- No time-based dedup needed — state-change model handles it

**Status page features**:
- Public URL with custom slug/domain
- Monitors grouped into named sections
- Current status (operational/degraded/down) + uptime bars (heartbeat history)
- Manual incident posting (title + Markdown body + severity)
- Password protection optional
- Multiple independent status pages

**Maintenance windows**:
- Scheduled (recurring or one-time) with start/end + timezone
- Affected monitors listed
- During maintenance: suppress alerts, status page shows "Under Maintenance"

**Key patterns to borrow**:
- State-change alerting (fire on transition, not on every check) — matches
  our existing Critical Rule #8 dedup model
- Monitor-to-notification many-to-many (reusable notification configs)
- Public status page with sections + incident management
- Maintenance window suppression
- Keyword/content matching for L2-style "did the deploy land" verification

---

### 2.7 DNSControl (Go) — Declarative DNS Configuration

**What it does**: DNS configuration as code, desired-state diff/sync model.

**Architecture**:
```
Domain → 1 Registrar (manages NS delegation at parent zone)
       → N DNS Providers (manage zone content)
       → Records[] (desired state)
```

**Provider interfaces**:
```go
type DNSProvider interface {
    GetNameservers(domain string) ([]*Nameserver, error)
    GetZoneRecords(dc *DomainConfig) (Records, error)
    GetZoneRecordsCorrections(dc *DomainConfig, existing Records) ([]*Correction, error)
}

type Registrar interface {
    GetRegistrarCorrections(dc *DomainConfig) ([]*Correction, error)
}
```

**Key patterns to borrow**:
- Registrar and DNS provider are SEPARATE roles for the same domain
- Desired-state model: declare what records should exist → diff → apply
- One domain can have multiple DNS providers
- Correction as unit of change (description + executable function)
- Domain-level metadata (DNSSEC, purge policy, ignore patterns)

### 2.8 Summary: What to Borrow from Each Project

| Project | Layer | What to Borrow | When to Apply |
|---------|-------|----------------|---------------|
| **DomainMOD** | Asset Layer | Registrar→Account model, expiry tracking, cost ledger, bulk ops | **Phase A (this restructuring)** |
| **Nomulus** | Asset Layer | Grace period concept, contact model, transfer state | **Phase A** (simplified) |
| **DNSControl** | DNS Mgmt | Registrar/Provider separation, desired-state diff, provider plugin interface | **Phase B** (DNS record mgmt) |
| **OctoDNS** | DNS Sync | Plan/apply workflow, safety thresholds, multi-target sync | **Phase B** (DNS record mgmt) |
| **PowerDNS-Admin** | UI/UX | Staged-edit + batch-apply, zone RBAC, audit log with JSON diff, zone templates | **Phase A** (UI) + **Phase B** (DNS UI) |
| **OONI Probe** | Monitoring | Multi-layer detection, control/measurement comparison, confidence scoring | **Phase D** (GFW vertical, parked) |
| **Uptime Kuma** | Monitoring | State-change alerting, status page, maintenance windows, keyword verification | **Phase C** (Probe + Alert enhancement) |

---

## 3. Proposed Entity Model

### 3.1 Entity Relationship Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                     Domain Asset Layer                            │
│                                                                  │
│  ┌────────────┐     ┌─────────────────────┐                     │
│  │ registrars │(1)──(N)│ registrar_accounts │                    │
│  └────────────┘     └──────────┬──────────┘                     │
│                                │(1)                              │
│                                │                                 │
│  ┌────────────────┐     ┌──────┴───────┐     ┌───────────────┐  │
│  │ dns_providers  │(1)──(N)│  domains   │(N)──(1)│  projects   │  │
│  └────────────────┘     └──────┬───────┘     └───────────────┘  │
│                                │                                 │
│            ┌───────────────────┼───────────────────┐             │
│            │                   │                   │             │
│            ▼                   ▼                   ▼             │
│  ┌─────────────────┐  ┌──────────────┐  ┌─────────────────────┐ │
│  │ ssl_certificates│  │ domain_costs │  │ domain_tags (M:N)   │ │
│  └─────────────────┘  └──────────────┘  └─────────────────────┘ │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### 3.2 Table Definitions

#### `registrars` — Registrar vendors

```sql
CREATE TABLE registrars (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    name          VARCHAR(128) NOT NULL,          -- "Namecheap", "GoDaddy", "Gandi"
    url           VARCHAR(512),                   -- registrar website
    api_type      VARCHAR(64),                    -- "namecheap", "godaddy", "manual"
    capabilities  JSONB DEFAULT '{}',             -- {"lists_domains":true,"returns_expiry":true,...}
    notes         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ,
    UNIQUE(name)
);
```

#### `registrar_accounts` — Accounts at registrars

```sql
CREATE TABLE registrar_accounts (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    registrar_id    BIGINT NOT NULL REFERENCES registrars(id),
    account_name    VARCHAR(256) NOT NULL,      -- display name / username
    owner_user_id   BIGINT REFERENCES users(id),
    credentials     JSONB,                      -- encrypted API key/secret (or vault ref)
    is_default      BOOLEAN NOT NULL DEFAULT false,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    UNIQUE(registrar_id, account_name)
);
```

#### `dns_providers` — DNS hosting providers

```sql
CREATE TABLE dns_providers (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    name        VARCHAR(128) NOT NULL,          -- "Cloudflare", "Route53", "PowerDNS"
    provider_type VARCHAR(64) NOT NULL,         -- maps to pkg/provider/dns registry
    config      JSONB,                          -- provider-specific config (zone mappings, etc.)
    credentials JSONB,                          -- encrypted credentials (or vault ref)
    notes       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    UNIQUE(name)
);
```

#### `domains` — Extended (in-place upgrade from current thin table)

```sql
CREATE TABLE domains (
    -- Existing fields (preserved)
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id      BIGINT NOT NULL REFERENCES projects(id),
    fqdn            VARCHAR(512) NOT NULL,
    lifecycle_state VARCHAR(32) NOT NULL DEFAULT 'requested',
    owner_user_id   BIGINT REFERENCES users(id),

    -- NEW: Asset metadata
    tld                 VARCHAR(64),                -- extracted TLD (.com, .io, .cn)
    registrar_account_id BIGINT REFERENCES registrar_accounts(id),
    dns_provider_id     BIGINT REFERENCES dns_providers(id),

    -- NEW: Registration & Expiry
    registration_date   DATE,
    expiry_date         DATE,
    auto_renew          BOOLEAN NOT NULL DEFAULT false,
    grace_end_date      DATE,                      -- recoverable until this date after expiry
    expiry_status       VARCHAR(32),               -- computed: null, expiring_30d, expiring_7d, expired, grace, redemption

    -- NEW: Status flags (orthogonal to lifecycle_state, from Nomulus)
    transfer_lock       BOOLEAN NOT NULL DEFAULT true,   -- registrar lock (prevents transfer)
    hold                BOOLEAN NOT NULL DEFAULT false,  -- domain removed from DNS resolution

    -- NEW: Transfer tracking (from Nomulus, simplified)
    transfer_status             VARCHAR(32),        -- null, 'pending', 'completed', 'failed'
    transfer_gaining_registrar  VARCHAR(128),
    transfer_requested_at       TIMESTAMPTZ,
    transfer_completed_at       TIMESTAMPTZ,
    last_transfer_at            TIMESTAMPTZ,
    last_renewed_at             TIMESTAMPTZ,

    -- NEW: DNS infrastructure
    nameservers         JSONB DEFAULT '[]',        -- ["ns1.cloudflare.com", "ns2.cloudflare.com"]
    dnssec_enabled      BOOLEAN NOT NULL DEFAULT false,

    -- NEW: WHOIS & Contacts
    whois_privacy       BOOLEAN NOT NULL DEFAULT false,
    registrant_contact  JSONB,                     -- {name, org, email, phone}
    admin_contact       JSONB,
    tech_contact        JSONB,

    -- NEW: Financial
    annual_cost         DECIMAL(10,2),
    currency            VARCHAR(3) DEFAULT 'USD',  -- ISO 4217
    purchase_price      DECIMAL(10,2),
    fee_fixed           BOOLEAN NOT NULL DEFAULT false,  -- manual cost override (premium domains)

    -- NEW: Purpose & Metadata
    purpose             VARCHAR(255),              -- domain's business purpose
    notes               TEXT,
    metadata            JSONB DEFAULT '{}',        -- custom fields (extensible)

    -- Existing fields (preserved)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    UNIQUE(fqdn)
);

-- Indexes
CREATE INDEX idx_domains_project_id ON domains(project_id);
CREATE INDEX idx_domains_lifecycle_state ON domains(lifecycle_state);
CREATE INDEX idx_domains_registrar_account ON domains(registrar_account_id);
CREATE INDEX idx_domains_dns_provider ON domains(dns_provider_id);
CREATE INDEX idx_domains_expiry_date ON domains(expiry_date);
CREATE INDEX idx_domains_tld ON domains(tld);
```

#### `ssl_certificates` — Certificate tracking

```sql
CREATE TABLE ssl_certificates (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    domain_id       BIGINT NOT NULL REFERENCES domains(id),
    issuer          VARCHAR(256),               -- "Let's Encrypt", "DigiCert"
    cert_type       VARCHAR(32),                -- "dv", "ov", "ev", "self-signed"
    issued_at       TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    auto_renew      BOOLEAN NOT NULL DEFAULT false,
    last_check_at   TIMESTAMPTZ,
    status          VARCHAR(32) NOT NULL DEFAULT 'active',  -- active, expiring, expired, revoked
    serial_number   VARCHAR(128),
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_ssl_certs_domain ON ssl_certificates(domain_id);
CREATE INDEX idx_ssl_certs_expires ON ssl_certificates(expires_at);
```

#### `domain_costs` — Cost history (per renewal event)

```sql
CREATE TABLE domain_costs (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT NOT NULL REFERENCES domains(id),
    cost_type       VARCHAR(32) NOT NULL,       -- "registration", "renewal", "transfer", "restore"
    amount          DECIMAL(10,2) NOT NULL,
    currency        VARCHAR(3) NOT NULL DEFAULT 'USD',
    period_start    DATE,
    period_end      DATE,
    paid_at         DATE,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_domain_costs_domain ON domain_costs(domain_id);
```

#### `domain_fee_schedules` — Cost schedule per (Registrar x TLD)

> Inspired by DomainMOD `fees` table. Auto-calculates `domains.annual_cost`
> unless `fee_fixed = true`.

```sql
CREATE TABLE domain_fee_schedules (
    id               BIGSERIAL PRIMARY KEY,
    registrar_id     BIGINT NOT NULL REFERENCES registrars(id),
    tld              VARCHAR(64) NOT NULL,
    registration_fee DECIMAL(10,2) NOT NULL DEFAULT 0,
    renewal_fee      DECIMAL(10,2) NOT NULL DEFAULT 0,
    transfer_fee     DECIMAL(10,2) NOT NULL DEFAULT 0,
    privacy_fee      DECIMAL(10,2) NOT NULL DEFAULT 0,
    currency         VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(registrar_id, tld)
);
```

#### `tags` + `domain_tags` — Tagging system (many-to-many)

```sql
CREATE TABLE tags (
    id      BIGSERIAL PRIMARY KEY,
    name    VARCHAR(64) NOT NULL UNIQUE,
    color   VARCHAR(7)                         -- hex color for UI (#FF5733)
);

CREATE TABLE domain_tags (
    domain_id   BIGINT NOT NULL REFERENCES domains(id),
    tag_id      BIGINT NOT NULL REFERENCES tags(id),
    PRIMARY KEY (domain_id, tag_id)
);
```

#### `domain_import_jobs` — Bulk import tracking

> Inspired by DomainMOD `domain_queue` pipeline, simplified for asynq.

```sql
CREATE TABLE domain_import_jobs (
    id                   BIGSERIAL PRIMARY KEY,
    uuid                 UUID NOT NULL DEFAULT gen_random_uuid(),
    registrar_account_id BIGINT REFERENCES registrar_accounts(id),
    source_type          VARCHAR(32) NOT NULL,    -- "api_sync", "csv_upload", "manual_bulk"
    status               VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending, fetching, processing, completed, failed
    total_count          INT NOT NULL DEFAULT 0,
    imported_count       INT NOT NULL DEFAULT 0,
    skipped_count        INT NOT NULL DEFAULT 0,  -- already exists
    failed_count         INT NOT NULL DEFAULT 0,
    error_details        JSONB,
    created_by           BIGINT REFERENCES users(id),
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 4. Key Design Decisions

### D1: Registrar and DNS Provider are SEPARATE entities

**Decision**: A domain's registrar (where it's registered) and its DNS
provider (who hosts the zone) are modeled independently.

**Why**: This reflects reality — a domain registered at Namecheap commonly
has its DNS hosted at Cloudflare. DNSControl validates this separation.
The `registrar_account_id` and `dns_provider_id` on `domains` are independent
foreign keys.

### D2: Two-level registrar abstraction (Registrar → Account)

**Decision**: Follow DomainMOD's pattern — `registrars` (vendor) →
`registrar_accounts` (specific account at that vendor).

**Why**: Enterprises commonly have multiple accounts at the same registrar
(different teams, billing entities, or geographic reasons). Linking domains to
an account (not just a vendor) enables accurate cost attribution and API
credential isolation.

### D3: No full EPP state machine

**Decision**: Keep the existing 6-state lifecycle state machine. Do NOT
implement EPP-style statuses (clientHold, serverDeleteProhibited, etc.).

**Why**: We are not a domain registrar. The platform manages domains we own
across registrars. Nomulus's EPP states are relevant only if we operate a
TLD registry. Instead, borrow only the concept of `grace_end_date` (domain is
expired but still recoverable).

### D4: Financial tracking at per-event granularity

**Decision**: `domain_costs` stores one row per financial event (registration,
renewal, transfer). `domains.annual_cost` is denormalized for dashboard display.

**Why**: DomainMOD's approach — simple enough for reporting, detailed enough
to track cost history. Nomulus's billing event system is overkill.

### D5: DNS records desired-state (DEFERRED — not this phase)

**Decision**: DNS record management (desired-state diff/sync a la DNSControl)
is NOT part of this domain asset layer phase. It will be a separate phase
built on top of the asset layer.

**Why**: The asset layer is about "what domains do we own and where are they
managed". Record management is about "what DNS records should exist". Mixing
them increases scope and delays the foundation. The `dns_provider_id` FK
provides the hook for future DNS record management.

### D6: SSL certificate tracking is lightweight

**Decision**: Track cert metadata (issuer, expiry, status) only. Do NOT
manage cert issuance/renewal (that's cert-manager / ACME territory).

**Why**: The goal is alerting on upcoming cert expiry (same importance as
domain expiry). Actual issuance is handled externally (Let's Encrypt,
cert-manager, Caddy auto-HTTPS).

### D7: Domain contacts as JSONB (not normalized tables)

**Decision**: Store registrant/admin/tech contacts as JSONB on the domain
row, not in separate normalized tables.

**Why**: Contacts change infrequently and are primarily for display/WHOIS
reference. The platform does not need to query "all domains where admin
email = X" at scale. JSONB keeps the schema simple.

### D8: Tags replace categories

**Decision**: Use a flexible many-to-many tagging system instead of a rigid
single-category assignment.

**Why**: Domains can belong to multiple classifications simultaneously
(e.g., "production" + "asia" + "gambling"). Tags are more flexible than
DomainMOD's single-category approach.

### D9: Fee schedule is per (Registrar x TLD), not per domain

**Decision**: Add `domain_fee_schedules` table with renewal/registration/
transfer/privacy fees per (registrar, TLD) pair. `domains.annual_cost` is
auto-calculated from the schedule unless `fee_fixed = true`.

**Why**: DomainMOD source confirms this pattern. Most domains at the same
registrar with the same TLD have identical pricing. Only premium/special
domains need manual override (`fee_fixed`). This avoids entering the same
fee 1000 times for 1000 `.com` domains at the same registrar.

### D10: Bulk import via queue (not inline)

**Decision**: Domain bulk import (from registrar API or CSV) goes through
an `domain_import_jobs` table + asynq task, not inline API calls.

**Why**: DomainMOD's 4-table queue pipeline (domain_queue → domain_queue_temp
→ domain_queue_history) shows that import is inherently async (API calls are
slow, dedup is complex). Our adaptation uses asynq for the queue and a single
tracking table for status.

### D11: Registrar API capability matrix via JSONB

**Decision**: Store registrar API capabilities as a JSONB `capabilities`
column on `registrars` instead of a separate mapping table.

**Why**: DomainMOD's `api_registrars` table (with columns like
`lists_domains`, `ret_expiry_date`, `ret_dns_servers`) is the right concept
but wrong implementation (rigid columns). JSONB allows:
```json
{"lists_domains": true, "returns_expiry": true, "returns_ns": true, "returns_privacy": true}
```

### D12: Status flags are ORTHOGONAL to lifecycle state (from Nomulus)

**Decision**: `hold`, `transfer_lock`, `transfer_status`, `expiry_status`
are independent fields, NOT states in the lifecycle state machine.

**Why**: Nomulus source confirms that EPP statuses (hold, lock, pending
transfer) coexist independently of the domain's primary lifecycle. A domain
can be simultaneously `lifecycle_state = active` AND `transfer_status =
pending` AND `expiry_status = expiring_30d`. Collapsing these into one state
machine would create an explosion of states (6 × 5 × 6 = 180 combinations).

**Implication**: The lifecycle state machine (`internal/lifecycle`) remains
the single write path for `lifecycle_state` only. Transfer status and expiry
status are managed by separate service methods with their own validation.

### D13: Transfer is tracked, not initiated (from Nomulus)

**Decision**: The platform RECORDS domain transfers initiated externally
(at the registrar). It does NOT send EPP transfer commands.

**Why**: We are not a registrar. Transfers happen at GoDaddy/Namecheap/etc.
The platform tracks the transfer for audit and auto-updates
`registrar_account_id` when completed. Future: registrar API adapters could
auto-detect transfer completion.

### D14: Expiry drives automated worker actions (from Nomulus)

**Decision**: A periodic asynq task (`domain:expiry_check`) computes
`expiry_status` for all domains and triggers notifications at 30d/7d/expired
thresholds.

**Why**: Nomulus uses lazy evaluation (compute on read). We prefer explicit
state via worker — simpler to reason about, and we already have asynq
infrastructure. The worker runs daily, updates `expiry_status`, and enqueues
notification tasks for status changes.

---

## 5. Go Package Structure Changes

```
internal/
├── domain/                     # NEW — replaces thin domain logic in lifecycle
│   ├── service.go              # Domain CRUD + asset management
│   ├── model.go                # Domain, DomainCost, SSLCertificate structs
│   ├── repository.go           # Interface for domain store
│   └── import.go               # Bulk import from registrar APIs
├── registrar/                  # NEW
│   ├── service.go              # Registrar + Account CRUD
│   ├── model.go                # Registrar, RegistrarAccount structs
│   └── repository.go
├── dnsprovider/                # NEW (wraps existing pkg/provider/dns)
│   ├── service.go              # DNS provider account CRUD
│   ├── model.go                # DNSProvider struct
│   └── repository.go
├── lifecycle/                  # PRESERVED — state machine unchanged
│   └── ...                     # Transition() still the single write path
├── ssl/                        # NEW
│   ├── service.go              # SSL cert tracking + expiry check
│   ├── model.go
│   └── checker.go              # Periodic TLS connection check
└── ...
```

### Relationship to existing `internal/domain/`

The current `internal/domain/` package (if it exists) likely has minimal
logic. The new `internal/domain/` becomes the **primary domain service**
that handles all asset CRUD. `internal/lifecycle/` retains exclusive
authority over the `lifecycle_state` field via its state machine.

---

## 6. API Endpoints (New / Changed)

### Registrar management
```
POST   /api/v1/registrars                    # Create registrar
GET    /api/v1/registrars                    # List registrars
GET    /api/v1/registrars/:id                # Get registrar detail
PUT    /api/v1/registrars/:id                # Update registrar
DELETE /api/v1/registrars/:id                # Soft delete

POST   /api/v1/registrars/:id/accounts       # Create account
GET    /api/v1/registrars/:id/accounts       # List accounts
PUT    /api/v1/registrar-accounts/:id        # Update account
DELETE /api/v1/registrar-accounts/:id        # Soft delete
```

### DNS Provider management
```
POST   /api/v1/dns-providers                 # Create DNS provider
GET    /api/v1/dns-providers                 # List providers
GET    /api/v1/dns-providers/:id             # Get provider detail
PUT    /api/v1/dns-providers/:id             # Update provider
DELETE /api/v1/dns-providers/:id             # Soft delete
```

### Domain asset (extends existing domain API)
```
GET    /api/v1/domains                       # List (add filters: expiry, registrar, provider, tag)
GET    /api/v1/domains/:id                   # Get (response now includes full asset data)
PUT    /api/v1/domains/:id                   # Update (asset fields)
POST   /api/v1/domains/import                # Bulk import
GET    /api/v1/domains/expiring              # Domains expiring within N days
GET    /api/v1/domains/stats                 # Aggregate stats (by registrar, TLD, cost)
```

### SSL Certificate tracking
```
POST   /api/v1/domains/:id/ssl-certs         # Add cert record
GET    /api/v1/domains/:id/ssl-certs         # List certs for domain
GET    /api/v1/ssl-certs/expiring            # All certs expiring within N days
```

### Tags
```
POST   /api/v1/tags                          # Create tag
GET    /api/v1/tags                          # List tags
PUT    /api/v1/domains/:id/tags              # Set domain tags (replace)
```

### Cost tracking
```
POST   /api/v1/domains/:id/costs             # Add cost record
GET    /api/v1/domains/:id/costs             # List cost history
GET    /api/v1/costs/summary                 # Aggregate: total by registrar, TLD, period
```

---

## 7. Dashboard / Reporting Features (Frontend)

Inspired by DomainMOD dashboard + Uptime Kuma status page:

### 7.1 Domain Asset Dashboard
- Total domain count (by lifecycle_state)
- Expiring domains: 30 / 60 / 90 day bands (urgent / warning / info)
- Expiring SSL certs: same bands
- Cost summary: total annual, by registrar, by TLD
- Domains by registrar (pie chart)
- Domains by DNS provider (pie chart)
- Recent activity (last 10 domain changes)

### 7.2 Domain List Enhancements
- Column: registrar, DNS provider, expiry_date, annual_cost, tags
- Filter: by registrar, DNS provider, TLD, tag, expiry range
- Sort: by expiry (soonest first), by cost, by name
- Bulk actions: assign registrar, assign DNS provider, add tags

### 7.3 Expiry Calendar View
- Calendar showing domain + SSL cert expirations
- Color-coded by urgency

---

## 8. Migration Strategy

Since we are in the **pre-launch migration exception window** (ADR-0003 D9,
Critical Rule #12), the `000001_init.up.sql` can be edited in place.

### Step 1: Add new tables
- `registrars`, `registrar_accounts`, `dns_providers`
- `ssl_certificates`, `domain_costs`, `tags`, `domain_tags`

### Step 2: Extend `domains` table
- Add all new columns (nullable where data doesn't exist yet)
- Add indexes

### Step 3: Data migration
- Existing domains keep their current data
- New fields default to NULL / false until populated

---

## 9. Phasing Within This Restructuring

| Step | Scope | Deliverable |
|---|---|---|
| **A1** | Schema + models + store layer | Migration, Go structs, repository interfaces + postgres impl |
| **A2** | Registrar + DNS Provider CRUD (API + UI) | Can add/edit registrars and providers |
| **A3** | Domain asset extension (API + UI) | Domains show full asset data, assign registrar/provider |
| **A4** | SSL cert tracking + expiry alerts | Cert CRUD, periodic check, alert on expiry |
| **A5** | Cost tracking + reporting | Cost CRUD, dashboard stats |
| **A6** | Tags + bulk operations | Tag system, bulk assign, saved filters |
| **A7** | Expiry dashboard + notifications | 30/60/90 day alerts, dashboard widgets |

---

## 10. What This Does NOT Change

- `internal/lifecycle/` state machine — unchanged, still the single write
  path for `lifecycle_state`
- `internal/release/` — unchanged, still references `domain.id`
- `internal/template/` — unchanged
- `cmd/agent/` — unchanged (agents don't know about asset metadata)
- Agent protocol — unchanged
- Release pipeline (P2.1–P2.6) — unchanged

The asset layer is purely additive to the existing deployment system.

---

## 11. Open Questions (For Review)

1. **Credential storage**: Should `registrar_accounts.credentials` and
   `dns_providers.credentials` use JSONB with application-level encryption,
   or should we integrate with an external secret manager from day 1?

2. **Registrar API integration**: Should we build registrar API adapters
   (auto-sync domain list + expiry from Namecheap/GoDaddy APIs) in this
   phase, or defer to a later phase?

3. **DNS record management scope**: Confirmed deferred (D5), but should
   the `dns_providers` table schema anticipate the zone_id mapping that
   DNS record sync will need?

4. **Multi-project domains**: Can a single FQDN belong to multiple projects,
   or is the current `UNIQUE(fqdn)` constraint correct?

5. **Cost currency normalization**: Should we store a base currency
   exchange rate for cross-currency cost aggregation, or keep per-record
   currency and let the UI handle conversion?

---

## References

- DomainMOD: https://github.com/domainmod/domainmod — Domain portfolio asset management
- Google Nomulus: https://github.com/google/nomulus — EPP domain registry, lifecycle model
- DNSControl: https://github.com/StackExchange/dnscontrol — Declarative DNS, provider plugins
- OctoDNS: https://github.com/octodns/octodns — Multi-provider DNS sync, plan/apply
- PowerDNS-Admin: https://github.com/PowerDNS-Admin/PowerDNS-Admin — DNS web UI, RBAC, audit
- OONI Probe: https://github.com/ooni/probe-cli — Censorship detection methodology
- Uptime Kuma: https://github.com/louislam/uptime-kuma — Monitoring, status page, alerting
- CLAUDE.md §"Domain Lifecycle State Machine"
- docs/DATABASE_SCHEMA.md
- docs/adr/0003-pivot-to-generic-release-platform-2026-04.md
