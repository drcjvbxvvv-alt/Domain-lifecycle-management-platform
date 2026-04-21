# DomainMOD Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/domainmod/domainmod (master branch)
> **Files analyzed**: `install/go.php` (full database schema, ~30 tables)
> **Purpose**: Extract domain asset data model for our platform's asset layer
> restructuring.

---

## 1. Architecture Overview

DomainMOD is a **domain portfolio management tool** (asset ledger), NOT a
deployment platform. Its core value is answering:
- What domains do we own?
- Where are they registered?
- When do they expire?
- How much do they cost?
- What DNS/hosting/SSL is attached?

### Entity Hierarchy

```
owners (business entity)
  └── registrar_accounts (login credentials at a registrar)
        └── domains (the asset)
              ├── fees (per registrar × TLD cost schedule)
              ├── dns (nameserver profile)
              ├── ip_addresses
              ├── hosting
              ├── categories
              ├── ssl_certs
              └── custom fields (EAV pattern)
```

---

## 2. Core Tables (Exact Schema from Source)

### `domains` — Central entity

```sql
CREATE TABLE domains (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    owner_id        INT UNSIGNED NOT NULL DEFAULT 1,    -- FK owners
    registrar_id    INT UNSIGNED NOT NULL DEFAULT 1,    -- FK registrars
    account_id      INT UNSIGNED NOT NULL DEFAULT 1,    -- FK registrar_accounts
    domain          VARCHAR(255) NOT NULL,              -- FQDN
    tld             VARCHAR(50) NOT NULL,               -- extracted TLD
    expiry_date     DATE NOT NULL DEFAULT '1970-01-01',
    cat_id          INT UNSIGNED NOT NULL DEFAULT 1,    -- FK categories
    fee_id          INT UNSIGNED NOT NULL DEFAULT 0,    -- FK fees
    total_cost      DECIMAL(10,2) NOT NULL,             -- calculated annual cost
    dns_id          INT UNSIGNED NOT NULL DEFAULT 1,    -- FK dns (nameserver profile)
    ip_id           INT UNSIGNED NOT NULL DEFAULT 1,    -- FK ip_addresses
    hosting_id      INT UNSIGNED NOT NULL DEFAULT 1,    -- FK hosting
    function        VARCHAR(255) NOT NULL,              -- domain purpose description
    notes           LONGTEXT NOT NULL,
    autorenew       TINYINT(1) NOT NULL DEFAULT 0,
    privacy         TINYINT(1) NOT NULL DEFAULT 0,      -- WHOIS privacy
    active          TINYINT(2) NOT NULL DEFAULT 1,      -- soft-delete/inactive flag
    fee_fixed       TINYINT(1) NOT NULL DEFAULT 0,      -- manual fee override
    creation_type_id TINYINT(2) NOT NULL DEFAULT 2,     -- how was it created
    created_by      INT UNSIGNED NOT NULL DEFAULT 0,    -- FK users
    insert_time     DATETIME NOT NULL,
    update_time     DATETIME NOT NULL,
    KEY domain (domain)
);
```

### `registrars` — Registrar vendors

```sql
CREATE TABLE registrars (
    id                INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name              VARCHAR(100) NOT NULL,            -- "Namecheap", "GoDaddy"
    url               VARCHAR(100) NOT NULL,            -- vendor URL
    api_registrar_id  TINYINT(3) NOT NULL DEFAULT 0,   -- FK api_registrars (API capability)
    notes             LONGTEXT NOT NULL,
    creation_type_id  TINYINT(2) NOT NULL DEFAULT 2,
    created_by        INT UNSIGNED NOT NULL DEFAULT 0,
    insert_time       DATETIME NOT NULL,
    update_time       DATETIME NOT NULL,
    KEY name (name)
);
```

### `registrar_accounts` — Credentials per registrar

