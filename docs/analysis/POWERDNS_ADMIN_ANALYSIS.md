# PowerDNS-Admin Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/PowerDNS-Admin/PowerDNS-Admin (master branch)
> **Files analyzed**: `models/domain.py`, `models/user.py`, `models/role.py`,
> `models/history.py`, `models/account.py`, `models/domain_template.py`
> **Purpose**: Extract DNS web management UI patterns (RBAC, audit logging,
> zone templates) for our platform.

---

## 1. Architecture Overview

PowerDNS-Admin is a **web UI proxy** for PowerDNS Authoritative Server:
- It does NOT store DNS records itself — it proxies to PowerDNS API
- It ADDS: user management, RBAC, audit logging, zone templates, API keys
- Communication: PDA → PowerDNS REST API (port 8081)

This is relevant to us because we also proxy to external DNS providers
(Cloudflare, Route53) and need the same UI/RBAC/audit layer.

---

## 2. Data Models (Exact from Source)

### Domain (Zone) Model

```python
class Domain(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    name = db.Column(db.String(255), index=True, unique=True)  # zone FQDN
    master = db.Column(db.String(128))                          # master server IP (for slave)
    type = db.Column(db.String(8), nullable=False)              # NATIVE, MASTER, SLAVE
    serial = db.Column(db.BigInteger)                           # SOA serial
    notified_serial = db.Column(db.BigInteger)
    last_check = db.Column(db.Integer)                          # last slave check timestamp
    dnssec = db.Column(db.Integer)                              # DNSSEC enabled flag
    account_id = db.Column(db.Integer, ForeignKey('account.id'))

    # Relationships
    account = relationship("Account", back_populates="domains")
    settings = relationship('DomainSetting', back_populates='domain')
    apikeys = relationship("ApiKey", secondary=domain_apikey)
```

Key methods:
- `add()` — creates zone in PowerDNS API + local DB row
- `delete()` — deletes from PowerDNS API + local DB
- `update()` — syncs local DB from PowerDNS API state
- `is_valid_access(user_id)` — checks DomainUser OR AccountUser membership
- `grant_privileges(user_ids)` — grants access to users

### User Model

```python
class User(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    username = db.Column(db.String(64), unique=True)
    password = db.Column(db.String(64))        # bcrypt hash
    firstname = db.Column(db.String(64))
    lastname = db.Column(db.String(64))
    email = db.Column(db.String(128))
    otp_secret = db.Column(db.String(16))      # TOTP 2FA
    confirmed = db.Column(db.SmallInteger, default=0)
    role_id = db.Column(db.Integer, ForeignKey('role.id'))  # single role
```

Auth methods: local bcrypt, LDAP/AD, with TOTP 2FA support.

### Role Model

```python
class Role(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    name = db.Column(db.String(64), unique=True)    # "Administrator", "Operator", "User"
    description = db.Column(db.String(128))
```

**Three fixed roles** — no dynamic permission matrix.

### Account Model (Organization/Tenant)

```python
class Account(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    name = db.Column(db.String(40), unique=True)
    description = db.Column(db.String(128))
    contact = db.Column(db.String(128))
    mail = db.Column(db.String(128))
    domains = relationship("Domain", back_populates="account")
```

Account is the grouping unit:
- An Account owns N Domains
- Users belong to Accounts (many-to-many via `AccountUser`)
- User with Account membership → access to all Account's domains

### History (Audit Log)

```python
class History(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    msg = db.Column(db.String(256))          # action description
    detail = db.Column(db.Text())            # JSON: change details
    created_by = db.Column(db.String(128))   # username (not FK!)
    created_on = db.Column(db.DateTime, default=utcnow)
    domain_id = db.Column(db.Integer, ForeignKey('domain.id'), nullable=True)
```

**Minimal audit** — no before/after field-level diff, no structured action
type enum. Just free-text message + optional JSON blob.

### Domain Template

```python
class DomainTemplate(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    name = db.Column(db.String(255), unique=True)
    description = db.Column(db.String(255))
    records = relationship('DomainTemplateRecord', cascade="all, delete-orphan")

class DomainTemplateRecord(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    name = db.Column(db.String(255))     # record name ("www", "@", "mail")
    type = db.Column(db.String(64))      # A, AAAA, CNAME, MX, TXT
    ttl = db.Column(db.Integer)
    data = db.Column(db.Text)            # record content/target
    comment = db.Column(db.Text)
    status = db.Column(db.Boolean)       # enabled/disabled
    template_id = db.Column(db.Integer, ForeignKey('domain_template.id'))
```

Templates are **mutable** (no versioning). Applied on zone creation to
pre-populate records.

