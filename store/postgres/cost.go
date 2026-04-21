package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type DomainFeeSchedule struct {
	ID              int64     `db:"id"`
	RegistrarID     int64     `db:"registrar_id"`
	TLD             string    `db:"tld"`
	RegistrationFee float64   `db:"registration_fee"`
	RenewalFee      float64   `db:"renewal_fee"`
	TransferFee     float64   `db:"transfer_fee"`
	PrivacyFee      float64   `db:"privacy_fee"`
	Currency        string    `db:"currency"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

type DomainCost struct {
	ID          int64      `db:"id"`
	DomainID    int64      `db:"domain_id"`
	CostType    string     `db:"cost_type"`
	Amount      float64    `db:"amount"`
	Currency    string     `db:"currency"`
	PeriodStart *time.Time `db:"period_start"`
	PeriodEnd   *time.Time `db:"period_end"`
	PaidAt      *time.Time `db:"paid_at"`
	Notes       *string    `db:"notes"`
	CreatedAt   time.Time  `db:"created_at"`
}

var (
	ErrFeeScheduleNotFound = errors.New("fee schedule not found")
)

type CostStore struct {
	db *sqlx.DB
}

func NewCostStore(db *sqlx.DB) *CostStore {
	return &CostStore{db: db}
}

// --- Fee Schedules ---

func (s *CostStore) CreateFeeSchedule(ctx context.Context, fs *DomainFeeSchedule) (*DomainFeeSchedule, error) {
	var out DomainFeeSchedule
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domain_fee_schedules (registrar_id, tld, registration_fee, renewal_fee, transfer_fee, privacy_fee, currency)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, registrar_id, tld, registration_fee, renewal_fee, transfer_fee, privacy_fee, currency, created_at, updated_at`,
		fs.RegistrarID, fs.TLD, fs.RegistrationFee, fs.RenewalFee, fs.TransferFee, fs.PrivacyFee, fs.Currency,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create fee schedule: %w", err)
	}
	return &out, nil
}

func (s *CostStore) GetFeeScheduleByID(ctx context.Context, id int64) (*DomainFeeSchedule, error) {
	var fs DomainFeeSchedule
	err := s.db.GetContext(ctx, &fs,
		`SELECT id, registrar_id, tld, registration_fee, renewal_fee, transfer_fee, privacy_fee, currency, created_at, updated_at
		 FROM domain_fee_schedules WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeeScheduleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get fee schedule: %w", err)
	}
	return &fs, nil
}

// GetFeeSchedule looks up the fee for a given registrar and TLD combination.
func (s *CostStore) GetFeeSchedule(ctx context.Context, registrarID int64, tld string) (*DomainFeeSchedule, error) {
	var fs DomainFeeSchedule
	err := s.db.GetContext(ctx, &fs,
		`SELECT id, registrar_id, tld, registration_fee, renewal_fee, transfer_fee, privacy_fee, currency, created_at, updated_at
		 FROM domain_fee_schedules WHERE registrar_id = $1 AND tld = $2`, registrarID, tld)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeeScheduleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get fee schedule by registrar+tld: %w", err)
	}
	return &fs, nil
}

func (s *CostStore) ListFeeSchedules(ctx context.Context, registrarID *int64) ([]DomainFeeSchedule, error) {
	var schedules []DomainFeeSchedule
	var err error
	if registrarID != nil {
		err = s.db.SelectContext(ctx, &schedules,
			`SELECT id, registrar_id, tld, registration_fee, renewal_fee, transfer_fee, privacy_fee, currency, created_at, updated_at
			 FROM domain_fee_schedules WHERE registrar_id = $1 ORDER BY tld ASC`, *registrarID)
	} else {
		err = s.db.SelectContext(ctx, &schedules,
			`SELECT id, registrar_id, tld, registration_fee, renewal_fee, transfer_fee, privacy_fee, currency, created_at, updated_at
			 FROM domain_fee_schedules ORDER BY registrar_id, tld ASC`)
	}
	if err != nil {
		return nil, fmt.Errorf("list fee schedules: %w", err)
	}
	return schedules, nil
}

func (s *CostStore) UpdateFeeSchedule(ctx context.Context, fs *DomainFeeSchedule) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE domain_fee_schedules SET
			registration_fee = $2, renewal_fee = $3, transfer_fee = $4, privacy_fee = $5, currency = $6, updated_at = NOW()
		 WHERE id = $1`,
		fs.ID, fs.RegistrationFee, fs.RenewalFee, fs.TransferFee, fs.PrivacyFee, fs.Currency)
	if err != nil {
		return fmt.Errorf("update fee schedule: %w", err)
	}
	return nil
}