```sql
CREATE TABLE registrar_accounts (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    owner_id        INT UNSIGNED NOT NULL,             -- FK owners (who owns this account)
    registrar_id    INT UNSIGNED NOT NULL,             -- FK registrars
    email_address   VARCHAR(100) NOT NULL,
    username        VARCHAR(100) NOT NULL,
    password        VARCHAR(255) NOT NULL,             -- stored in plaintext (!)
    account_id      VARCHAR(255) NOT NULL,             -- registrar's account ID
    reseller        TINYINT(1) NOT NULL DEFAULT 0,
    reseller_id     VARCHAR(100) NOT NULL,
    api_app_name    VARCHAR(255) NOT NULL,
    api_key         VARCHAR(255) NOT NULL,
    api_secret      VARCHAR(255) NOT NULL,
    api_ip_id       INT UNSIGNED NOT NULL DEFAULT 0,
    notes           LONGTEXT NOT NULL,
    creation_type_id TINYINT(2) NOT NULL DEFAULT 2,
    created_by      INT UNSIGNED NOT NULL DEFAULT 0,
    insert_time     DATETIME NOT NULL,
    update_time     DATETIME NOT NULL,
    KEY registrar_id (registrar_id)
);
```

### `fees` — Cost schedule (per Registrar × TLD)

```sql
CREATE TABLE fees (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    registrar_id    INT UNSIGNED NOT NULL,             -- FK registrars
    tld             VARCHAR(50) NOT NULL,              -- ".com", ".io", ".cn"
    initial_fee     DECIMAL(10,2) NOT NULL,            -- first-year registration
    renewal_fee     DECIMAL(10,2) NOT NULL,            -- annual renewal
    transfer_fee    DECIMAL(10,2) NOT NULL,            -- incoming transfer
    privacy_fee     DECIMAL(10,2) NOT NULL,            -- WHOIS privacy add-on
    misc_fee        DECIMAL(10,2) NOT NULL,            -- other charges
    currency_id     INT UNSIGNED NOT NULL,             -- FK currencies
    fee_fixed       TINYINT(1) NOT NULL DEFAULT 0,
    insert_time     DATETIME NOT NULL,
    update_time     DATETIME NOT NULL
);
```

### `dns` — Nameserver profile (NOT DNS records!)

```sql
CREATE TABLE dns (
    id          INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,            -- profile name
    dns1-dns10  VARCHAR(255) NOT NULL,            -- up to 10 nameservers
    ip1-ip10    VARCHAR(45) NOT NULL,             -- glue record IPs
    notes       LONGTEXT NOT NULL,
    number_of_servers TINYINT(2) NOT NULL DEFAULT 0,
    ...
);
```

**NOTE**: This is a flat nameserver profile (dns1, dns2, ..., dns10), NOT a
DNS record management system. DomainMOD does NOT manage A/CNAME/MX records.

### `ssl_certs` — SSL certificate tracking

```sql
CREATE TABLE ssl_certs (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    owner_id        INT UNSIGNED NOT NULL,
    ssl_provider_id INT UNSIGNED NOT NULL,         -- FK ssl_providers
    account_id      INT UNSIGNED NOT NULL,         -- FK ssl_accounts
    domain_id       INT UNSIGNED NOT NULL,         -- FK domains
    type_id         INT UNSIGNED NOT NULL,         -- FK ssl_cert_types
    ip_id           INT UNSIGNED NOT NULL,
    cat_id          INT UNSIGNED NOT NULL,
    name            VARCHAR(100) NOT NULL,          -- cert display name
    expiry_date     DATE NOT NULL,
    fee_id          INT UNSIGNED NOT NULL,          -- FK ssl_fees
    total_cost      DECIMAL(10,2) NOT NULL,
    notes           LONGTEXT NOT NULL,
    active          TINYINT(2) NOT NULL DEFAULT 1,
    ...
);
```

### `ssl_providers` + `ssl_accounts` — Mirror pattern of registrars

```sql
-- ssl_providers: "Let's Encrypt", "DigiCert", "Comodo"
-- ssl_accounts: credentials at each SSL provider (same two-level pattern)
```

### `segments` — Saved domain filters/lists

```sql
CREATE TABLE segments (
    id          INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(35) NOT NULL,
    description LONGTEXT NOT NULL,
    segment     LONGTEXT NOT NULL,              -- serialized filter criteria
    number_of_domains INT(6) NOT NULL,
    notes       LONGTEXT NOT NULL,
    ...
);

CREATE TABLE segment_data (
    id          INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    segment_id  INT UNSIGNED NOT NULL,
    domain      VARCHAR(255) NOT NULL,          -- FQDN
    active      TINYINT(1) NOT NULL DEFAULT 0,
    inactive    TINYINT(1) NOT NULL DEFAULT 0,
    missing     TINYINT(1) NOT NULL DEFAULT 0,  -- in segment but not in domains table
    filtered    TINYINT(1) NOT NULL DEFAULT 0,
    ...
);
```

