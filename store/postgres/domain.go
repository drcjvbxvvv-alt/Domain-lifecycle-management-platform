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

	// Asset: Registration & Provider binding
	TLD                  *string `db:"tld"`
	RegistrarAccountID   *int64  `db:"registrar_account_id"`
	DNSProviderID        *int64  `db:"dns_provider_id"`

	// Asset: Dates & Expiry
	RegistrationDate *time.Time `db:"registration_date"`
	ExpiryDate       *time.Time `db:"expiry_date"`
	AutoRenew        bool       `db:"auto_renew"`
	GraceEndDate     *time.Time `db:"grace_end_date"`
	ExpiryStatus     *string    `db:"expiry_status"`

	// Asset: Status flags
	TransferLock bool `db:"transfer_lock"`
	Hold         bool `db:"hold"`

	// Asset: Transfer tracking
	TransferStatus           *string    `db:"transfer_status"`
	TransferGainingRegistrar *string    `db:"transfer_gaining_registrar"`
	TransferRequestedAt      *time.Time `db:"transfer_requested_at"`
	TransferCompletedAt      *time.Time `db:"transfer_completed_at"`
	LastTransferAt           *time.Time `db:"last_transfer_at"`
	LastRenewedAt            *time.Time `db:"last_renewed_at"`

	// Asset: DNS infrastructure
	Nameservers    json.RawMessage `db:"nameservers"`
	DNSSECEnabled  bool            `db:"dnssec_enabled"`

	// Asset: WHOIS & Contacts
	WhoisPrivacy       bool            `db:"whois_privacy"`
	RegistrantContact  json.RawMessage `db:"registrant_contact"`
	AdminContact       json.RawMessage `db:"admin_contact"`
	TechContact        json.RawMessage `db:"tech_contact"`

	// Asset: Financial
	AnnualCost    *float64 `db:"annual_cost"`
	Currency      *string  `db:"currency"`
	PurchasePrice *float64 `db:"purchase_price"`
	FeeFixed      bool     `db:"fee_fixed"`

	// Asset: Metadata
	Purpose  *string         `db:"purpose"`
	Notes    *string         `db:"notes"`
	Metadata json.RawMessage `db:"metadata"`

	// Timestamps
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at"`
}

var ErrDomainNotFound = errors.New("domain not found")

type DomainStore struct {
	db *sqlx.DB
}

func NewDomainStore(db *sqlx.DB) *DomainStore {
	return &DomainStore{db: db}
}

const domainColumns = `id, uuid, project_id, fqdn, lifecycle_state, owner_user_id,
	tld, registrar_account_id, dns_provider_id,
	registration_date, expiry_date, auto_renew, grace_end_date, expiry_status,
	transfer_lock, hold,
	transfer_status, transfer_gaining_registrar, transfer_requested_at, transfer_completed_at, last_transfer_at, last_renewed_at,
	nameservers, dnssec_enabled,
	whois_privacy, registrant_contact, admin_contact, tech_contact,
	annual_cost, currency, purchase_price, fee_fixed,
	purpose, notes, metadata,
	created_at, updated_at, deleted_at`

