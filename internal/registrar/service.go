package registrar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	registrarprovider "domain-platform/pkg/provider/registrar"
	"domain-platform/store/postgres"
)

var (
	ErrNotFound         = errors.New("registrar not found")
	ErrAccountNotFound  = errors.New("registrar account not found")
	ErrDuplicateName    = errors.New("registrar name already exists")
	ErrHasDependents    = errors.New("registrar has dependent accounts or domains — detach first")

	// Provider-level errors surfaced by SyncAccount.
	// Defined here so callers (e.g. the HTTP handler) can check them
	// without importing pkg/provider/registrar directly.
	ErrCredentialsRejected  = errors.New("registrar API credentials rejected — check your Key and Secret")
	ErrCredentialsMissing   = errors.New("registrar account credentials are missing or invalid")
	ErrProviderNotSupported = errors.New("registrar api_type is not supported")
	ErrRateLimitExceeded    = errors.New("registrar API rate limit exceeded")
	// ErrAccessDenied means credentials are valid but the account lacks API permission.
	// Common for GoDaddy retail accounts (non-reseller) on the Production API.
	ErrAccessDenied = errors.New("registrar API access denied — account does not have API permission")
)

// domainDateUpdater is the subset of DomainStore used by SyncAccount.
// Defined as an interface so tests can inject a mock.
type domainDateUpdater interface {
	UpdateDomainDates(ctx context.Context, fqdn string, registrarAccountID int64,
		registrationDate *time.Time, expiryDate *time.Time, autoRenew bool) (bool, error)
}

type Service struct {
	store       *postgres.RegistrarStore
	domainStore domainDateUpdater
	logger      *zap.Logger
}

func NewService(store *postgres.RegistrarStore, domainStore domainDateUpdater, logger *zap.Logger) *Service {
	return &Service{store: store, domainStore: domainStore, logger: logger}
}

// ── Registrar ────────────────────────────────────────────────────────────────

type CreateInput struct {
	Name         string
	URL          *string
	APIType      *string
	Capabilities []byte // raw JSON; nil → "{}"
	Notes        *string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.Registrar, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("registrar name required")
	}
	caps := in.Capabilities
	if len(caps) == 0 {
		caps = []byte("{}")
	}

	r := &postgres.Registrar{
		Name:         in.Name,
		URL:          in.URL,
		APIType:      in.APIType,
		Capabilities: caps,
		Notes:        in.Notes,
	}

	created, err := s.store.Create(ctx, r)
	if err != nil {
		if strings.Contains(err.Error(), "uq_registrars_name") {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("create registrar: %w", err)
	}

	s.logger.Info("registrar created",
		zap.Int64("id", created.ID),
		zap.String("name", created.Name),
	)
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.Registrar, error) {
	r, err := s.store.GetByID(ctx, id)
	if errors.Is(err, postgres.ErrRegistrarNotFound) {
		return nil, ErrNotFound
	}
	return r, err
}

func (s *Service) List(ctx context.Context) ([]postgres.Registrar, error) {
	return s.store.List(ctx)
}

type UpdateInput struct {
	ID           int64
	Name         string
	URL          *string
	APIType      *string
	Capabilities []byte
	Notes        *string
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*postgres.Registrar, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("registrar name required")
	}

	// Verify exists first
	existing, err := s.store.GetByID(ctx, in.ID)
	if errors.Is(err, postgres.ErrRegistrarNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get registrar: %w", err)
	}

	caps := in.Capabilities
	if len(caps) == 0 {
		caps = existing.Capabilities
	}

	existing.Name = in.Name
	existing.URL = in.URL
	existing.APIType = in.APIType
	existing.Capabilities = caps
	existing.Notes = in.Notes

	if err := s.store.Update(ctx, existing); err != nil {
		if strings.Contains(err.Error(), "uq_registrars_name") {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("update registrar: %w", err)
	}

	s.logger.Info("registrar updated", zap.Int64("id", in.ID))
	return s.store.GetByID(ctx, in.ID)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.store.SoftDelete(ctx, id)
	if errors.Is(err, postgres.ErrRegistrarNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, postgres.ErrRegistrarHasDependents) {
		return ErrHasDependents
	}
	if err != nil {
		return fmt.Errorf("delete registrar: %w", err)
	}
	s.logger.Info("registrar deleted", zap.Int64("id", id))
	return nil
}