func (s *CostStore) DeleteFeeSchedule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM domain_fee_schedules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete fee schedule: %w", err)
	}
	return nil
}

// --- Domain Costs ---

func (s *CostStore) CreateCost(ctx context.Context, c *DomainCost) (*DomainCost, error) {
	var out DomainCost
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domain_costs (domain_id, cost_type, amount, currency, period_start, period_end, paid_at, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, domain_id, cost_type, amount, currency, period_start, period_end, paid_at, notes, created_at`,
		c.DomainID, c.CostType, c.Amount, c.Currency, c.PeriodStart, c.PeriodEnd, c.PaidAt, c.Notes,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create domain cost: %w", err)
	}
	return &out, nil
}

func (s *CostStore) ListCostsByDomain(ctx context.Context, domainID int64) ([]DomainCost, error) {
	var costs []DomainCost
	err := s.db.SelectContext(ctx, &costs,
		`SELECT id, domain_id, cost_type, amount, currency, period_start, period_end, paid_at, notes, created_at
		 FROM domain_costs WHERE domain_id = $1 ORDER BY created_at DESC`, domainID)
	if err != nil {
		return nil, fmt.Errorf("list domain costs: %w", err)
	}
	return costs, nil
}

// CostSummary holds aggregated cost data.
type CostSummary struct {
	GroupKey   string  `db:"group_key"`
	TotalCost float64 `db:"total_cost"`
	Currency  string  `db:"currency"`
	Count     int64   `db:"count"`
}

// GetCostSummaryByRegistrar returns total annual_cost grouped by registrar.
func (s *CostStore) GetCostSummaryByRegistrar(ctx context.Context) ([]CostSummary, error) {
	var summaries []CostSummary
	err := s.db.SelectContext(ctx, &summaries,
		`SELECT r.name AS group_key, COALESCE(SUM(d.annual_cost), 0) AS total_cost,
		        COALESCE(d.currency, 'USD') AS currency, COUNT(d.id) AS count
		 FROM domains d
		 JOIN registrar_accounts ra ON d.registrar_account_id = ra.id
		 JOIN registrars r ON ra.registrar_id = r.id
		 WHERE d.deleted_at IS NULL AND d.annual_cost IS NOT NULL
		 GROUP BY r.name, d.currency
		 ORDER BY total_cost DESC`)
	if err != nil {
		return nil, fmt.Errorf("cost summary by registrar: %w", err)
	}
	return summaries, nil
}

// GetCostSummaryByTLD returns total annual_cost grouped by TLD.
func (s *CostStore) GetCostSummaryByTLD(ctx context.Context) ([]CostSummary, error) {
	var summaries []CostSummary
	err := s.db.SelectContext(ctx, &summaries,
		`SELECT COALESCE(tld, 'unknown') AS group_key, COALESCE(SUM(annual_cost), 0) AS total_cost,
		        COALESCE(currency, 'USD') AS currency, COUNT(id) AS count
		 FROM domains
		 WHERE deleted_at IS NULL AND annual_cost IS NOT NULL
		 GROUP BY tld, currency
		 ORDER BY total_cost DESC`)
	if err != nil {
		return nil, fmt.Errorf("cost summary by tld: %w", err)
	}
	return summaries, nil
}
