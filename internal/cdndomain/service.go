// Package cdndomain manages the lifecycle of CDN acceleration domains:
// binding a platform domain to a CDN account, removing it, and refreshing
// its status from the CDN provider.
//
// Write-through pattern: every mutating operation calls the CDN provider API
// first and only persists locally on success.  This keeps the local DB in
// sync with the CDN platform and prevents phantom bindings.
package cdndomain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	cdnprovider "domain-platform/pkg/provider/cdn"
	"domain-platform/store/postgres"
)

// ── Sentinel errors ────────────────────────────────────────────────────────────

var (
	// ErrBindingNotFound is returned when the requested CDN binding does not exist.
	ErrBindingNotFound = postgres.ErrCDNBindingNotFound

	// ErrBindingAlreadyExists is returned when trying to bind a domain to a CDN
	// account that already has an active binding for that domain.
	ErrBindingAlreadyExists = postgres.ErrCDNBindingAlreadyExists

	// ErrNoCDNProvider is returned when the CDN provider type required by the
	// account is not registered in the cdn package registry.
	ErrNoCDNProvider = errors.New("cdn provider type not registered")

	// ErrAccountNotFound is returned when the referenced CDN account does not exist.
	ErrAccountNotFound = postgres.ErrCDNAccountNotFound
)

// ── Response types ────────────────────────────────────────────────────────────