---

## 3. RBAC Pattern (Exact from Source)

### Access Control Flow

```
User requests action on Domain
    │
    ├── Is user role = "Administrator"? → ALLOW (full access)
    │
    ├── Is user in DomainUser for this domain? → ALLOW
    │
    ├── Is user in AccountUser for domain's account? → ALLOW
    │
    └── DENY
```

### Implementation

```python
# Domain.is_valid_access(user_id)
def is_valid_access(self, user_id):
    # Check DomainUser table
    if DomainUser.query.filter_by(user_id=user_id, domain_id=self.id).first():
        return True
    # Check AccountUser → Account → Domain chain
    if self.account_id:
        if AccountUser.query.filter_by(user_id=user_id, account_id=self.account_id).first():
            return True
    return False
```

### Join Tables

```python
# DomainUser — direct domain access
domain_id + user_id (composite PK)

# AccountUser — account membership
account_id + user_id (composite PK)
```

---

## 4. Key Design Patterns

### Pattern 1: Two-Path Access Control

Access is granted through EITHER:
- **Direct**: User → Domain (fine-grained, per-zone)
- **Indirect**: User → Account → Domain (org-level, all zones in account)

This is simple and effective. No complex permission matrix needed.

### Pattern 2: Proxy Pattern (No Local Record Storage)

PDA doesn't duplicate PowerDNS data — it proxies all record operations:
- Zone list: `GET /api/v1/servers/localhost/zones` → PowerDNS
- Record edit: `PATCH /api/v1/servers/localhost/zones/{zone}` → PowerDNS
- Local DB only stores: access control, audit, templates, settings

**Implication for us**: We should also avoid duplicating DNS records in our
DB when we add DNS management. Use the provider as source of truth; our DB
stores only desired state + access control + audit.

### Pattern 3: Staged Edit + Batch Apply (Frontend Pattern)

The frontend collects multiple record edits in memory, then submits them as
one batch via a single API call. This prevents:
- Intermediate invalid states
- Multiple API roundtrips
- Partial updates on failure

### Pattern 4: Minimal Audit (Simple but Insufficient)

PDA's audit is too minimal for enterprise use:
- No structured action type (just free-text `msg`)
- No before/after state capture
- `created_by` is a username string, not a FK (can't handle renames)
- No filtering by action type

We should do better — see our proposed improvement below.

---

## 5. Applicability to Our Platform

### What We Should Adopt

| PowerDNS-Admin Pattern | Our Adaptation |
|---|---|
| Two-path access (direct + account) | `domain_permissions` + project membership |
| Account as org grouping | Our `projects` table already serves this role |
| Domain template (pre-populated records) | Phase B: DNS record templates for new domains |
| Proxy pattern (don't duplicate DNS records) | Store desired state in DB, sync to provider; DON'T cache actual state |

### What We Should Improve Upon

| PowerDNS-Admin Weakness | Our Improvement |
|---|---|
| Free-text audit `msg` | Structured `action` enum + `target_kind` + `detail` JSONB with before/after |
| `created_by` as string | FK to `users.id` (nullable for system actions) |
| No domain_id on all audits | Required `target_id` on every audit row |
| Mutable templates (no versioning) | We already have immutable `template_versions` |
| 3 fixed roles only | We have configurable RBAC with per-route permission checks |
| No API rate limiting | We plan this for Phase 3+ |
| No 2FA enforcement policy | Add to our auth module |

### Proposed DNS Access Control for Our Platform

```sql
-- Domain-level permission (fine-grained, optional)
CREATE TABLE domain_permissions (
    id          BIGSERIAL PRIMARY KEY,
    domain_id   BIGINT NOT NULL REFERENCES domains(id),
    user_id     BIGINT NOT NULL REFERENCES users(id),
    permission  VARCHAR(32) NOT NULL DEFAULT 'viewer',  -- viewer, editor, admin
    granted_by  BIGINT REFERENCES users(id),
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(domain_id, user_id)
);
```

Access flow:
1. Is user `admin` role? → full access
2. Is user member of domain's project with sufficient role? → project-level access
3. Is user in `domain_permissions` for this domain? → domain-level access
4. Otherwise → deny

---

## 6. Summary

PowerDNS-Admin validates our existing architecture choices and adds two
actionable patterns:

1. **Two-path access control** (direct + org) — we implement this as
   project membership + optional domain-level permissions
2. **Proxy pattern for DNS** — don't cache provider state locally; store
   only desired state and sync

Its weaknesses (minimal audit, no template versioning, limited RBAC) are
areas where our platform is already stronger.
