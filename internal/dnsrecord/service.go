// Package dnsrecord provides DNS record CRUD operations through DNS provider APIs.
// It bridges the platform's domain model with the pkg/provider/dns abstraction,
// handling provider lookup, zone resolution, and input validation.
package dnsrecord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	dnsprovider "domain-platform/pkg/provider/dns"
	"domain-platform/store/postgres"
)

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrNoProvider      = errors.New("domain has no DNS provider configured")
	ErrProviderInit    = errors.New("failed to initialise DNS provider")
	ErrInvalidInput    = errors.New("invalid input")
)

// ── Service ───────────────────────────────────────────────────────────────────

// Service manages DNS records via provider APIs.
type Service struct {
	dnsProviders *postgres.DNSProviderStore
	domains      *postgres.DomainStore
	logger       *zap.Logger
}

// NewService creates a DNS record management service.
func NewService(dnsProviders *postgres.DNSProviderStore, domains *postgres.DomainStore, logger *zap.Logger) *Service {
	return &Service{dnsProviders: dnsProviders, domains: domains, logger: logger}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolveProvider fetches the domain's DNS provider from DB and creates a Provider instance.
func (s *Service) resolveProvider(ctx context.Context, domain *postgres.Domain) (dnsprovider.Provider, string, error) {
	if domain.DNSProviderID == nil {
		return nil, "", ErrNoProvider
	}

	provider, err := s.dnsProviders.GetByID(ctx, *domain.DNSProviderID)
	if err != nil {
		return nil, "", fmt.Errorf("fetch dns provider: %w", err)
	}

	p, err := dnsprovider.Get(provider.ProviderType, provider.Config, provider.Credentials)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrProviderInit, err)
	}

	zoneID := extractZoneID(provider.Config)
	return p, zoneID, nil
}

func extractZoneID(config json.RawMessage) string {
	var c struct {
		ZoneID string `json:"zone_id"`
	}
	_ = json.Unmarshal(config, &c)
	return c.ZoneID
}

// ── Valid record types ────────────────────────────────────────────────────────

var validRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true,
	"TXT": true, "NS": true, "SRV": true, "CAA": true, "PTR": true,
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

// ListRecords returns DNS records from the provider for the given domain.
// filterType can be empty (all types) or a specific record type.
func (s *Service) ListRecords(ctx context.Context, domain *postgres.Domain, filterType string) ([]dnsprovider.Record, error) {
	p, zoneID, err := s.resolveProvider(ctx, domain)
	if err != nil {
		return nil, err
	}

	filter := dnsprovider.RecordFilter{Name: domain.FQDN}
	if filterType != "" {
		filter.Type = strings.ToUpper(filterType)
	}

	records, err := p.ListRecords(ctx, zoneID, filter)
	if err != nil {
		return nil, fmt.Errorf("list records from %s: %w", p.Name(), err)
	}

	s.logger.Info("listed provider records",
		zap.String("fqdn", domain.FQDN),
		zap.String("provider", p.Name()),
		zap.Int("count", len(records)),
	)

	return records, nil
}

// CreateRecordInput is the input for creating a DNS record.
type CreateRecordInput struct {
	Type     string `json:"type"`     // A, AAAA, CNAME, MX, TXT, etc.
	Name     string `json:"name"`     // record name (full FQDN or subdomain)
	Content  string `json:"content"`  // record value
	TTL      int    `json:"ttl"`      // 0 or 1 = auto
	Priority int    `json:"priority"` // MX / SRV
	Proxied  bool   `json:"proxied"`  // Cloudflare-specific
}

// Validate checks the input fields.
func (in *CreateRecordInput) Validate() error {
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	in.Name = strings.TrimSpace(in.Name)
	in.Content = strings.TrimSpace(in.Content)

	if !validRecordTypes[in.Type] {
		return fmt.Errorf("%w: unsupported record type %q", ErrInvalidInput, in.Type)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if in.Content == "" {
		return fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if in.TTL < 0 {
		return fmt.Errorf("%w: TTL must be >= 0", ErrInvalidInput)
	}
	return nil
}

// CreateRecord adds a DNS record via the provider API.
func (s *Service) CreateRecord(ctx context.Context, domain *postgres.Domain, in CreateRecordInput) (*dnsprovider.Record, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	p, zoneID, err := s.resolveProvider(ctx, domain)
	if err != nil {
		return nil, err
	}

	rec := dnsprovider.Record{
		Type:     in.Type,
		Name:     in.Name,
		Content:  in.Content,
		TTL:      in.TTL,
		Priority: in.Priority,
		Proxied:  in.Proxied,
	}

	created, err := p.CreateRecord(ctx, zoneID, rec)
	if err != nil {
		return nil, fmt.Errorf("create record via %s: %w", p.Name(), err)
	}

	s.logger.Info("created provider record",
		zap.String("fqdn", domain.FQDN),
		zap.String("provider", p.Name()),
		zap.String("record_id", created.ID),
		zap.String("type", created.Type),
		zap.String("content", created.Content),
	)

	return created, nil
}

// UpdateRecordInput is the input for updating a DNS record.
type UpdateRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
	Proxied  bool   `json:"proxied"`
}

// Validate checks the input fields.
func (in *UpdateRecordInput) Validate() error {
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	in.Name = strings.TrimSpace(in.Name)
	in.Content = strings.TrimSpace(in.Content)

	if !validRecordTypes[in.Type] {
		return fmt.Errorf("%w: unsupported record type %q", ErrInvalidInput, in.Type)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if in.Content == "" {
		return fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	return nil
}

// UpdateRecord modifies an existing DNS record via the provider API.
func (s *Service) UpdateRecord(ctx context.Context, domain *postgres.Domain, recordID string, in UpdateRecordInput) (*dnsprovider.Record, error) {
	if recordID == "" {
		return nil, fmt.Errorf("%w: record ID is required", ErrInvalidInput)
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}

	p, zoneID, err := s.resolveProvider(ctx, domain)
	if err != nil {
		return nil, err
	}

	rec := dnsprovider.Record{
		Type:     in.Type,
		Name:     in.Name,
		Content:  in.Content,
		TTL:      in.TTL,
		Priority: in.Priority,
		Proxied:  in.Proxied,
	}

	updated, err := p.UpdateRecord(ctx, zoneID, recordID, rec)
	if err != nil {
		return nil, fmt.Errorf("update record %s via %s: %w", recordID, p.Name(), err)
	}

	s.logger.Info("updated provider record",
		zap.String("fqdn", domain.FQDN),
		zap.String("provider", p.Name()),
		zap.String("record_id", recordID),
		zap.String("type", updated.Type),
		zap.String("content", updated.Content),
	)

	return updated, nil
}

// DeleteRecord removes a DNS record via the provider API.
func (s *Service) DeleteRecord(ctx context.Context, domain *postgres.Domain, recordID string) error {
	if recordID == "" {
		return fmt.Errorf("%w: record ID is required", ErrInvalidInput)
	}

	p, zoneID, err := s.resolveProvider(ctx, domain)
	if err != nil {
		return err
	}

	if err := p.DeleteRecord(ctx, zoneID, recordID); err != nil {
		return fmt.Errorf("delete record %s via %s: %w", recordID, p.Name(), err)
	}

	s.logger.Info("deleted provider record",
		zap.String("fqdn", domain.FQDN),
		zap.String("provider", p.Name()),
		zap.String("record_id", recordID),
	)

	return nil
}
