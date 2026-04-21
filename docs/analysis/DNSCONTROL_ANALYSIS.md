# DNSControl Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/StackExchange/dnscontrol v4 (main branch)
> **Files analyzed**: `models/domain.go`, `models/provider.go`, `models/record.go`
> **Purpose**: Extract precise provider interface design and data model for our
> platform's DNS provider abstraction layer.

---

## 1. Core Architecture Insight

DNSControl separates two distinct roles for a domain:

```
Domain (DomainConfig)
  ├── RegistrarName (string)           → 1 Registrar (manages NS delegation)
  └── DNSProviderNames (map[string]int) → N DNS Providers (manage zone content)
```

A **Registrar** only manages nameserver delegation at the parent zone.
A **DNSProvider** manages the actual zone content (A, CNAME, MX, etc.).
These are separate interfaces — a provider can implement one or both.

---

## 2. Provider Interfaces (Exact from Source)

```go
// models/provider.go

// DNSProvider is an interface for DNS Provider plug-ins.
type DNSProvider interface {
    GetNameservers(domain string) ([]*Nameserver, error)
    GetZoneRecords(dc *DomainConfig) (Records, error)
    GetZoneRecordsCorrections(dc *DomainConfig, existing Records) ([]*Correction, int, error)
}

// Registrar is an interface for Registrar plug-ins.
type Registrar interface {
    GetRegistrarCorrections(dc *DomainConfig) ([]*Correction, error)
}
```

### Method Breakdown

| Interface | Method | Purpose |
|-----------|--------|---------|
| DNSProvider | `GetNameservers(domain)` | Return nameservers this provider assigns to a zone |
| DNSProvider | `GetZoneRecords(dc)` | Fetch current (actual) records from provider API |
| DNSProvider | `GetZoneRecordsCorrections(dc, existing)` | Compare desired (dc.Records) vs actual (existing), return corrections + change count |
| Registrar | `GetRegistrarCorrections(dc)` | Compare desired NS delegation vs actual, return corrections |

### Correction Type

```go
type Correction struct {
    // Human-readable description of what this correction does
    // (e.g., "CREATE A record: foo.example.com → 1.2.3.4")
    Msg string

    // Executable function that applies the correction
    F func() error
}
```

**Key design**: A Correction is a **description + executable function** pair.
The system first collects all corrections (plan phase), displays them for
review, then executes them (apply phase). This is the plan/apply pattern.

### Instance Wrappers

```go
type ProviderBase struct {
    Name         string    // user-assigned name (e.g., "cf_main")
    IsDefault    bool
    ProviderType string    // provider type (e.g., "CLOUDFLAREAPI")
}

type RegistrarInstance struct {
    ProviderBase
    Driver Registrar
}

type DNSProviderInstance struct {
    ProviderBase
    Driver              DNSProvider
    NumberOfNameservers int
}
```

---

## 3. DomainConfig (Exact Fields from Source)

```go
type DomainConfig struct {
    // Identity
    NameRaw     string    // as entered by user in dnsconfig.js
    Name        string    // punycode (IDN) version, no trailing dot
    NameUnicode string    // unicode display version
    Tag         string    // split-horizon tag
    UniqueName  string    // Name + "!" + Tag

    // Provider bindings
    RegistrarName     string            // name of the registrar instance
    DNSProviderNames  map[string]int    // provider name → expected NS count

    // Records (desired state)
    Records      Records               // desired records from config
    EnsureAbsent Records               // records that MUST NOT exist

    // Nameservers
    Nameservers  []*Nameserver

    // Configuration
    Metadata         map[string]string  // arbitrary key-value metadata
    KeepUnknown      bool              // NO_PURGE: don't delete unmanaged records
    Unmanaged        []*UnmanagedConfig // IGNORE() patterns
    AutoDNSSEC       string            // "", "on", "off"

    // Runtime (populated after linking)
    RegistrarInstance     *RegistrarInstance
    DNSProviderInstances  []*DNSProviderInstance

    // Pending corrections (thread-safe accumulation)
    pendingCorrections  map[string][]*Correction
    // ... (mutex, counters, etc.)
}
```

### Design Decisions Observed

1. **Split-horizon via Tag**: same domain name can appear multiple times with
   different tags (different views for different networks).
2. **`KeepUnknown` (NO_PURGE)**: opt-in to preserve records not managed by
   DNSControl. Critical for shared zones.
3. **`EnsureAbsent`**: explicitly declare records that must NOT exist (cleanup).
4. **`Unmanaged` patterns**: fine-grained ignore rules (e.g., ignore all TXT
   records matching `_acme-*`).
5. **Thread-safe correction accumulation**: multiple providers can compute
   corrections concurrently.

---

## 4. RecordConfig (Key Fields from Source)

```go
type RecordConfig struct {
    Type string           // "A", "AAAA", "CNAME", "MX", "TXT", etc.
    TTL  uint32

    // Label (short name + FQDN forms)
    Name        string    // short name ("www", "@")
    NameFQDN    string    // fully qualified ("www.example.com")

    // Target (stored privately, accessed via GetTargetField())
    target string

    // Type-specific fields
    MxPreference    uint16
    SrvPriority     uint16
    SrvWeight       uint16
    SrvPort         uint16
    CaaTag          string
    CaaFlag         uint8
    DsKeyTag        uint16
    DsAlgorithm     uint8
    DsDigestType    uint8
    DsDigest        string
    SvcPriority     uint16
    SvcParams       string
    // ... (20+ more type-specific fields)

    // Metadata
    Metadata  map[string]string
    FilePos   string          // source position in dnsconfig.js
    Original  any             // opaque reference to provider-native object
}
```