// BindingResponse is the API-facing view of a DomainCDNBinding row.
// It is always constructed from the local DB record; live status is fetched
// on demand via GetBinding or RefreshStatus.
type BindingResponse struct {
	ID           int64   `json:"id"`
	UUID         string  `json:"uuid"`
	DomainID     int64   `json:"domain_id"`
	CDNAccountID int64   `json:"cdn_account_id"`
	CDNCNAME     *string `json:"cdn_cname"`
	BusinessType string  `json:"business_type"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service handles CDN domain lifecycle operations for the control plane.
type Service struct {
	bindings *postgres.CDNBindingStore
	cdn      *postgres.CDNStore
	logger   *zap.Logger
}

// NewService creates a Service.
func NewService(
	bindings *postgres.CDNBindingStore,
	cdn *postgres.CDNStore,
	logger *zap.Logger,
) *Service {
	return &Service{bindings: bindings, cdn: cdn, logger: logger}
}

// ── Bind ──────────────────────────────────────────────────────────────────────

// BindDomain registers the domain as an acceleration domain on the CDN provider
// and saves the binding locally.
//
// Flow:
//  1. Load CDN account + its parent provider type.
//  2. Resolve the CDN provider factory via the registry.
//  3. Call provider.AddDomain — returns the provider-assigned CNAME.
//  4. Persist binding with status + CNAME.
//
// Returns ErrNoCDNProvider if the provider type is not yet registered.
// Returns ErrBindingAlreadyExists if an active binding already exists.
func (s *Service) BindDomain(ctx context.Context, domain *postgres.Domain, cdnAccountID int64, businessType string) (*BindingResponse, error) {
	if businessType == "" {
		businessType = cdnprovider.BusinessTypeWeb
	}

	provider, account, err := s.resolveProvider(ctx, cdnAccountID)
	if err != nil {
		return nil, err
	}

	// Call CDN provider — write-through: provider first.
	req := cdnprovider.AddDomainRequest{
		Domain:       domain.FQDN,
		BusinessType: businessType,
	}
	cdnDomain, err := provider.AddDomain(ctx, req)
	if err != nil {
		if errors.Is(err, cdnprovider.ErrDomainAlreadyExists) {
			// Domain already exists on the CDN — fetch its current state so we
			// can still create the local binding.
			cdnDomain, err = provider.GetDomain(ctx, domain.FQDN)
			if err != nil {
				return nil, fmt.Errorf("cdn add domain (already exists) then get domain: %w", err)
			}
		} else {
			return nil, fmt.Errorf("cdn add domain %s: %w", domain.FQDN, err)
		}
	}

	b := &postgres.DomainCDNBinding{
		DomainID:     domain.ID,
		CDNAccountID: account.ID,
		BusinessType: businessType,
		Status:       cdnDomain.Status,
	}
	if cdnDomain.CNAME != "" {
		b.CDNCNAME = &cdnDomain.CNAME
	}

	saved, err := s.bindings.CreateBinding(ctx, b)
	if err != nil {
		// If the binding already exists locally but the provider accepted the
		// AddDomain call, the system is in sync — surface the existing binding.
		if errors.Is(err, postgres.ErrCDNBindingAlreadyExists) {
			return nil, ErrBindingAlreadyExists
		}
		return nil, fmt.Errorf("persist cdn binding for %s: %w", domain.FQDN, err)
	}

	s.logger.Info("cdn domain bound",
		zap.String("fqdn", domain.FQDN),
		zap.Int64("cdn_account_id", cdnAccountID),
		zap.String("provider", provider.Name()),
		zap.Stringp("cname", saved.CDNCNAME),
	)
	return bindingToResponse(saved), nil
}

// ── Unbind ────────────────────────────────────────────────────────────────────

// UnbindDomain removes the acceleration domain from the CDN provider and
// soft-deletes the local binding.
//
// Returns ErrBindingNotFound if no active binding with bindingID exists or
// it does not belong to the given domain.
func (s *Service) UnbindDomain(ctx context.Context, domain *postgres.Domain, bindingID int64) error {
	binding, err := s.bindings.GetBindingByID(ctx, bindingID)
	if err != nil {
		return err // ErrBindingNotFound propagates unchanged
	}
	if binding.DomainID != domain.ID {
		// Binding belongs to a different domain — treat as not found.
		return ErrBindingNotFound
	}

	provider, _, err := s.resolveProvider(ctx, binding.CDNAccountID)
	if err != nil {
		return err
	}

	if err := provider.RemoveDomain(ctx, domain.FQDN); err != nil {
		if !errors.Is(err, cdnprovider.ErrDomainNotFound) {
			return fmt.Errorf("cdn remove domain %s: %w", domain.FQDN, err)
		}
		// Domain already gone on the CDN side — still clean up locally.
	}

	if err := s.bindings.SoftDeleteBinding(ctx, bindingID); err != nil {
		return fmt.Errorf("soft delete cdn binding %d: %w", bindingID, err)
	}

	s.logger.Info("cdn domain unbound",
		zap.String("fqdn", domain.FQDN),
		zap.Int64("binding_id", bindingID),
	)
	return nil
}

// ── GetBinding ────────────────────────────────────────────────────────────────

// GetBinding returns the local snapshot of a CDN binding.
// It does NOT make a live provider call — use RefreshStatus for that.
func (s *Service) GetBinding(ctx context.Context, bindingID int64) (*BindingResponse, error) {
	b, err := s.bindings.GetBindingByID(ctx, bindingID)
	if err != nil {
		return nil, err
	}
	return bindingToResponse(b), nil
}

// ── ListBindings ──────────────────────────────────────────────────────────────

// ListBindings returns all active CDN bindings for a domain.
func (s *Service) ListBindings(ctx context.Context, domainID int64) ([]BindingResponse, error) {
	bindings, err := s.bindings.ListBindingsByDomain(ctx, domainID)
	if err != nil {
		return nil, fmt.Errorf("list cdn bindings: %w", err)
	}
	out := make([]BindingResponse, len(bindings))
	for i, b := range bindings {
		out[i] = *bindingToResponse(&b)
	}
	return out, nil
}

// ── RefreshStatus ─────────────────────────────────────────────────────────────

// RefreshStatus polls the CDN provider for the current domain status/CNAME and
// updates the local binding record.
//
// Returns ErrBindingNotFound if the binding does not exist.
// Returns ErrNoCDNProvider if the provider type is not registered.
func (s *Service) RefreshStatus(ctx context.Context, domain *postgres.Domain, bindingID int64) (*BindingResponse, error) {
	binding, err := s.bindings.GetBindingByID(ctx, bindingID)
	if err != nil {
		return nil, err
	}
	if binding.DomainID != domain.ID {
		return nil, ErrBindingNotFound
	}

	provider, _, err := s.resolveProvider(ctx, binding.CDNAccountID)
	if err != nil {
		return nil, err
	}

	cdnDomain, err := provider.GetDomain(ctx, domain.FQDN)
	if err != nil {
		if errors.Is(err, cdnprovider.ErrDomainNotFound) {
			// Domain was deleted on the CDN side behind our back — mark offline.
			cdnDomain = &cdnprovider.CDNDomain{Status: cdnprovider.DomainStatusOffline}
		} else {
			return nil, fmt.Errorf("cdn get domain %s: %w", domain.FQDN, err)
		}
	}

	cname := cdnDomain.CNAME
	updated, err := s.bindings.UpdateBindingStatus(ctx, bindingID, cdnDomain.Status, cname)
	if err != nil {
		return nil, fmt.Errorf("update cdn binding status: %w", err)
	}

	return bindingToResponse(updated), nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// resolveProvider loads the CDN account and builds a live CDN provider from the
// global registry.  Returns ErrAccountNotFound or ErrNoCDNProvider on failure.
func (s *Service) resolveProvider(ctx context.Context, cdnAccountID int64) (cdnprovider.Provider, *postgres.CDNAccount, error) {
	account, err := s.cdn.GetAccountByID(ctx, cdnAccountID)
	if err != nil {
		return nil, nil, err // ErrCDNAccountNotFound propagates
	}

	cdnParent, err := s.cdn.GetProviderByID(ctx, account.CDNProviderID)
	if err != nil {
		return nil, nil, fmt.Errorf("get cdn provider for account %d: %w", cdnAccountID, err)
	}

	provider, err := cdnprovider.Get(cdnParent.ProviderType, json.RawMessage(`{}`), account.Credentials)
	if err != nil {
		if errors.Is(err, cdnprovider.ErrProviderNotRegistered) {
			return nil, nil, fmt.Errorf("%w: %s", ErrNoCDNProvider, cdnParent.ProviderType)
		}
		return nil, nil, fmt.Errorf("build cdn provider %s: %w", cdnParent.ProviderType, err)
	}

	return provider, account, nil
}

// bindingToResponse converts a store row to the API response type.
func bindingToResponse(b *postgres.DomainCDNBinding) *BindingResponse {
	return &BindingResponse{
		ID:           b.ID,
		UUID:         b.UUID,
		DomainID:     b.DomainID,
		CDNAccountID: b.CDNAccountID,
		CDNCNAME:     b.CDNCNAME,
		BusinessType: b.BusinessType,
		Status:       b.Status,
		CreatedAt:    b.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    b.UpdatedAt.Format(time.RFC3339),
	}
}