### `api_registrars` — Registrar API capability matrix

```sql
CREATE TABLE api_registrars (
    id                      TINYINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name                    VARCHAR(255) NOT NULL,
    req_account_username    TINYINT(1) NOT NULL DEFAULT 0,  -- does API need username?
    req_account_password    TINYINT(1) NOT NULL DEFAULT 0,
    req_account_id          TINYINT(1) NOT NULL DEFAULT 0,
    req_reseller_id         TINYINT(1) NOT NULL DEFAULT 0,
    req_api_app_name        TINYINT(1) NOT NULL DEFAULT 0,
    req_api_key             TINYINT(1) NOT NULL DEFAULT 0,
    req_api_secret          TINYINT(1) NOT NULL DEFAULT 0,
    req_ip_address          TINYINT(1) NOT NULL DEFAULT 0,
    lists_domains           TINYINT(1) NOT NULL DEFAULT 0,  -- can list domains?
    ret_expiry_date         TINYINT(1) NOT NULL DEFAULT 0,  -- returns expiry?
    ret_dns_servers         TINYINT(1) NOT NULL DEFAULT 0,  -- returns NS?
    ret_privacy_status      TINYINT(1) NOT NULL DEFAULT 0,
    ret_autorenewal_status  TINYINT(1) NOT NULL DEFAULT 0,
    ...
);
```

This is clever — it describes what each registrar API supports, so the UI can
show/hide fields dynamically.

### `domain_queue` — Bulk import pipeline

```sql
CREATE TABLE domain_queue (
    id                  INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    api_registrar_id    SMALLINT NOT NULL DEFAULT 0,
    domain_id           INT UNSIGNED NOT NULL DEFAULT 0,
    domain              VARCHAR(255) NOT NULL,
    tld                 VARCHAR(50) NOT NULL,
    expiry_date         DATE NOT NULL,
    -- ...processing flags:
    processing          TINYINT(1) NOT NULL DEFAULT 0,
    ready_to_import     TINYINT(1) NOT NULL DEFAULT 0,
    finished            TINYINT(1) NOT NULL DEFAULT 0,
    already_in_domains  TINYINT(1) NOT NULL DEFAULT 0,
    already_in_queue    TINYINT(1) NOT NULL DEFAULT 0,
    invalid_domain      TINYINT(1) NOT NULL DEFAULT 0,
    ...
);
```

This models an **import queue** — domains fetched from registrar APIs go
through processing before becoming real `domains` rows.

---

## 3. Key Design Patterns

### Pattern 1: Two-Level Provider Abstraction

```
registrars (vendor entity)
  └── registrar_accounts (credential set, tied to an owner)
        └── domains

ssl_providers (vendor entity)
  └── ssl_accounts (credential set, tied to an owner)
        └── ssl_certs
```

Both follow the same pattern. This supports:
- Multiple accounts at the same vendor (different teams/billing)
- Credential isolation per account
- Owner attribution

### Pattern 2: Fee Schedule (Registrar × TLD Matrix)

Fees are not per-domain but per (registrar, TLD) combination:
- `.com` at Namecheap = $10.98/year renewal
- `.com` at GoDaddy = $19.99/year renewal
- `.io` at Namecheap = $25.98/year renewal

`domains.total_cost` is denormalized from `fees.renewal_fee` for quick display.
`domains.fee_fixed = 1` means manual override (premium domain, non-standard
pricing).

### Pattern 3: Import Queue Pipeline

DomainMOD has a full import pipeline:
1. `domain_queue_list` — batch import job (which account to pull from)
2. `domain_queue_temp` — raw data from registrar API (including ns1-ns10)
3. `domain_queue` — processing state (dedup, validation flags)
4. `domain_queue_history` — completed imports for audit

This is a proper ETL pipeline for domain data sync.

### Pattern 4: Segments (Saved Filters)

Segments are materialized views of domain subsets:
- Named filter criteria stored as serialized data
- `segment_data` pre-computes which domains match
- Tracks `active`, `inactive`, `missing` status per domain in segment
- Used for bulk operations and reporting

### Pattern 5: Custom Fields (EAV)

```
domain_fields → field definition (name, type, description)
domain_field_data → values (one row per domain, columns added dynamically)
```

DomainMOD uses ALTER TABLE to add columns to `domain_field_data` dynamically.
This is a poor pattern — we should use JSONB instead.

