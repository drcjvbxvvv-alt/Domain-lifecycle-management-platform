// Package cdn manages CDN/加速商 provider and account records.
// It is the control-plane counterpart to the Registrar and DNS Provider packages
// and follows the same thin-service-over-store pattern.
package cdn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// ── Sentinel errors ────────────────────────────────────────────────────────────

var (
	ErrProviderNotFound      = errors.New("cdn provider not found")
	ErrAccountNotFound       = errors.New("cdn account not found")
	ErrProviderDuplicate     = errors.New("cdn provider with this type and name already exists")
	ErrAccountDuplicate      = errors.New("cdn account name already exists for this provider")
	ErrProviderHasDependents = errors.New("cdn provider has active accounts — remove them first")
	ErrAccountHasDependents  = errors.New("cdn account is in use by one or more domains — unlink first")
)

// allowedProviderTypes is the exhaustive set of recognised CDN provider_type
// values.  New entries require a code change (intentional — prevents typos).
var allowedProviderTypes = map[string]struct{}{
	"cloudflare":  {},
	"juhe":        {},
	"wangsu":      {},
	"baishan":     {},
	"tencent_cdn": {},
	"huawei_cdn":  {},
	"aliyun_cdn":  {},
	"fastly":      {},
	"other":       {}, // catch-all for unlisted providers
}

// ── Service ────────────────────────────────────────────────────────────────────

type Service struct {
	store  *postgres.CDNStore
	logger *zap.Logger
}

func NewService(store *postgres.CDNStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// ── CDN Provider ───────────────────────────────────────────────────────────────

type CreateProviderInput struct {
	Name         string
	ProviderType string
	Description  *string
}

func (s *Service) CreateProvider(ctx context.Context, in CreateProviderInput) (*postgres.CDNProvider, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("cdn provider name is required")
	}
	if _, ok := allowedProviderTypes[in.ProviderType]; !ok {
		return nil, fmt.Errorf("unsupported provider_type %q — allowed: cloudflare, juhe, wangsu, baishan, tencent_cdn, huawei_cdn, aliyun_cdn, fastly, other", in.ProviderType)
	}

	p := &postgres.CDNProvider{
		Name:         strings.TrimSpace(in.Name),
		ProviderType: in.ProviderType,
		Description:  in.Description,
	}
	created, err := s.store.CreateProvider(ctx, p)
	if err != nil {
		if errors.Is(err, postgres.ErrCDNProviderDuplicate) {
			return nil, ErrProviderDuplicate
		}
		return nil, fmt.Errorf("cdn service: create provider: %w", err)
	}
	s.logger.Info("cdn provider created", zap.Int64("id", created.ID), zap.String("name", created.Name))
	return created, nil
}

func (s *Service) GetProvider(ctx context.Context, id int64) (*postgres.CDNProvider, error) {
	p, err := s.store.GetProviderByID(ctx, id)
	if err != nil {
		if errors.Is(err, postgres.ErrCDNProviderNotFound) {
			return nil, ErrProviderNotFound
		}
		return nil, fmt.Errorf("cdn service: get provider: %w", err)
	}
	return p, nil
}

func (s *Service) ListProviders(ctx context.Context) ([]postgres.CDNProvider, error) {
	providers, err := s.store.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("cdn service: list providers: %w", err)
	}
	return providers, nil
}

type UpdateProviderInput struct {
	Name         string
	ProviderType string
	Description  *string
}

func (s *Service) UpdateProvider(ctx context.Context, id int64, in UpdateProviderInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("cdn provider name is required")
	}
	if _, ok := allowedProviderTypes[in.ProviderType]; !ok {
		return fmt.Errorf("unsupported provider_type %q", in.ProviderType)
	}

	p := &postgres.CDNProvider{
		ID:           id,
		Name:         strings.TrimSpace(in.Name),
		ProviderType: in.ProviderType,
		Description:  in.Description,
	}
	if err := s.store.UpdateProvider(ctx, p); err != nil {
		if errors.Is(err, postgres.ErrCDNProviderNotFound) {
			return ErrProviderNotFound
		}
		if errors.Is(err, postgres.ErrCDNProviderDuplicate) {
			return ErrProviderDuplicate
		}
		return fmt.Errorf("cdn service: update provider: %w", err)
	}
	return nil
}