// ── Registrar Accounts ────────────────────────────────────────────────────────

type CreateAccountInput struct {
	RegistrarID int64
	AccountName string
	OwnerUserID *int64
	Credentials []byte // raw JSON; nil → "{}"
	IsDefault   bool
	Notes       *string
}

func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*postgres.RegistrarAccount, error) {
	if strings.TrimSpace(in.AccountName) == "" {
		return nil, fmt.Errorf("account name required")
	}

	// Verify parent registrar exists
	if _, err := s.store.GetByID(ctx, in.RegistrarID); err != nil {
		if errors.Is(err, postgres.ErrRegistrarNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("check registrar: %w", err)
	}

	creds := in.Credentials
	if len(creds) == 0 {
		creds = []byte("{}")
	}

	a := &postgres.RegistrarAccount{
		RegistrarID: in.RegistrarID,
		AccountName: in.AccountName,
		OwnerUserID: in.OwnerUserID,
		Credentials: creds,
		IsDefault:   in.IsDefault,
		Notes:       in.Notes,
	}

	created, err := s.store.CreateAccount(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("create registrar account: %w", err)
	}

	s.logger.Info("registrar account created",
		zap.Int64("id", created.ID),
		zap.Int64("registrar_id", in.RegistrarID),
		zap.String("account_name", created.AccountName),
	)
	return created, nil
}

func (s *Service) GetAccount(ctx context.Context, id int64) (*postgres.RegistrarAccount, error) {
	a, err := s.store.GetAccountByID(ctx, id)
	if errors.Is(err, postgres.ErrRegistrarAccountNotFound) {
		return nil, ErrAccountNotFound
	}
	return a, err
}

func (s *Service) ListAccounts(ctx context.Context, registrarID int64) ([]postgres.RegistrarAccount, error) {
	// Verify parent exists
	if _, err := s.store.GetByID(ctx, registrarID); err != nil {
		if errors.Is(err, postgres.ErrRegistrarNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("check registrar: %w", err)
	}
	return s.store.ListAccountsByRegistrar(ctx, registrarID)
}

type UpdateAccountInput struct {
	ID          int64
	AccountName string
	OwnerUserID *int64
	Credentials []byte
	IsDefault   bool
	Notes       *string
}

func (s *Service) UpdateAccount(ctx context.Context, in UpdateAccountInput) (*postgres.RegistrarAccount, error) {
	if strings.TrimSpace(in.AccountName) == "" {
		return nil, fmt.Errorf("account name required")
	}

	existing, err := s.store.GetAccountByID(ctx, in.ID)
	if errors.Is(err, postgres.ErrRegistrarAccountNotFound) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	creds := in.Credentials
	if len(creds) == 0 {
		creds = existing.Credentials
	}

	existing.AccountName = in.AccountName
	existing.OwnerUserID = in.OwnerUserID
	existing.Credentials = creds
	existing.IsDefault = in.IsDefault
	existing.Notes = in.Notes

	if err := s.store.UpdateAccount(ctx, existing); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}

	s.logger.Info("registrar account updated", zap.Int64("id", in.ID))
	return s.store.GetAccountByID(ctx, in.ID)
}

func (s *Service) DeleteAccount(ctx context.Context, id int64) error {
	err := s.store.SoftDeleteAccount(ctx, id)
	if errors.Is(err, postgres.ErrRegistrarAccountNotFound) {
		return ErrAccountNotFound
	}
	if errors.Is(err, postgres.ErrRegistrarHasDependents) {
		return ErrHasDependents
	}
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	s.logger.Info("registrar account deleted", zap.Int64("id", id))
	return nil
}

// ── Sync ──────────────────────────────────────────────────────────────────────

// SyncResult summarises the outcome of a SyncAccount call.
type SyncResult struct {
	// Total is the number of domains returned by the registrar API.
	Total int `json:"total"`
	// Updated is the number of domains whose dates were written to our DB.
	Updated int `json:"updated"`
	// NotFound holds FQDNs that exist in the registrar but are not in our DB
	// under this account. They are reported but not created automatically.
	NotFound []string `json:"not_found"`
	// Errors holds per-domain error messages that did not abort the sync.
	Errors []SyncItemError `json:"errors,omitempty"`
}

// SyncItemError records a non-fatal per-domain error during sync.
type SyncItemError struct {
	FQDN    string `json:"fqdn"`
	Message string `json:"message"`
}

// ErrNoAPIType is returned when the registrar has no api_type set.
var ErrNoAPIType = errors.New("registrar has no api_type configured")

// SyncAccount fetches all domains from the registrar API for the given account
// and updates registration_date, expiry_date, and auto_renew in the domains table.
//
// Domains returned by the API that are not found in our DB are recorded in
// SyncResult.NotFound — they are NOT automatically created.
func (s *Service) SyncAccount(ctx context.Context, accountID int64) (*SyncResult, error) {
	// 1. Load account (credentials) and its parent registrar (api_type).
	account, err := s.store.GetAccountByID(ctx, accountID)
	if errors.Is(err, postgres.ErrRegistrarAccountNotFound) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	reg, err := s.store.GetByID(ctx, account.RegistrarID)
	if errors.Is(err, postgres.ErrRegistrarNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get registrar: %w", err)
	}

	if reg.APIType == nil || strings.TrimSpace(*reg.APIType) == "" {
		return nil, ErrNoAPIType
	}

	// 2. Build provider from api_type + credentials.
	provider, err := registrarprovider.Get(*reg.APIType, account.Credentials)
	if err != nil {
		if errors.Is(err, registrarprovider.ErrProviderNotRegistered) {
			return nil, fmt.Errorf("%w: %s", ErrProviderNotSupported, *reg.APIType)
		}
		if errors.Is(err, registrarprovider.ErrMissingCredentials) {
			return nil, ErrCredentialsMissing
		}
		return nil, fmt.Errorf("build provider: %w", err)
	}

	// 3. Fetch domain list from registrar.
	domains, err := provider.ListDomains(ctx)
	if err != nil {
		if errors.Is(err, registrarprovider.ErrAccessDenied) {
			return nil, fmt.Errorf("%w", ErrAccessDenied)
		}
		if errors.Is(err, registrarprovider.ErrUnauthorized) {
			return nil, fmt.Errorf("%w", ErrCredentialsRejected)
		}
		if errors.Is(err, registrarprovider.ErrMissingCredentials) {
			return nil, ErrCredentialsMissing
		}
		if errors.Is(err, registrarprovider.ErrRateLimitExceeded) {
			return nil, ErrRateLimitExceeded
		}
		return nil, fmt.Errorf("list domains from registrar: %w", err)
	}

	result := &SyncResult{
		Total:    len(domains),
		NotFound: []string{},
	}

	// 4. Update each domain's dates in our DB.
	for _, d := range domains {
		updated, err := s.domainStore.UpdateDomainDates(
			ctx, d.FQDN, accountID,
			d.RegistrationDate, d.ExpiryDate, d.AutoRenew,
		)
		if err != nil {
			result.Errors = append(result.Errors, SyncItemError{
				FQDN:    d.FQDN,
				Message: err.Error(),
			})
			continue
		}
		if !updated {
			result.NotFound = append(result.NotFound, d.FQDN)
		} else {
			result.Updated++
		}
	}

	s.logger.Info("registrar account synced",
		zap.Int64("account_id", accountID),
		zap.String("registrar", reg.Name),
		zap.Int("total", result.Total),
		zap.Int("updated", result.Updated),
		zap.Int("not_found", len(result.NotFound)),
	)

	return result, nil
}
