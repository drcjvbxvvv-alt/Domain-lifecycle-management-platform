package registrar

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

var (
	ErrNotFound         = errors.New("registrar not found")
	ErrAccountNotFound  = errors.New("registrar account not found")
	ErrDuplicateName    = errors.New("registrar name already exists")
	ErrHasDependents    = errors.New("registrar has dependent accounts or domains — detach first")
)

type Service struct {
	store  *postgres.RegistrarStore
	logger *zap.Logger
}

func NewService(store *postgres.RegistrarStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
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