// Create inserts a new domain in the initial "requested" state.
// This is the documented exception to the Transition() rule: there is no
// nil → requested edge, so the INSERT sets lifecycle_state directly.
func (s *DomainStore) Create(ctx context.Context, d *Domain) (*Domain, error) {
	var out Domain
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domains (
			project_id, fqdn, lifecycle_state, owner_user_id,
			tld, registrar_account_id, dns_provider_id,
			registration_date, expiry_date, auto_renew, grace_end_date,
			transfer_lock, hold, nameservers, dnssec_enabled,
			whois_privacy, registrant_contact, admin_contact, tech_contact,
			annual_cost, currency, purchase_price, fee_fixed,
			purpose, notes, metadata
		) VALUES (
			$1, $2, 'requested', $3,
			$4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16, $17, $18,
			$19, $20, $21, $22,
			$23, $24, $25
		) RETURNING `+domainColumns,
		d.ProjectID, d.FQDN, d.OwnerUserID,
		d.TLD, d.RegistrarAccountID, d.DNSProviderID,
		d.RegistrationDate, d.ExpiryDate, d.AutoRenew, d.GraceEndDate,
		d.TransferLock, d.Hold, d.Nameservers, d.DNSSECEnabled,
		d.WhoisPrivacy, d.RegistrantContact, d.AdminContact, d.TechContact,
		d.AnnualCost, d.Currency, d.PurchasePrice, d.FeeFixed,
		d.Purpose, d.Notes, d.Metadata,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create domain: %w", err)
	}
	return &out, nil
}

func (s *DomainStore) GetByID(ctx context.Context, id int64) (*Domain, error) {
	var d Domain
	err := s.db.GetContext(ctx, &d,
		`SELECT `+domainColumns+` FROM domains WHERE id = $1 AND deleted_at IS NULL`, id)
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
		`SELECT `+domainColumns+`
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
		`SELECT `+domainColumns+`
		 FROM domains
		 WHERE project_id = $1 AND lifecycle_state = 'active' AND deleted_at IS NULL
		 ORDER BY fqdn ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list active domains by project: %w", err)
	}
	return domains, nil
}

// UpdateAssetFields updates the domain's asset-related columns.
// Does NOT touch lifecycle_state (that goes through LifecycleStore.TransitionTx).
func (s *DomainStore) UpdateAssetFields(ctx context.Context, d *Domain) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE domains SET
			tld = $2, registrar_account_id = $3, dns_provider_id = $4,
			registration_date = $5, expiry_date = $6, auto_renew = $7, grace_end_date = $8,
			transfer_lock = $9, hold = $10,
			nameservers = $11, dnssec_enabled = $12,
			whois_privacy = $13, registrant_contact = $14, admin_contact = $15, tech_contact = $16,
			annual_cost = $17, currency = $18, purchase_price = $19, fee_fixed = $20,
			purpose = $21, notes = $22, metadata = $23,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		d.ID,
		d.TLD, d.RegistrarAccountID, d.DNSProviderID,
		d.RegistrationDate, d.ExpiryDate, d.AutoRenew, d.GraceEndDate,
		d.TransferLock, d.Hold,
		d.Nameservers, d.DNSSECEnabled,
		d.WhoisPrivacy, d.RegistrantContact, d.AdminContact, d.TechContact,
		d.AnnualCost, d.Currency, d.PurchasePrice, d.FeeFixed,
		d.Purpose, d.Notes, d.Metadata,
	)
	if err != nil {
		return fmt.Errorf("update domain asset fields: %w", err)
	}
	return nil
}

// UpdateExpiryStatus sets the computed expiry_status field.
func (s *DomainStore) UpdateExpiryStatus(ctx context.Context, domainID int64, status *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE domains SET expiry_status = $2, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		domainID, status)
	if err != nil {
		return fmt.Errorf("update expiry status: %w", err)
	}
	return nil
}

// UpdateTransferStatus updates transfer tracking fields.
func (s *DomainStore) UpdateTransferStatus(ctx context.Context, domainID int64, status *string, gainingRegistrar *string, requestedAt, completedAt *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE domains SET
			transfer_status = $2, transfer_gaining_registrar = $3,
			transfer_requested_at = $4, transfer_completed_at = $5,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		domainID, status, gainingRegistrar, requestedAt, completedAt)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	return nil
}

// ListExpiring returns domains expiring within the given number of days.
func (s *DomainStore) ListExpiring(ctx context.Context, days int) ([]Domain, error) {
	var domains []Domain
	err := s.db.SelectContext(ctx, &domains,
		`SELECT `+domainColumns+`
		 FROM domains
		 WHERE deleted_at IS NULL
		   AND expiry_date IS NOT NULL
		   AND expiry_date <= CURRENT_DATE + $1 * INTERVAL '1 day'
		   AND lifecycle_state NOT IN ('retired')
		 ORDER BY expiry_date ASC`, days)
	if err != nil {
		return nil, fmt.Errorf("list expiring domains: %w", err)
	}
	return domains, nil
}
