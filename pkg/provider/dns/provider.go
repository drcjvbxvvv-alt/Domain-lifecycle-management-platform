// Package dns defines the DNS provider abstraction layer.
//
// A Provider represents a DNS hosting service (Cloudflare, Route53, etc.)
// that can list, create, update, and delete DNS records via its API.
// The platform uses this to:
//  1. Fetch "expected" records (what the provider says should exist)
//  2. Compare against live DNS resolution (drift detection)
//  3. Manage records programmatically (future: auto-provision)
//
// Each provider implementation reads its config and credentials from the
// dns_providers table's config/credentials JSONB columns.
package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrProviderNotRegistered = errors.New("dns provider type not registered")
	ErrMissingCredentials    = errors.New("dns provider credentials missing or invalid")
	ErrMissingConfig         = errors.New("dns provider config missing or invalid")
	ErrZoneNotFound          = errors.New("dns zone not found")
	ErrRecordNotFound        = errors.New("dns record not found")
	ErrRecordAlreadyExists   = errors.New("dns record already exists")
	ErrRateLimitExceeded     = errors.New("dns provider API rate limit exceeded")
	ErrUnauthorized          = errors.New("dns provider API credentials rejected")
)

// ── Record types ──────────────────────────────────────────────────────────────

// Standard DNS record type identifiers.
// Use these constants instead of raw strings to avoid typos.
const (
	RecordTypeA     = "A"
	RecordTypeAAAA  = "AAAA"
	RecordTypeCNAME = "CNAME"
	RecordTypeTXT   = "TXT"
	RecordTypeMX    = "MX"
	RecordTypeNS    = "NS"
	RecordTypeSRV   = "SRV"
	RecordTypeCAA   = "CAA"
	RecordTypePTR   = "PTR"
)

// ── Record ────────────────────────────────────────────────────────────────────

// Record represents a single DNS record as stored by a DNS provider.
type Record struct {
	ID       string            `json:"id"`                 // provider-specific record ID
	Type     string            `json:"type"`               // A, AAAA, CNAME, MX, TXT, etc.
	Name     string            `json:"name"`               // full record name (e.g. "shop.example.com")
	Content  string            `json:"content"`            // record value
	TTL      int               `json:"ttl"`                // seconds; 1 = automatic (Cloudflare)
	Priority int               `json:"priority,omitempty"` // MX, SRV
	Proxied  bool              `json:"proxied,omitempty"`  // Cloudflare orange-cloud proxy
	Extra    map[string]string `json:"extra,omitempty"`    // provider-specific fields (e.g. SRV weight/port)
}

// RecordFilter limits which records are returned by ListRecords.
type RecordFilter struct {
	Type string // filter by record type (empty = all)
	Name string // filter by record name (empty = all in zone)
}

// ── Provider interface ────────────────────────────────────────────────────────

// Provider is the abstraction for a DNS hosting provider's API.
//
// Implementations must be safe for concurrent use. All methods accept a
// context for cancellation and timeout control.
//
// The zone parameter is provider-specific:
//   - Cloudflare: zone ID (from the Cloudflare dashboard)
//   - Aliyun DNS: domain name (e.g. "example.com")
//   - Tencent DNSPod: domain name
//   - Huawei Cloud DNS: zone ID
//
// When zone is empty, implementations should fall back to the zone
// configured in their credentials/config (if applicable).
type Provider interface {
	// Name returns the provider type identifier (e.g. "cloudflare").
	Name() string

	// ListRecords returns all DNS records matching the filter.
	ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error)

	// CreateRecord creates a new DNS record in the zone.
	// Returns ErrRecordAlreadyExists if a conflicting record exists.
	CreateRecord(ctx context.Context, zone string, record Record) (*Record, error)

	// UpdateRecord replaces an existing DNS record by its provider-specific ID.
	// This is a full replacement (PUT semantics), not a partial update.
	UpdateRecord(ctx context.Context, zone string, recordID string, record Record) (*Record, error)

	// DeleteRecord removes a DNS record by its provider-specific ID.
	// Returns ErrRecordNotFound if the record does not exist.
	DeleteRecord(ctx context.Context, zone string, recordID string) error

	// GetNameservers returns the authoritative nameservers for the zone.
	// These are the NS values the user must configure at their domain registrar.
	// Returns ErrZoneNotFound if the zone does not exist in this account.
	GetNameservers(ctx context.Context, zone string) ([]string, error)

	// BatchCreateRecords creates multiple DNS records in a single logical
	// operation. Implementations may use a provider batch API or loop internally.
	// On partial failure, returns the records created so far plus the error.
	BatchCreateRecords(ctx context.Context, zone string, records []Record) ([]Record, error)

	// BatchDeleteRecords deletes multiple DNS records by their provider-specific
	// IDs. On partial failure, returns the error for the first failed deletion.
	BatchDeleteRecords(ctx context.Context, zone string, recordIDs []string) error
}

// ── Factory ───────────────────────────────────────────────────────────────────

// Factory creates a Provider instance from config and credentials JSON.
type Factory func(config, credentials json.RawMessage) (Provider, error)

// ── Registry ──────────────────────────────────────────────────────────────────

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register adds a provider factory to the global registry.
// Called in init() of each provider implementation file.
func Register(providerType string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[providerType] = factory
}

// Get creates a Provider instance for the given type, using the provided
// config and credentials JSON. Returns ErrProviderNotRegistered if the
// type has no registered factory.
func Get(providerType string, config, credentials json.RawMessage) (Provider, error) {
	registryMu.RLock()
	factory, ok := registry[providerType]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotRegistered, providerType)
	}
	return factory(config, credentials)
}

// RegisteredTypes returns the list of provider types that have been registered.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}