### Pattern 6: DNS as Nameserver Profile (NOT Record Management)

The `dns` table stores **nameserver profiles** (ns1-ns10 + ip1-ip10), not
DNS records. A domain links to a DNS profile. This is asset-level tracking
("these are our nameservers"), not operational DNS management.

---

## 4. Deficiencies / Anti-Patterns to AVOID

| DomainMOD Pattern | Problem | Our Approach |
|---|---|---|
| No foreign key constraints | All relationships enforced in PHP app layer only | Use real PostgreSQL FKs |
| Passwords in plaintext in `registrar_accounts` | Critical security flaw | Encrypted JSONB or vault reference |
| `dns1`-`dns10` fixed columns | Inflexible, wastes space | JSONB array `nameservers` |
| `ip1`-`ip10` fixed columns | Same | JSONB array |
| Dynamic ALTER TABLE for custom fields | Fragile, no schema safety | JSONB column for custom data |
| No lifecycle state machine | Only `active` flag (0/1) | Keep our 6-state lifecycle |
| No audit log for domain changes | Only generic `log` table | Per-entity history tables |
| `DEFAULT '1970-01-01'` for dates | Sentinel values instead of NULL | Use nullable DATE |
| No soft delete (just `active=0`) | Can't distinguish disabled vs deleted | Use `deleted_at TIMESTAMPTZ` |
| Single `categories` table for both domains and SSL | Mixed concerns | Flexible tags (many-to-many) |
| `segments` with serialized filter | Opaque, not queryable | Use SQL-based saved views or API filter params |

---

## 5. What to Adopt (Refined from Source)

### Adopt Directly

| Pattern | DomainMOD Implementation | Our Adaptation |
|---|---|---|
| Registrar → Account two-level | `registrars` + `registrar_accounts` | Same, with encrypted credentials |
| SSL Provider → Account two-level | `ssl_providers` + `ssl_accounts` | Same pattern |
| Fee schedule (registrar × TLD) | `fees` table | `domain_fee_schedules` with proper FKs |
| `total_cost` denormalized on domain | `domains.total_cost` | `domains.annual_cost` (calculated from fee schedule) |
| `expiry_date` + `autorenew` + `privacy` | Direct on domains table | Same |
| `tld` extracted field | `domains.tld` | Same (useful for grouping/filtering) |
| API capability matrix | `api_registrars` | `registrar_capabilities` JSONB on registrars table |
| Import queue concept | `domain_queue` pipeline | asynq task: `domain:import` with processing state |
| `function` field (domain purpose) | `domains.function` | Rename to `purpose` or use tags |

### Adopt with Improvement

| Concept | Improvement |
|---|---|
| Nameserver tracking | JSONB array instead of dns1-dns10 |
| Custom fields | JSONB `metadata` column on domains |
| Segments | Pinia-side saved filters + optional server-side saved queries |
| Owner concept | Map to our `users` table + `owner_user_id` FK |
| Categories | Replace with flexible many-to-many tags |
| Currency conversion | Per-user display currency, stored in original currency |

---

## 6. Revised Domain Table Design (Post-Analysis)

Based on actual DomainMOD schema analysis, here's our refined design:

```sql
CREATE TABLE domains (
    -- Identity (existing)
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id          BIGINT NOT NULL REFERENCES projects(id),
    fqdn                VARCHAR(512) NOT NULL,
    lifecycle_state     VARCHAR(32) NOT NULL DEFAULT 'requested',

    -- Asset: Registration (inspired by DomainMOD)
    tld                 VARCHAR(64),                    -- extracted from fqdn
    registrar_account_id BIGINT REFERENCES registrar_accounts(id),
    registration_date   DATE,
    expiry_date         DATE,
    auto_renew          BOOLEAN NOT NULL DEFAULT false,
    whois_privacy       BOOLEAN NOT NULL DEFAULT false,
    transfer_lock       BOOLEAN NOT NULL DEFAULT true,  -- registrar lock

    -- Asset: DNS Infrastructure (DomainMOD dns profile → our provider model)
    dns_provider_id     BIGINT REFERENCES dns_providers(id),
    nameservers         JSONB DEFAULT '[]',             -- ["ns1.cf.com","ns2.cf.com"]
    dnssec_enabled      BOOLEAN NOT NULL DEFAULT false,

    -- Asset: Financial (DomainMOD fees model)
    annual_cost         DECIMAL(10,2),
    currency            VARCHAR(3) DEFAULT 'USD',
    purchase_price      DECIMAL(10,2),
    fee_fixed           BOOLEAN NOT NULL DEFAULT false,  -- manual cost override

    -- Asset: Purpose & Metadata
    purpose             VARCHAR(255),                   -- domain purpose (DomainMOD 'function')
    notes               TEXT,
    metadata            JSONB DEFAULT '{}',             -- custom fields (replaces EAV)

    -- Contacts (JSONB, not normalized)
    registrant_contact  JSONB,
    admin_contact       JSONB,
    tech_contact        JSONB,

    -- Ownership
    owner_user_id       BIGINT REFERENCES users(id),

    -- Timestamps
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    UNIQUE(fqdn)
);
```

