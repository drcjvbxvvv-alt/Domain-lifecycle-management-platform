package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// KnownProviderTypes is the canonical list of supported DNS provider types.
// Validated on Create/Update; new integrations require adding here + a pkg/provider/dns implementation.
var KnownProviderTypes = map[string]bool{
	"cloudflare": true,
	"route53":    true,
	"dnspod":     true,
	"alidns":     true,
	"godaddy":    true,
	"namecheap":  true,
	"manual":     true, // human-managed DNS; no API integration
}

var (
	ErrNotFound            = errors.New("dns provider not found")
	ErrInvalidProviderType = errors.New("unknown dns provider type")
	ErrHasDependents       = errors.New("dns provider has dependent domains — detach first")
)

type Service struct {
	store  *postgres.DNSProviderStore
	logger *zap.Logger
}

func NewService(store *postgres.DNSProviderStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

type CreateInput struct {
	Name         string
	ProviderType string
	Config       []byte // raw JSON zone config; nil → "{}"
	Credentials  []byte // raw JSON credentials; nil → "{}"
	Notes        *string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.DNSProvider, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("provider name required")
	}
	if !KnownProviderTypes[in.ProviderType] {
		return nil, ErrInvalidProviderType
	}

	cfg := in.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	creds := in.Credentials
	if len(creds) == 0 {
		creds = []byte("{}")
	}

	p := &postgres.DNSProvider{
		Name:         in.Name,
		ProviderType: in.ProviderType,
		Config:       cfg,
		Credentials:  creds,
		Notes:        in.Notes,
	}

	created, err := s.store.Create(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("create dns provider: %w", err)
	}

	s.logger.Info("dns provider created",
		zap.Int64("id", created.ID),
		zap.String("name", created.Name),
		zap.String("type", created.ProviderType),
	)
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.DNSProvider, error) {
	p, err := s.store.GetByID(ctx, id)
	if errors.Is(err, postgres.ErrDNSProviderNotFound) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Service) List(ctx context.Context) ([]postgres.DNSProvider, error) {
	return s.store.List(ctx)
}

type UpdateInput struct {
	ID           int64
	Name         string
	ProviderType string
	Config       []byte
	Credentials  []byte
	Notes        *string
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*postgres.DNSProvider, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("provider name required")
	}
	if !KnownProviderTypes[in.ProviderType] {
		return nil, ErrInvalidProviderType
	}

	existing, err := s.store.GetByID(ctx, in.ID)
	if errors.Is(err, postgres.ErrDNSProviderNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get dns provider: %w", err)
	}

	if len(in.Config) > 0 {
		existing.Config = in.Config
	}
	if len(in.Credentials) > 0 {
		existing.Credentials = in.Credentials
	}
	existing.Name = in.Name
	existing.ProviderType = in.ProviderType
	existing.Notes = in.Notes

	if err := s.store.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update dns provider: %w", err)
	}

	s.logger.Info("dns provider updated", zap.Int64("id", in.ID))
	return s.store.GetByID(ctx, in.ID)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.store.SoftDelete(ctx, id)
	if errors.Is(err, postgres.ErrDNSProviderNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, postgres.ErrDNSProviderHasDependents) {
		return ErrHasDependents
	}
	if err != nil {
		return fmt.Errorf("delete dns provider: %w", err)
	}
	s.logger.Info("dns provider deleted", zap.Int64("id", id))
	return nil
}

// SupportedTypes returns the list of known provider type strings for use in API
// responses / frontend dropdowns.
func SupportedTypes() []string {
	types := make([]string, 0, len(KnownProviderTypes))
	for t := range KnownProviderTypes {
		types = append(types, t)
	}
	return types
}