func (s *Service) DeleteProvider(ctx context.Context, id int64) error {
	if err := s.store.SoftDeleteProvider(ctx, id); err != nil {
		if errors.Is(err, postgres.ErrCDNProviderNotFound) {
			return ErrProviderNotFound
		}
		if errors.Is(err, postgres.ErrCDNProviderHasDependents) {
			return ErrProviderHasDependents
		}
		return fmt.Errorf("cdn service: delete provider: %w", err)
	}
	s.logger.Info("cdn provider deleted", zap.Int64("id", id))
	return nil
}

// ── CDN Account ────────────────────────────────────────────────────────────────

type CreateAccountInput struct {
	CDNProviderID int64
	AccountName   string
	Credentials   []byte // raw JSON; nil → "{}"
	Notes         *string
	Enabled       bool
	CreatedBy     *int64
}

func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*postgres.CDNAccount, error) {
	if strings.TrimSpace(in.AccountName) == "" {
		return nil, fmt.Errorf("cdn account name is required")
	}
	// Verify provider exists.
	if _, err := s.store.GetProviderByID(ctx, in.CDNProviderID); err != nil {
		if errors.Is(err, postgres.ErrCDNProviderNotFound) {
			return nil, ErrProviderNotFound
		}
		return nil, fmt.Errorf("cdn service: verify provider: %w", err)
	}

	creds := in.Credentials
	if len(creds) == 0 {
		creds = []byte("{}")
	}

	a := &postgres.CDNAccount{
		CDNProviderID: in.CDNProviderID,
		AccountName:   strings.TrimSpace(in.AccountName),
		Credentials:   creds,
		Notes:         in.Notes,
		Enabled:       in.Enabled,
		CreatedBy:     in.CreatedBy,
	}
	created, err := s.store.CreateAccount(ctx, a)
	if err != nil {
		if errors.Is(err, postgres.ErrCDNAccountDuplicate) {
			return nil, ErrAccountDuplicate
		}
		return nil, fmt.Errorf("cdn service: create account: %w", err)
	}
	s.logger.Info("cdn account created",
		zap.Int64("id", created.ID),
		zap.String("name", created.AccountName),
		zap.Int64("provider_id", created.CDNProviderID),
	)
	return created, nil
}

func (s *Service) GetAccount(ctx context.Context, id int64) (*postgres.CDNAccount, error) {
	a, err := s.store.GetAccountByID(ctx, id)
	if err != nil {
		if errors.Is(err, postgres.ErrCDNAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("cdn service: get account: %w", err)
	}
	return a, nil
}

func (s *Service) ListAccountsByProvider(ctx context.Context, providerID int64) ([]postgres.CDNAccount, error) {
	accounts, err := s.store.ListAccountsByProvider(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("cdn service: list accounts: %w", err)
	}
	return accounts, nil
}

func (s *Service) ListAllAccounts(ctx context.Context) ([]postgres.CDNAccount, error) {
	accounts, err := s.store.ListAllAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("cdn service: list all accounts: %w", err)
	}
	return accounts, nil
}

type UpdateAccountInput struct {
	AccountName string
	Credentials []byte
	Notes       *string
	Enabled     bool
}

func (s *Service) UpdateAccount(ctx context.Context, id int64, in UpdateAccountInput) error {
	if strings.TrimSpace(in.AccountName) == "" {
		return fmt.Errorf("cdn account name is required")
	}
	creds := in.Credentials
	if len(creds) == 0 {
		creds = []byte("{}")
	}
	a := &postgres.CDNAccount{
		ID:          id,
		AccountName: strings.TrimSpace(in.AccountName),
		Credentials: creds,
		Notes:       in.Notes,
		Enabled:     in.Enabled,
	}
	if err := s.store.UpdateAccount(ctx, a); err != nil {
		if errors.Is(err, postgres.ErrCDNAccountNotFound) {
			return ErrAccountNotFound
		}
		if errors.Is(err, postgres.ErrCDNAccountDuplicate) {
			return ErrAccountDuplicate
		}
		return fmt.Errorf("cdn service: update account: %w", err)
	}
	return nil
}

func (s *Service) DeleteAccount(ctx context.Context, id int64) error {
	if err := s.store.SoftDeleteAccount(ctx, id); err != nil {
		if errors.Is(err, postgres.ErrCDNAccountNotFound) {
			return ErrAccountNotFound
		}
		if errors.Is(err, postgres.ErrCDNAccountHasDependents) {
			return ErrAccountHasDependents
		}
		return fmt.Errorf("cdn service: delete account: %w", err)
	}
	s.logger.Info("cdn account deleted", zap.Int64("id", id))
	return nil
}