### Key Differences from Our Previous Draft (DOMAIN_ASSET_LAYER_DESIGN.md)

| Field | Previous Draft | After DomainMOD Analysis |
|---|---|---|
| `transfer_lock` | not included | **Added** — registrar lock is standard |
| `fee_fixed` | not included | **Added** — premium domain manual pricing |
| `purpose` | not included | **Added** — replaces DomainMOD's `function` |
| `metadata` JSONB | not included | **Added** — replaces custom field EAV |
| `grace_end_date` | included (from Nomulus) | **Keep** — but optional, not from DomainMOD |
| `ssl_certificates` table | separate table | **Keep separate** — confirmed by DomainMOD's pattern |

---

## 7. Fee Schedule Design (New Insight)

DomainMOD's `fees` table reveals we need a **fee schedule** separate from
per-domain costs:

```sql
-- Fee schedule: what does each TLD cost at each registrar?
CREATE TABLE domain_fee_schedules (
    id              BIGSERIAL PRIMARY KEY,
    registrar_id    BIGINT NOT NULL REFERENCES registrars(id),
    tld             VARCHAR(64) NOT NULL,
    registration_fee DECIMAL(10,2) NOT NULL DEFAULT 0,
    renewal_fee     DECIMAL(10,2) NOT NULL DEFAULT 0,
    transfer_fee    DECIMAL(10,2) NOT NULL DEFAULT 0,
    privacy_fee     DECIMAL(10,2) NOT NULL DEFAULT 0,
    currency        VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(registrar_id, tld)
);
```

This lets the system **auto-calculate** `domains.annual_cost` from the fee
schedule (unless `fee_fixed = true`).

---

## 8. Import Queue Design (New Insight)

DomainMOD's 4-table import pipeline is over-engineered for our needs (asynq
handles queuing). But the concept is valid:

```go
// Task type: domain:import
// Payload:
type DomainImportPayload struct {
    RegistrarAccountID int64    `json:"registrar_account_id"`
    Domains            []ImportDomain `json:"domains,omitempty"` // if manual
    SyncFromAPI        bool     `json:"sync_from_api"`         // if API pull
}

type ImportDomain struct {
    FQDN        string `json:"fqdn"`
    ExpiryDate  string `json:"expiry_date,omitempty"`
    AutoRenew   bool   `json:"auto_renew"`
    Privacy     bool   `json:"privacy"`
}
```

Processing states (tracked in `domain_import_jobs` table):
- `pending` → `fetching` → `processing` → `completed` / `failed`
- Dedup: skip domains already in `domains` table
- Validation: FQDN format check
- Conflict: if FQDN exists in different project → error, require manual

---

## 9. Summary

DomainMOD confirms our design direction and adds these refinements:

1. **Fee schedule is per (registrar × TLD)**, not per domain — auto-calculate
   annual cost from the schedule
2. **Import queue** is a real requirement for bulk domain onboarding
3. **Registrar API capability matrix** helps the UI show what's possible per
   registrar (auto-sync support, etc.)
4. **`transfer_lock`** and **`fee_fixed`** are standard fields we missed
5. **DNS is NOT record management** at this layer — it's just "which
   nameservers are configured" (asset tracking)
6. **Custom fields → JSONB** is the right call (DomainMOD's ALTER TABLE EAV
   is a known anti-pattern)

DomainMOD's core weakness is that it's purely a ledger with no automation
(no API sync actually works well, no state machine, no deployment). Our
platform fills that gap by connecting the asset layer to the lifecycle +
release pipeline.
