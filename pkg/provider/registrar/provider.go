// Package registrar defines the domain registrar provider abstraction.
//
// A Provider represents a domain registrar's API (GoDaddy, Namecheap, etc.)
// that can list owned domains and fetch registration/expiry dates.
// The platform uses this to sync registration_date and expiry_date on domains.
//
// Each provider reads its credentials from the registrar_accounts.credentials
// JSONB column. The credentials format is provider-specific (JSON object).
//
// Usage:
//
//	provider, err := registrar.Get("godaddy", account.Credentials)
//	domains, err := provider.ListDomains(ctx)
package registrar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ── Errors ─────────────────────────────────────────────────────────────────────

var (
	ErrProviderNotRegistered = errors.New("registrar provider type not registered")
	ErrMissingCredentials    = errors.New("registrar credentials missing or invalid")
	ErrDomainNotFound        = errors.New("domain not found in registrar")
	ErrRateLimitExceeded     = errors.New("registrar API rate limit exceeded")
	ErrUnauthorized          = errors.New("registrar API credentials rejected — check your Key and Secret")
	// ErrAccessDenied is returned when credentials are valid but the account
	// does not have API access (e.g. GoDaddy retail accounts after 2023).
	ErrAccessDenied = errors.New("registrar API access denied — account does not have API permission")
)

// ── DomainInfo ─────────────────────────────────────────────────────────────────

// DomainInfo holds the information returned by a registrar for a single domain.
type DomainInfo struct {
	FQDN             string
	RegistrationDate *time.Time
	ExpiryDate       *time.Time
	AutoRenew        bool
	NameServers      []string
	Status           string // provider-specific, e.g. "ACTIVE", "EXPIRED"
}

// ── Provider interface ─────────────────────────────────────────────────────────

// Provider is the abstraction for a domain registrar's management API.
type Provider interface {
	// Name returns the provider type identifier (e.g. "godaddy").
	Name() string

	// ListDomains fetches all domains owned by the account.
	// Handles pagination internally; returns the full list.
	ListDomains(ctx context.Context) ([]DomainInfo, error)

	// GetDomain fetches info for a single domain by FQDN.
	// Returns ErrDomainNotFound if the domain is not in this account.
	GetDomain(ctx context.Context, fqdn string) (*DomainInfo, error)
}

// ── Factory + Registry ─────────────────────────────────────────────────────────

// Factory creates a Provider from credentials JSON.
// credentials is the raw JSONB value from registrar_accounts.credentials.
type Factory func(credentials json.RawMessage) (Provider, error)

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

// Get creates a Provider instance for the given type using credentials JSON.
// Returns ErrProviderNotRegistered if the type has no factory.
func Get(providerType string, credentials json.RawMessage) (Provider, error) {
	registryMu.RLock()
	factory, ok := registry[providerType]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotRegistered, providerType)
	}
	return factory(credentials)
}

// RegisteredTypes returns the list of currently registered provider types.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}
