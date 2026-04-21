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

type Registrar struct {
	ID           int64           `db:"id"`
	UUID         string          `db:"uuid"`
	Name         string          `db:"name"`
	URL          *string         `db:"url"`
	APIType      *string         `db:"api_type"`
	Capabilities json.RawMessage `db:"capabilities"`
	Notes        *string         `db:"notes"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
	DeletedAt    *time.Time      `db:"deleted_at"`
}

type RegistrarAccount struct {
	ID           int64           `db:"id"`
	UUID         string          `db:"uuid"`
	RegistrarID  int64           `db:"registrar_id"`
	AccountName  string          `db:"account_name"`
	OwnerUserID  *int64          `db:"owner_user_id"`
	Credentials  json.RawMessage `db:"credentials"`
	IsDefault    bool            `db:"is_default"`
	Notes        *string         `db:"notes"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
	DeletedAt    *time.Time      `db:"deleted_at"`
}

var (
	ErrRegistrarNotFound        = errors.New("registrar not found")
	ErrRegistrarAccountNotFound = errors.New("registrar account not found")
	ErrRegistrarHasDependents   = errors.New("registrar has dependent accounts or domains")
)

type RegistrarStore struct {
	db *sqlx.DB
}

func NewRegistrarStore(db *sqlx.DB) *RegistrarStore {
	return &RegistrarStore{db: db}
}

func (s *RegistrarStore) Create(ctx context.Context, r *Registrar) (*Registrar, error) {
	var out Registrar
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO registrars (name, url, api_type, capabilities, notes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, uuid, name, url, api_type, capabilities, notes, created_at, updated_at, deleted_at`,
		r.Name, r.URL, r.APIType, r.Capabilities, r.Notes,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create registrar: %w", err)
	}
	return &out, nil
}

func (s *RegistrarStore) GetByID(ctx context.Context, id int64) (*Registrar, error) {
	var r Registrar
	err := s.db.GetContext(ctx, &r,
		`SELECT id, uuid, name, url, api_type, capabilities, notes, created_at, updated_at, deleted_at
		 FROM registrars WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegistrarNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get registrar: %w", err)
	}
	return &r, nil
}

func (s *RegistrarStore) List(ctx context.Context) ([]Registrar, error) {
	var registrars []Registrar
	err := s.db.SelectContext(ctx, &registrars,
		`SELECT id, uuid, name, url, api_type, capabilities, notes, created_at, updated_at, deleted_at
		 FROM registrars WHERE deleted_at IS NULL ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list registrars: %w", err)
	}
	return registrars, nil
}

func (s *RegistrarStore) Update(ctx context.Context, r *Registrar) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE registrars SET name = $2, url = $3, api_type = $4, capabilities = $5, notes = $6, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		r.ID, r.Name, r.URL, r.APIType, r.Capabilities, r.Notes)
	if err != nil {
		return fmt.Errorf("update registrar: %w", err)
	}
	return nil
}

func (s *RegistrarStore) SoftDelete(ctx context.Context, id int64) error {
	// Check for dependent accounts
	var accountCount int64
	err := s.db.GetContext(ctx, &accountCount,
		`SELECT COUNT(*) FROM registrar_accounts WHERE registrar_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("check registrar dependents: %w", err)
	}
	if accountCount > 0 {
		return ErrRegistrarHasDependents
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE registrars SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete registrar: %w", err)
	}
	return nil
}

// --- Registrar Accounts ---

func (s *RegistrarStore) CreateAccount(ctx context.Context, a *RegistrarAccount) (*RegistrarAccount, error) {
	var out RegistrarAccount
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO registrar_accounts (registrar_id, account_name, owner_user_id, credentials, is_default, notes)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, uuid, registrar_id, account_name, owner_user_id, credentials, is_default, notes, created_at, updated_at, deleted_at`,
		a.RegistrarID, a.AccountName, a.OwnerUserID, a.Credentials, a.IsDefault, a.Notes,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create registrar account: %w", err)
	}
	return &out, nil
}

func (s *RegistrarStore) GetAccountByID(ctx context.Context, id int64) (*RegistrarAccount, error) {
	var a RegistrarAccount
	err := s.db.GetContext(ctx, &a,
		`SELECT id, uuid, registrar_id, account_name, owner_user_id, credentials, is_default, notes, created_at, updated_at, deleted_at
		 FROM registrar_accounts WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegistrarAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get registrar account: %w", err)
	}
	return &a, nil
}

func (s *RegistrarStore) ListAccountsByRegistrar(ctx context.Context, registrarID int64) ([]RegistrarAccount, error) {
	var accounts []RegistrarAccount
	err := s.db.SelectContext(ctx, &accounts,
		`SELECT id, uuid, registrar_id, account_name, owner_user_id, credentials, is_default, notes, created_at, updated_at, deleted_at
		 FROM registrar_accounts WHERE registrar_id = $1 AND deleted_at IS NULL ORDER BY account_name ASC`, registrarID)
	if err != nil {
		return nil, fmt.Errorf("list registrar accounts: %w", err)
	}
	return accounts, nil
}

func (s *RegistrarStore) UpdateAccount(ctx context.Context, a *RegistrarAccount) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE registrar_accounts SET account_name = $2, owner_user_id = $3, credentials = $4, is_default = $5, notes = $6, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		a.ID, a.AccountName, a.OwnerUserID, a.Credentials, a.IsDefault, a.Notes)
	if err != nil {
		return fmt.Errorf("update registrar account: %w", err)
	}
	return nil
}

func (s *RegistrarStore) SoftDeleteAccount(ctx context.Context, id int64) error {
	// Check for dependent domains
	var domainCount int64
	err := s.db.GetContext(ctx, &domainCount,
		`SELECT COUNT(*) FROM domains WHERE registrar_account_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("check account dependents: %w", err)
	}
	if domainCount > 0 {
		return ErrRegistrarHasDependents
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE registrar_accounts SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete registrar account: %w", err)
	}
	return nil
}

// CountDomainsByRegistrar returns how many domains use accounts under this registrar.
func (s *RegistrarStore) CountDomainsByRegistrar(ctx context.Context, registrarID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM domains d
		 JOIN registrar_accounts ra ON d.registrar_account_id = ra.id
		 WHERE ra.registrar_id = $1 AND d.deleted_at IS NULL`, registrarID)
	if err != nil {
		return 0, fmt.Errorf("count domains by registrar: %w", err)
	}
	return count, nil
}
