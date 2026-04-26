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

// ── Sentinel errors ────────────────────────────────────────────────────────────

var (
	ErrCDNProviderNotFound        = errors.New("cdn provider not found")
	ErrCDNAccountNotFound         = errors.New("cdn account not found")
	ErrCDNProviderHasDependents   = errors.New("cdn provider has dependent accounts or domains")
	ErrCDNAccountHasDependents    = errors.New("cdn account is assigned to one or more domains")
	ErrCDNProviderDuplicate       = errors.New("cdn provider with this type and name already exists")
	ErrCDNAccountDuplicate        = errors.New("cdn account name already exists for this provider")
)

// ── Model structs ──────────────────────────────────────────────────────────────

// CDNProvider maps to the cdn_providers table.
type CDNProvider struct {
	ID           int64          `db:"id"`
	UUID         string         `db:"uuid"`
	Name         string         `db:"name"`
	ProviderType string         `db:"provider_type"`
	Description  *string        `db:"description"`
	CreatedAt    time.Time      `db:"created_at"`
	UpdatedAt    time.Time      `db:"updated_at"`
	DeletedAt    *time.Time     `db:"deleted_at"`
}

// CDNAccount maps to the cdn_accounts table.
type CDNAccount struct {
	ID            int64           `db:"id"`
	UUID          string          `db:"uuid"`
	CDNProviderID int64           `db:"cdn_provider_id"`
	AccountName   string          `db:"account_name"`
	Credentials   json.RawMessage `db:"credentials"`
	Notes         *string         `db:"notes"`
	Enabled       bool            `db:"enabled"`
	CreatedBy     *int64          `db:"created_by"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
	DeletedAt     *time.Time      `db:"deleted_at"`
}

// ── Store ──────────────────────────────────────────────────────────────────────

type CDNStore struct {
	db *sqlx.DB
}

func NewCDNStore(db *sqlx.DB) *CDNStore {
	return &CDNStore{db: db}
}

// ── CDN Provider CRUD ──────────────────────────────────────────────────────────

const providerCols = `id, uuid, name, provider_type, description, created_at, updated_at, deleted_at`

func (s *CDNStore) CreateProvider(ctx context.Context, p *CDNProvider) (*CDNProvider, error) {
	var out CDNProvider
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO cdn_providers (name, provider_type, description)
		 VALUES ($1, $2, $3)
		 RETURNING `+providerCols,
		p.Name, p.ProviderType, p.Description,
	).StructScan(&out)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCDNProviderDuplicate
		}
		return nil, fmt.Errorf("create cdn provider: %w", err)
	}
	return &out, nil
}

func (s *CDNStore) GetProviderByID(ctx context.Context, id int64) (*CDNProvider, error) {
	var p CDNProvider
	err := s.db.GetContext(ctx, &p,
		`SELECT `+providerCols+` FROM cdn_providers WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNProviderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cdn provider: %w", err)
	}
	return &p, nil
}

func (s *CDNStore) ListProviders(ctx context.Context) ([]CDNProvider, error) {
	var providers []CDNProvider
	err := s.db.SelectContext(ctx, &providers,
		`SELECT `+providerCols+` FROM cdn_providers WHERE deleted_at IS NULL ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list cdn providers: %w", err)
	}
	return providers, nil
}

func (s *CDNStore) UpdateProvider(ctx context.Context, p *CDNProvider) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE cdn_providers
		 SET name = $2, provider_type = $3, description = $4, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		p.ID, p.Name, p.ProviderType, p.Description)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrCDNProviderDuplicate
		}
		return fmt.Errorf("update cdn provider: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrCDNProviderNotFound
	}
	return nil
}

// SoftDeleteProvider soft-deletes a cdn_provider.
// Returns ErrCDNProviderHasDependents if it still has active accounts.
func (s *CDNStore) SoftDeleteProvider(ctx context.Context, id int64) error {
	var accountCount int64
	if err := s.db.GetContext(ctx, &accountCount,
		`SELECT COUNT(*) FROM cdn_accounts WHERE cdn_provider_id = $1 AND deleted_at IS NULL`, id); err != nil {
		return fmt.Errorf("check cdn provider dependents: %w", err)
	}
	if accountCount > 0 {
		return ErrCDNProviderHasDependents
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE cdn_providers SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete cdn provider: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrCDNProviderNotFound
	}
	return nil
}

