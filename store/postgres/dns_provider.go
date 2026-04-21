package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type DNSProvider struct {
	ID           int64           `db:"id"`
	UUID         string          `db:"uuid"`
	Name         string          `db:"name"`
	ProviderType string          `db:"provider_type"`
	Config       json.RawMessage `db:"config"`
	Credentials  json.RawMessage `db:"credentials"`
	Notes        *string         `db:"notes"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
	DeletedAt    *time.Time      `db:"deleted_at"`
}

var (
	ErrDNSProviderNotFound      = errors.New("dns provider not found")
	ErrDNSProviderHasDependents = errors.New("dns provider has dependent domains")
)

type DNSProviderStore struct {
	db *sqlx.DB
}

func NewDNSProviderStore(db *sqlx.DB) *DNSProviderStore {
	return &DNSProviderStore{db: db}
}

func (s *DNSProviderStore) Create(ctx context.Context, p *DNSProvider) (*DNSProvider, error) {
	var out DNSProvider
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO dns_providers (name, provider_type, config, credentials, notes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, uuid, name, provider_type, config, credentials, notes, created_at, updated_at, deleted_at`,
		p.Name, p.ProviderType, p.Config, p.Credentials, p.Notes,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create dns provider: %w", err)
	}
	return &out, nil
}

func (s *DNSProviderStore) GetByID(ctx context.Context, id int64) (*DNSProvider, error) {
	var p DNSProvider
	err := s.db.GetContext(ctx, &p,
		`SELECT id, uuid, name, provider_type, config, credentials, notes, created_at, updated_at, deleted_at
		 FROM dns_providers WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDNSProviderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get dns provider: %w", err)
	}
	return &p, nil
}

func (s *DNSProviderStore) List(ctx context.Context) ([]DNSProvider, error) {
	var providers []DNSProvider
	err := s.db.SelectContext(ctx, &providers,
		`SELECT id, uuid, name, provider_type, config, credentials, notes, created_at, updated_at, deleted_at
		 FROM dns_providers WHERE deleted_at IS NULL ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list dns providers: %w", err)
	}
	return providers, nil
}

func (s *DNSProviderStore) Update(ctx context.Context, p *DNSProvider) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE dns_providers SET name = $2, provider_type = $3, config = $4, credentials = $5, notes = $6, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		p.ID, p.Name, p.ProviderType, p.Config, p.Credentials, p.Notes)
	if err != nil {
		return fmt.Errorf("update dns provider: %w", err)
	}
	return nil
}

func (s *DNSProviderStore) SoftDelete(ctx context.Context, id int64) error {
	var domainCount int64
	err := s.db.GetContext(ctx, &domainCount,
		`SELECT COUNT(*) FROM domains WHERE dns_provider_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("check dns provider dependents: %w", err)
	}
	if domainCount > 0 {
		return ErrDNSProviderHasDependents
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE dns_providers SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete dns provider: %w", err)
	}
	return nil
}

// CountDomains returns how many domains use this provider.
func (s *DNSProviderStore) CountDomains(ctx context.Context, providerID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM domains WHERE dns_provider_id = $1 AND deleted_at IS NULL`, providerID)
	if err != nil {
		return 0, fmt.Errorf("count domains by dns provider: %w", err)
	}
	return count, nil
}