### Supported Record Types (from source)

A, AAAA, ALIAS, ANAME, AZURE_ALIAS, CAA, CNAME, DHCID, DNAME, DNSKEY, DS,
HTTPS, LOC, LUA, MX, NAPTR, NS, OPENPGPKEY, PTR, R53_ALIAS, SMIMEA, SOA,
SRV, SSHFP, SVCB, TLSA, TXT + vendor-specific pseudo-types (CF_REDIRECT,
CF_WORKER_ROUTE, AKAMAICDN, etc.)

### Records Collection Type

```go
type Records []*RecordConfig

// Helper methods:
func (recs Records) HasRecordTypeName(rtype, name string) bool
func (recs Records) GetByType(typeName string) Records
func (recs Records) GroupedByKey() map[RecordKey]Records
func (recs Records) GroupedByFQDN() ([]string, map[string]Records)
```

---

## 5. Applicability to Our Platform

### What We Should Adopt

| DNSControl Pattern | Our Adaptation |
|---|---|
| `DNSProvider` interface (3 methods) | Our `pkg/provider/dns/Provider` — same shape but with `context.Context` |
| `Registrar` interface (separate from DNS) | Our `pkg/provider/registrar/Provider` — new package |
| `Correction` (msg + func) | Our `Correction` struct for plan/apply workflow |
| `DomainConfig.DNSProviderNames` (map) | Our `domains.dns_provider_id` (single for now; multi later) |
| `KeepUnknown` / `Unmanaged` | Our domain-level config: whether to purge unmanaged records |
| Thread-safe correction accumulation | Not needed (we don't have concurrent provider computation yet) |

### What We Should NOT Adopt

| DNSControl Pattern | Why Not |
|---|---|
| JavaScript DSL (`dnsconfig.js`) | We use API/UI, not config-as-code |
| `RecordConfig` with 30+ type-specific fields | Over-engineered for our scope; use JSONB for type-specific data |
| `FilePos` tracking | We don't parse config files |
| Vendor-specific pseudo-types (CF_REDIRECT, etc.) | We abstract providers cleanly; no vendor leakage |
| `map[string]int` for DNS providers (count-based) | We start with single provider per domain |

### Proposed Interface for Our Platform

```go
// pkg/provider/dns/provider.go
package dns

import "context"

// Provider manages DNS zone content for a domain.
type Provider interface {
    // Name returns the provider identifier (e.g., "cloudflare").
    Name() string

    // GetZoneRecords fetches all current records in the zone.
    GetZoneRecords(ctx context.Context, zone string) ([]Record, error)

    // PlanChanges computes corrections needed to reach desired state.
    // Returns a list of corrections (human-readable + executable).
    PlanChanges(ctx context.Context, zone string, desired, existing []Record) ([]Correction, error)

    // ApplyCorrections executes a list of corrections.
    ApplyCorrections(ctx context.Context, zone string, corrections []Correction) error
}

// Registrar manages NS delegation at the parent zone.
type Registrar interface {
    // Name returns the registrar identifier.
    Name() string

    // GetNameservers returns the current NS records for the domain.
    GetNameservers(ctx context.Context, domain string) ([]string, error)

    // SetNameservers updates the NS delegation.
    SetNameservers(ctx context.Context, domain string, ns []string) error
}

// Correction represents a single planned change.
type Correction struct {
    Description string          // human-readable ("CREATE A www → 1.2.3.4 TTL 300")
    Type        CorrectionType  // create, update, delete
    Record      Record          // the record being changed
    Exec        func(ctx context.Context) error  // executable (nil during plan-only)
}

type CorrectionType string
const (
    CorrectionCreate CorrectionType = "create"
    CorrectionUpdate CorrectionType = "update"
    CorrectionDelete CorrectionType = "delete"
)

// Record represents a DNS record.
type Record struct {
    ID      string            // provider-assigned ID (empty for desired state)
    Type    string            // A, AAAA, CNAME, MX, TXT, etc.
    Name    string            // short label ("www", "@")
    Content string            // target value
    TTL     uint32
    Extra   map[string]any    // type-specific data (MX priority, SRV weight, etc.)
}
```

### Proposed Registrar Interface

```go
// pkg/provider/registrar/provider.go
package registrar

import "context"

// Provider manages domain registration and NS delegation.
type Provider interface {
    Name() string

    // ListDomains returns all domains managed by this account.
    ListDomains(ctx context.Context) ([]DomainInfo, error)

    // GetDomainInfo returns registration details for a domain.
    GetDomainInfo(ctx context.Context, domain string) (*DomainInfo, error)

    // GetNameservers returns current NS delegation.
    GetNameservers(ctx context.Context, domain string) ([]string, error)

    // SetNameservers updates NS delegation at the registry.
    SetNameservers(ctx context.Context, domain string, ns []string) error
}

type DomainInfo struct {
    FQDN           string
    ExpiryDate     time.Time
    AutoRenew      bool
    Locked         bool     // registrar lock (transfer protection)
    Nameservers    []string
    Status         string   // active, expired, pendingTransfer, etc.
}
```

---

## 6. Summary

DNSControl's provider architecture is clean, minimal, and well-proven. The
key insight is the **strict separation of Registrar (NS management) from
DNSProvider (zone content management)** — these are independent concerns that
happen to both relate to the same domain.

For our platform:
- **Phase A** (domain asset layer): adopt the Registrar interface concept for
  domain info sync (list domains, get expiry, check NS).
- **Phase B** (DNS record management): adopt the DNSProvider interface for
  desired-state record management with plan/apply workflow.

The `Correction` pattern (description + executable function) is the right
abstraction for our plan/apply workflow in both phases.
