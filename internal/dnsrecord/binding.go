package dnsrecord

import (
	"context"
	"errors"
	"fmt"

	"github.com/lib/pq"
	"go.uber.org/zap"

	dnsprovider "domain-platform/pkg/provider/dns"
	"domain-platform/store/postgres"
)

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrProviderNotFound = errors.New("dns provider not found")
)

// ── Types ─────────────────────────────────────────────────────────────────────

// BindingStatus is returned by GetBindingStatus.
type BindingStatus struct {
	DNSProviderID       *int64   `json:"dns_provider_id"`
	NSDelegationStatus  string   `json:"ns_delegation_status"`
	ExpectedNameservers []string `json:"expected_nameservers"`
	ActualNameservers   []string `json:"actual_nameservers"`
	NSVerifiedAt        *string  `json:"ns_verified_at"`
	NSLastCheckedAt     *string  `json:"ns_last_checked_at"`
}

// ── BindDNSProvider ───────────────────────────────────────────────────────────

// BindDNSProvider sets dns_provider_id on the domain and transitions
// ns_delegation_status to 'pending'. Returns the updated binding status
// including the expected nameservers from the provider.
func (s *Service) BindDNSProvider(ctx context.Context, domain *postgres.Domain, providerID int64) (*BindingStatus, error) {
	// Verify the provider exists and is reachable.
	provider, err := s.dnsProviders.GetByID(ctx, providerID)
	if err != nil {
		if errors.Is(err, postgres.ErrDNSProviderNotFound) {
			return nil, fmt.Errorf("%w: %d", ErrProviderNotFound, providerID)
		}
		return nil, fmt.Errorf("fetch dns provider: %w", err)
	}

	// Initialise provider client to resolve expected nameservers.
	p, err := dnsprovider.Get(provider.ProviderType, provider.Config, provider.Credentials)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderInit, err)
	}

	// Update dns_provider_id on the domain.
	domain.DNSProviderID = &providerID
	if err := s.domains.UpdateAssetFields(ctx, domain); err != nil {
		return nil, fmt.Errorf("update domain dns_provider_id: %w", err)
	}

	// Transition ns_delegation_status → pending.
	if err := s.domains.SetNSDelegationPending(ctx, domain.ID); err != nil {
		return nil, fmt.Errorf("set ns delegation pending: %w", err)
	}

	s.logger.Info("bound dns provider",
		zap.String("fqdn", domain.FQDN),
		zap.Int64("provider_id", providerID),
		zap.String("provider_type", provider.ProviderType),
	)

	// Fetch expected nameservers (non-fatal: if it fails, return empty slice).
	expected, nsErr := fetchExpectedNS(ctx, p, domain.FQDN)
	if nsErr != nil {
		s.logger.Warn("could not fetch expected nameservers",
			zap.String("fqdn", domain.FQDN),
			zap.Error(nsErr),
		)
	}

	return &BindingStatus{
		DNSProviderID:       &providerID,
		NSDelegationStatus:  "pending",
		ExpectedNameservers: expected,
		ActualNameservers:   []string{},
	}, nil
}

// ── UnbindDNSProvider ─────────────────────────────────────────────────────────

// UnbindDNSProvider clears dns_provider_id and resets ns_delegation_status
// to 'unset'.
func (s *Service) UnbindDNSProvider(ctx context.Context, domain *postgres.Domain) error {
	domain.DNSProviderID = nil
	if err := s.domains.UpdateAssetFields(ctx, domain); err != nil {
		return fmt.Errorf("clear domain dns_provider_id: %w", err)
	}

	if err := s.domains.UpdateNSDelegation(ctx, domain.ID, "unset", pq.StringArray{}); err != nil {
		return fmt.Errorf("reset ns delegation status: %w", err)
	}

	s.logger.Info("unbound dns provider",
		zap.String("fqdn", domain.FQDN),
	)
	return nil
}

// ── GetBindingStatus ──────────────────────────────────────────────────────────

// GetBindingStatus returns the current binding status for a domain, including
// expected nameservers from the provider (if bound) and actual observed NSes.
func (s *Service) GetBindingStatus(ctx context.Context, domain *postgres.Domain) (*BindingStatus, error) {
	status := &BindingStatus{
		DNSProviderID:      domain.DNSProviderID,
		NSDelegationStatus: domain.NSDelegationStatus,
		ActualNameservers:  toStringSlice(domain.NSActual),
	}

	if domain.NSVerifiedAt != nil {
		t := domain.NSVerifiedAt.UTC().Format("2006-01-02T15:04:05Z")
		status.NSVerifiedAt = &t
	}
	if domain.NSLastCheckedAt != nil {
		t := domain.NSLastCheckedAt.UTC().Format("2006-01-02T15:04:05Z")
		status.NSLastCheckedAt = &t
	}

	// Fetch expected nameservers if a provider is bound.
	if domain.DNSProviderID != nil {
		provider, err := s.dnsProviders.GetByID(ctx, *domain.DNSProviderID)
		if err == nil {
			if p, err := dnsprovider.Get(provider.ProviderType, provider.Config, provider.Credentials); err == nil {
				expected, _ := fetchExpectedNS(ctx, p, domain.FQDN)
				status.ExpectedNameservers = expected
			}
		}
	}
	if status.ExpectedNameservers == nil {
		status.ExpectedNameservers = []string{}
	}

	return status, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// fetchExpectedNS calls GetNameservers on the provider for the given FQDN.
// Returns an empty slice (not an error) if the provider returns no nameservers.
func fetchExpectedNS(ctx context.Context, p dnsprovider.Provider, fqdn string) ([]string, error) {
	ns, err := p.GetNameservers(ctx, fqdn)
	if err != nil {
		return []string{}, err
	}
	if ns == nil {
		return []string{}, nil
	}
	return ns, nil
}

// resolveProviderFromRecord creates a Provider client from a persisted DNSProvider record.
// Exported for use by the NS check task handler.
func resolveProviderFromRecord(svc *Service, provider *postgres.DNSProvider) (dnsprovider.Provider, error) {
	_ = svc // kept for API symmetry
	p, err := dnsprovider.Get(provider.ProviderType, provider.Config, provider.Credentials)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderInit, err)
	}
	return p, nil
}

func toStringSlice(a pq.StringArray) []string {
	if a == nil {
		return []string{}
	}
	return []string(a)
}
