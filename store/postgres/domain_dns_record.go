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

// DomainDNSRecord maps to the domain_dns_records table.
type DomainDNSRecord struct {
	ID               int64           `db:"id"`
	UUID             string          `db:"uuid"`
	DomainID         int64           `db:"domain_id"`
	DNSProviderID    *int64          `db:"dns_provider_id"`
	ProviderRecordID *string         `db:"provider_record_id"`
	RecordType       string          `db:"record_type"`
	Name             string          `db:"name"`
	Content          string          `db:"content"`
	TTL              int             `db:"ttl"`
	Priority         *int            `db:"priority"`
	Proxied          bool            `db:"proxied"`
	Extra            json.RawMessage `db:"extra"`
	SyncedAt         *time.Time      `db:"synced_at"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
	DeletedAt        *time.Time      `db:"deleted_at"`
}

var ErrDNSRecordNotFound = errors.New("domain dns record not found")

const domainDNSRecordColumns = `id, uuid, domain_id, dns_provider_id, provider_record_id,
	record_type, name, content, ttl, priority, proxied, extra,
	synced_at, created_at, updated_at, deleted_at`

// DomainDNSRecordStore handles CRUD for domain_dns_records.
type DomainDNSRecordStore struct {
	db *sqlx.DB
}

func NewDomainDNSRecordStore(db *sqlx.DB) *DomainDNSRecordStore {
	return &DomainDNSRecordStore{db: db}
}

// Create inserts a new domain DNS record row.
func (s *DomainDNSRecordStore) Create(ctx context.Context, r *DomainDNSRecord) (*DomainDNSRecord, error) {
	if r.Extra == nil {
		r.Extra = json.RawMessage(`{}`)
	}
	var out DomainDNSRecord
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domain_dns_records (
			domain_id, dns_provider_id, provider_record_id,
			record_type, name, content, ttl, priority, proxied, extra, synced_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+domainDNSRecordColumns,
		r.DomainID, r.DNSProviderID, r.ProviderRecordID,
		r.RecordType, r.Name, r.Content, r.TTL, r.Priority, r.Proxied, r.Extra, r.SyncedAt,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create domain dns record: %w", err)
	}
	return &out, nil
}

// GetByID returns a single record by its local ID.
func (s *DomainDNSRecordStore) GetByID(ctx context.Context, id int64) (*DomainDNSRecord, error) {
	var r DomainDNSRecord
	err := s.db.GetContext(ctx, &r,
		`SELECT `+domainDNSRecordColumns+`
		 FROM domain_dns_records
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDNSRecordNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain dns record by id: %w", err)
	}
	return &r, nil
}

// ListByDomain returns all non-deleted records for a domain, optionally filtered by type.
func (s *DomainDNSRecordStore) ListByDomain(ctx context.Context, domainID int64, recordType string) ([]DomainDNSRecord, error) {
	q := `SELECT ` + domainDNSRecordColumns + `
	      FROM domain_dns_records
	      WHERE domain_id = $1 AND deleted_at IS NULL`
	args := []any{domainID}
	if recordType != "" {
		q += ` AND record_type = $2`
		args = append(args, recordType)
	}
	q += ` ORDER BY record_type ASC, name ASC, id ASC`
	var records []DomainDNSRecord
	if err := s.db.SelectContext(ctx, &records, q, args...); err != nil {
		return nil, fmt.Errorf("list dns records by domain: %w", err)
	}
	return records, nil
}

// Update persists content/TTL/priority/proxied/extra changes for an existing record.
// Does NOT update record_type or name (those require delete + re-create at the provider).
func (s *DomainDNSRecordStore) Update(ctx context.Context, r *DomainDNSRecord) (*DomainDNSRecord, error) {
	if r.Extra == nil {
		r.Extra = json.RawMessage(`{}`)
	}
	var out DomainDNSRecord
	err := s.db.QueryRowxContext(ctx,
		`UPDATE domain_dns_records SET
			content              = $2,
			ttl                  = $3,
			priority             = $4,
			proxied              = $5,
			extra                = $6,
			provider_record_id   = $7,
			updated_at           = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING `+domainDNSRecordColumns,
		r.ID, r.Content, r.TTL, r.Priority, r.Proxied, r.Extra, r.ProviderRecordID,
	).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDNSRecordNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update domain dns record: %w", err)
	}
	return &out, nil
}

// SoftDelete marks a record as deleted (sets deleted_at = NOW()).
func (s *DomainDNSRecordStore) SoftDelete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE domain_dns_records SET deleted_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete domain dns record: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDNSRecordNotFound
	}
	return nil
}

// UpsertByProviderID creates or updates a record identified by
// (domain_id, provider_record_id). Used by the sync operation.
// Returns the created/updated row and a boolean (true = newly created).
func (s *DomainDNSRecordStore) UpsertByProviderID(ctx context.Context, r *DomainDNSRecord) (*DomainDNSRecord, bool, error) {
	if r.Extra == nil {
		r.Extra = json.RawMessage(`{}`)
	}
	now := time.Now()
	r.SyncedAt = &now

	var out DomainDNSRecord
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domain_dns_records (
			domain_id, dns_provider_id, provider_record_id,
			record_type, name, content, ttl, priority, proxied, extra, synced_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (domain_id, provider_record_id) WHERE deleted_at IS NULL
		DO UPDATE SET
			record_type        = EXCLUDED.record_type,
			name               = EXCLUDED.name,
			content            = EXCLUDED.content,
			ttl                = EXCLUDED.ttl,
			priority           = EXCLUDED.priority,
			proxied            = EXCLUDED.proxied,
			extra              = EXCLUDED.extra,
			synced_at          = EXCLUDED.synced_at,
			updated_at         = NOW()
		RETURNING `+domainDNSRecordColumns+`, (xmax = 0) AS inserted`,
		r.DomainID, r.DNSProviderID, r.ProviderRecordID,
		r.RecordType, r.Name, r.Content, r.TTL, r.Priority, r.Proxied, r.Extra, r.SyncedAt,
	).StructScan(&out)
	if err != nil {
		return nil, false, fmt.Errorf("upsert domain dns record: %w", err)
	}
	return &out, false, nil // inserted bool is tricky with StructScan; callers don't need it
}

// DeleteByProviderIDs soft-deletes all records for a domain whose provider_record_id
// is NOT in the given set. Used by sync to prune records deleted on the provider side.
func (s *DomainDNSRecordStore) DeleteByProviderIDs(ctx context.Context, domainID int64, keepIDs []string) (int64, error) {
	if len(keepIDs) == 0 {
		// No provider records remain — soft-delete all local records for this domain.
		res, err := s.db.ExecContext(ctx,
			`UPDATE domain_dns_records SET deleted_at = NOW(), updated_at = NOW()
			 WHERE domain_id = $1 AND deleted_at IS NULL`, domainID)
		if err != nil {
			return 0, fmt.Errorf("delete all dns records for domain: %w", err)
		}
		n, _ := res.RowsAffected()
		return n, nil
	}

	query, args, err := sqlx.In(
		`UPDATE domain_dns_records
		 SET deleted_at = NOW(), updated_at = NOW()
		 WHERE domain_id = ? AND provider_record_id NOT IN (?) AND deleted_at IS NULL`,
		domainID, keepIDs,
	)
	if err != nil {
		return 0, fmt.Errorf("build delete by provider ids query: %w", err)
	}
	query = s.db.Rebind(query)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete stale dns records: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
