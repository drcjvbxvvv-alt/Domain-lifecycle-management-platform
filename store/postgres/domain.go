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

// Domain maps to the domains table row.
type Domain struct {
	ID             int64      `db:"id"`
	UUID           string     `db:"uuid"`
	ProjectID      int64      `db:"project_id"`
	FQDN           string     `db:"fqdn"`
	LifecycleState string     `db:"lifecycle_state"`
	OwnerUserID    *int64     `db:"owner_user_id"`
	DNSProvider    *string    `db:"dns_provider"`
	DNSZone        *string    `db:"dns_zone"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
	DeletedAt      *time.Time `db:"deleted_at"`
}

var ErrDomainNotFound = errors.New("domain not found")

type DomainStore struct {
	db *sqlx.DB
}

func NewDomainStore(db *sqlx.DB) *DomainStore {
	return &DomainStore{db: db}
}

// Create inserts a new domain in the initial "requested" state.
// This is the documented exception to the Transition() rule: there is no
// nil → requested edge, so the INSERT sets lifecycle_state directly.
func (s *DomainStore) Create(ctx context.Context, d *Domain) (*Domain, error) {
	var out Domain
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domains (project_id, fqdn, lifecycle_state, owner_user_id, dns_provider, dns_zone)
		 VALUES ($1, $2, 'requested', $3, $4, $5)
		 RETURNING id, uuid, project_id, fqdn, lifecycle_state, owner_user_id, dns_provider, dns_zone, created_at, updated_at, deleted_at`,
		d.ProjectID, d.FQDN, d.OwnerUserID, d.DNSProvider, d.DNSZone).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create domain: %w", err)
	}
	return &out, nil
}

func (s *DomainStore) GetByID(ctx context.Context, id int64) (*Domain, error) {
	var d Domain
	err := s.db.GetContext(ctx, &d,
		`SELECT id, uuid, project_id, fqdn, lifecycle_state, owner_user_id, dns_provider, dns_zone, created_at, updated_at, deleted_at
		 FROM domains WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain by id: %w", err)
	}
	return &d, nil
}

// ListByProject returns domains for a project with cursor pagination.
func (s *DomainStore) ListByProject(ctx context.Context, projectID int64, cursor int64, limit int) ([]Domain, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var domains []Domain
	err := s.db.SelectContext(ctx, &domains,
		`SELECT id, uuid, project_id, fqdn, lifecycle_state, owner_user_id, dns_provider, dns_zone, created_at, updated_at, deleted_at
		 FROM domains
		 WHERE project_id = $1 AND deleted_at IS NULL AND id > $2
		 ORDER BY id ASC
		 LIMIT $3`, projectID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list domains by project: %w", err)
	}
	return domains, nil
}

// CountByProject returns the total non-deleted domains for a project.
func (s *DomainStore) CountByProject(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM domains WHERE project_id = $1 AND deleted_at IS NULL`, projectID)
	if err != nil {
		return 0, fmt.Errorf("count domains: %w", err)
	}
	return count, nil
}

// GetVariables returns the domain-specific variables as a map.
// Returns an empty map (not an error) if no variables are set.
func (s *DomainStore) GetVariables(ctx context.Context, domainID int64) (map[string]any, error) {
	var raw []byte
	err := s.db.GetContext(ctx, &raw,
		`SELECT variables FROM domain_variables WHERE domain_id = $1`, domainID)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get domain variables %d: %w", domainID, err)
	}
	var vars map[string]any
	if err := json.Unmarshal(raw, &vars); err != nil {
		return nil, fmt.Errorf("unmarshal domain variables: %w", err)
	}
	return vars, nil
}

// ListActiveByProject returns all active domains for a project.
func (s *DomainStore) ListActiveByProject(ctx context.Context, projectID int64) ([]Domain, error) {
	var domains []Domain
	err := s.db.SelectContext(ctx, &domains,
		`SELECT id, uuid, project_id, fqdn, lifecycle_state, owner_user_id, dns_provider, dns_zone, created_at, updated_at, deleted_at
		 FROM domains
		 WHERE project_id = $1 AND lifecycle_state = 'active' AND deleted_at IS NULL
		 ORDER BY fqdn ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list active domains by project: %w", err)
	}
	return domains, nil
}