// CountAccountsByProvider returns the number of active accounts under a provider.
func (s *CDNStore) CountAccountsByProvider(ctx context.Context, providerID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM cdn_accounts WHERE cdn_provider_id = $1 AND deleted_at IS NULL`, providerID)
	if err != nil {
		return 0, fmt.Errorf("count cdn accounts by provider: %w", err)
	}
	return count, nil
}

// ── CDN Account CRUD ───────────────────────────────────────────────────────────

const accountCols = `id, uuid, cdn_provider_id, account_name, credentials, notes, enabled, created_by, created_at, updated_at, deleted_at`

func (s *CDNStore) CreateAccount(ctx context.Context, a *CDNAccount) (*CDNAccount, error) {
	var out CDNAccount
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO cdn_accounts (cdn_provider_id, account_name, credentials, notes, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+accountCols,
		a.CDNProviderID, a.AccountName, a.Credentials, a.Notes, a.Enabled, a.CreatedBy,
	).StructScan(&out)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCDNAccountDuplicate
		}
		return nil, fmt.Errorf("create cdn account: %w", err)
	}
	return &out, nil
}

func (s *CDNStore) GetAccountByID(ctx context.Context, id int64) (*CDNAccount, error) {
	var a CDNAccount
	err := s.db.GetContext(ctx, &a,
		`SELECT `+accountCols+` FROM cdn_accounts WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cdn account: %w", err)
	}
	return &a, nil
}

func (s *CDNStore) ListAccountsByProvider(ctx context.Context, providerID int64) ([]CDNAccount, error) {
	var accounts []CDNAccount
	err := s.db.SelectContext(ctx, &accounts,
		`SELECT `+accountCols+`
		 FROM cdn_accounts
		 WHERE cdn_provider_id = $1 AND deleted_at IS NULL
		 ORDER BY account_name ASC`, providerID)
	if err != nil {
		return nil, fmt.Errorf("list cdn accounts: %w", err)
	}
	return accounts, nil
}

// ListAllAccounts returns all active CDN accounts across all providers.
// Used by domain create/edit selects.
func (s *CDNStore) ListAllAccounts(ctx context.Context) ([]CDNAccount, error) {
	var accounts []CDNAccount
	err := s.db.SelectContext(ctx, &accounts,
		`SELECT `+accountCols+`
		 FROM cdn_accounts
		 WHERE deleted_at IS NULL AND enabled = true
		 ORDER BY cdn_provider_id ASC, account_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list all cdn accounts: %w", err)
	}
	return accounts, nil
}

func (s *CDNStore) UpdateAccount(ctx context.Context, a *CDNAccount) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE cdn_accounts
		 SET account_name = $2, credentials = $3, notes = $4, enabled = $5, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		a.ID, a.AccountName, a.Credentials, a.Notes, a.Enabled)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrCDNAccountDuplicate
		}
		return fmt.Errorf("update cdn account: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrCDNAccountNotFound
	}
	return nil
}

// SoftDeleteAccount soft-deletes a cdn_account.
// Returns ErrCDNAccountHasDependents if any domains still reference it.
func (s *CDNStore) SoftDeleteAccount(ctx context.Context, id int64) error {
	var domainCount int64
	if err := s.db.GetContext(ctx, &domainCount,
		`SELECT COUNT(*) FROM domains WHERE cdn_account_id = $1 AND deleted_at IS NULL`, id); err != nil {
		return fmt.Errorf("check cdn account dependents: %w", err)
	}
	if domainCount > 0 {
		return ErrCDNAccountHasDependents
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE cdn_accounts SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete cdn account: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrCDNAccountNotFound
	}
	return nil
}

// CountDomainsByAccount returns how many domains reference a specific CDN account.
func (s *CDNStore) CountDomainsByAccount(ctx context.Context, accountID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM domains WHERE cdn_account_id = $1 AND deleted_at IS NULL`, accountID)
	if err != nil {
		return 0, fmt.Errorf("count domains by cdn account: %w", err)
	}
	return count, nil
}

// ── helpers ────────────────────────────────────────────────────────────────────

// isUniqueViolation detects PostgreSQL unique constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pq driver wraps as *pq.Error; check string for portability across drivers.
	return containsString(err.Error(), "23505") || containsString(err.Error(), "unique constraint")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
