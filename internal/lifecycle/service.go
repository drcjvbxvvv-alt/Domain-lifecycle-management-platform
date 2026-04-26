package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// Service implements the domain lifecycle business logic.
// All state mutations go through Transition() — see CLAUDE.md Critical Rule #1.
type Service struct {
	domains   *postgres.DomainStore
	lifecycle *postgres.LifecycleStore
	logger    *zap.Logger
}

func NewService(domains *postgres.DomainStore, lifecycle *postgres.LifecycleStore, logger *zap.Logger) *Service {
	return &Service{domains: domains, lifecycle: lifecycle, logger: logger}
}

// ── State machine ─────────────────────────────────────────────────────────────

// Transition atomically moves a domain from one lifecycle state to another.
//
// The method:
//  1. Validates the edge (from → to) against the state machine
//  2. Opens a transaction
//  3. SELECT ... FOR UPDATE on the domain row
//  4. Optimistic check: current state == from
//  5. UPDATE domains SET lifecycle_state (single write path)
//  6. INSERT into domain_lifecycle_history
//  7. Commits
//
// If a concurrent caller has already changed the state, ErrLifecycleRaceCondition
// is returned. The caller should retry or inform the user.
func (s *Service) Transition(ctx context.Context, domainID int64, from, to, reason, triggeredBy string) error {
	if !CanLifecycleTransition(from, to) {
		return fmt.Errorf("transition %q → %q: %w", from, to, ErrInvalidLifecycleState)
	}

	tx, err := s.lifecycle.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	err = s.lifecycle.TransitionTx(ctx, tx, domainID, from, to, reason, triggeredBy)
	if err != nil {
		if errors.Is(err, postgres.ErrLifecycleRaceCondition) {
			return ErrLifecycleRaceCondition
		}
		return fmt.Errorf("transition domain %d: %w", domainID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}

	s.logger.Info("domain lifecycle transition",
		zap.Int64("domain_id", domainID),
		zap.String("from", from),
		zap.String("to", to),
		zap.String("reason", reason),
		zap.String("triggered_by", triggeredBy),
	)

	return nil
}

// ── TLD extraction ────────────────────────────────────────────────────────────

// ExtractTLD returns the TLD portion of an FQDN (with leading dot).
// Handles common ccSLDs (co.uk, com.au, etc.) without requiring a full PSL library.
// The heuristic: if the last label is a 2-letter ccTLD and the second-to-last
// label is ≤ 4 characters (e.g. "co", "com", "net", "org", "gov"), treat it
// as a 2-level TLD.
//
// Examples:
//
//	"example.com"        → ".com"
//	"www.example.com"    → ".com"
//	"test.co.uk"         → ".co.uk"
//	"api.example.co.uk"  → ".co.uk"
//	"shop.example.com.au"→ ".com.au"
//	"example.io"         → ".io"
func ExtractTLD(fqdn string) string {
	fqdn = strings.TrimSuffix(strings.ToLower(fqdn), ".")
	parts := strings.Split(fqdn, ".")
	n := len(parts)
	if n < 2 {
		return "." + fqdn
	}
	last := parts[n-1]
	secondToLast := parts[n-2]
	// ccSLD heuristic: last part is 2-letter ccTLD, second-to-last ≤ 4 chars
	if len(last) == 2 && len(secondToLast) <= 4 && n >= 3 {
		return "." + secondToLast + "." + last
	}
	return "." + last
}

// ── Registration ──────────────────────────────────────────────────────────────

// Register creates a new domain in the "requested" state.
// This is the documented exception to the Transition() rule: there is no
// nil → requested edge. The domain_lifecycle_history row is inserted manually.
type RegisterInput struct {
	ProjectID          int64
	FQDN               string
	OwnerUserID        *int64
	DNSProviderID      *int64
	RegistrarAccountID *int64
	CDNAccountID       *int64
	OriginIPs          []string
	RegistrationDate   *time.Time
	ExpiryDate         *time.Time
	AutoRenew          bool
	AnnualCost         *float64
	Currency           *string
	Purpose            *string
	Notes              *string
	TriggeredBy        string
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*postgres.Domain, error) {
	tld := ExtractTLD(in.FQDN)
	d := &postgres.Domain{
		ProjectID:          in.ProjectID,
		FQDN:               in.FQDN,
		TLD:                &tld,
		OwnerUserID:        in.OwnerUserID,
		DNSProviderID:      in.DNSProviderID,
		RegistrarAccountID: in.RegistrarAccountID,
		CDNAccountID:       in.CDNAccountID,
		OriginIPs:          in.OriginIPs,
		RegistrationDate:   in.RegistrationDate,
		ExpiryDate:         in.ExpiryDate,
		AutoRenew:          in.AutoRenew,
		AnnualCost:         in.AnnualCost,
		Currency:           in.Currency,
		Purpose:            in.Purpose,
		Notes:              in.Notes,
	}

	created, err := s.domains.Create(ctx, d)
	if err != nil {
		if strings.Contains(err.Error(), "uq_domains_fqdn") {
			return nil, ErrDuplicateFQDN
		}
		return nil, fmt.Errorf("register domain: %w", err)
	}

	// Insert initial history row (nil → requested)
	tx, err := s.lifecycle.BeginTx(ctx)
	if err != nil {
		return created, nil // domain created but history insert failed — non-fatal
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx,
		`INSERT INTO domain_lifecycle_history (domain_id, from_state, to_state, reason, triggered_by)
		 VALUES ($1, NULL, 'requested', 'domain registered', $2)`,
		created.ID, in.TriggeredBy)
	if err != nil {
		s.logger.Warn("failed to insert initial lifecycle history", zap.Error(err))
		return created, nil
	}
	tx.Commit() //nolint:errcheck

	s.logger.Info("domain registered",
		zap.Int64("id", created.ID),
		zap.String("fqdn", created.FQDN),
		zap.String("tld", tld),
		zap.Int64("project_id", created.ProjectID),
	)

	return created, nil
}

// ── Read ──────────────────────────────────────────────────────────────────────

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.Domain, error) {
	return s.domains.GetByID(ctx, id)
}

// ── List ──────────────────────────────────────────────────────────────────────

// ListInput supports both legacy (project_id only) and extended filtering.
type ListInput struct {
	ProjectID      *int64
	RegistrarID    *int64
	DNSProviderID  *int64
	CDNAccountID   *int64
	TLD            *string
	ExpiryStatus   *string
	LifecycleState *string
	TagID          *int64
	Cursor         int64
	Limit          int
}

type ListResult struct {
	Items  []postgres.Domain `json:"items"`
	Total  int64             `json:"total"`
	Cursor int64             `json:"cursor"`
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	f := postgres.ListFilter{
		ProjectID:      in.ProjectID,
		RegistrarID:    in.RegistrarID,
		DNSProviderID:  in.DNSProviderID,
		CDNAccountID:   in.CDNAccountID,
		TLD:            in.TLD,
		ExpiryStatus:   in.ExpiryStatus,
		LifecycleState: in.LifecycleState,
		TagID:          in.TagID,
		Cursor:         in.Cursor,
		Limit:          in.Limit,
	}

	items, err := s.domains.ListWithFilter(ctx, f)
	if err != nil {
		return nil, err
	}
	total, err := s.domains.CountWithFilter(ctx, f)
	if err != nil {
		return nil, err
	}
	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return &ListResult{Items: items, Total: total, Cursor: nextCursor}, nil
}

// ── Update asset fields ───────────────────────────────────────────────────────

type UpdateAssetInput struct {
	ID                 int64
	RegistrarAccountID *int64
	DNSProviderID      *int64
	CDNAccountID       *int64
	OriginIPs          []string
	RegistrationDate   *time.Time
	ExpiryDate         *time.Time
	AutoRenew          bool
	GraceEndDate       *time.Time
	TransferLock       bool
	Hold               bool
	DNSSECEnabled      bool
	WhoisPrivacy       bool
	AnnualCost         *float64
	Currency           *string
	PurchasePrice      *float64
	FeeFixed           bool
	Purpose            *string
	Notes              *string
}

func (s *Service) UpdateAsset(ctx context.Context, in UpdateAssetInput) (*postgres.Domain, error) {
	existing, err := s.domains.GetByID(ctx, in.ID)
	if errors.Is(err, postgres.ErrDomainNotFound) {
		return nil, ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}

	// Apply changes
	existing.RegistrarAccountID = in.RegistrarAccountID
	existing.DNSProviderID = in.DNSProviderID
	existing.CDNAccountID = in.CDNAccountID
	existing.OriginIPs = in.OriginIPs
	existing.RegistrationDate = in.RegistrationDate
	existing.ExpiryDate = in.ExpiryDate
	existing.AutoRenew = in.AutoRenew
	existing.GraceEndDate = in.GraceEndDate
	existing.TransferLock = in.TransferLock
	existing.Hold = in.Hold
	existing.DNSSECEnabled = in.DNSSECEnabled
	existing.WhoisPrivacy = in.WhoisPrivacy
	existing.AnnualCost = in.AnnualCost
	existing.Currency = in.Currency
	existing.PurchasePrice = in.PurchasePrice
	existing.FeeFixed = in.FeeFixed
	existing.Purpose = in.Purpose
	existing.Notes = in.Notes

	if err := s.domains.UpdateAssetFields(ctx, existing); err != nil {
		return nil, fmt.Errorf("update domain asset: %w", err)
	}

	s.logger.Info("domain asset updated", zap.Int64("id", in.ID))
	return s.domains.GetByID(ctx, in.ID)
}

// ── Transfer tracking ─────────────────────────────────────────────────────────

type InitiateTransferInput struct {
	DomainID                int64
	GainingRegistrarAccount *string // name/reference of gaining registrar
	Notes                   *string
}

func (s *Service) InitiateTransfer(ctx context.Context, in InitiateTransferInput) (*postgres.Domain, error) {
	d, err := s.domains.GetByID(ctx, in.DomainID)
	if errors.Is(err, postgres.ErrDomainNotFound) {
		return nil, ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}

	if d.TransferStatus != nil && *d.TransferStatus == "pending" {
		return nil, ErrTransferAlreadyPending
	}

	status := "pending"
	now := time.Now()
	if err := s.domains.UpdateTransferStatus(ctx, in.DomainID, &status, in.GainingRegistrarAccount, &now, nil); err != nil {
		return nil, fmt.Errorf("initiate transfer: %w", err)
	}

	s.logger.Info("domain transfer initiated",
		zap.Int64("domain_id", in.DomainID),
		zap.Stringp("gaining", in.GainingRegistrarAccount),
	)
	return s.domains.GetByID(ctx, in.DomainID)
}

func (s *Service) CompleteTransfer(ctx context.Context, domainID int64) (*postgres.Domain, error) {
	d, err := s.domains.GetByID(ctx, domainID)
	if errors.Is(err, postgres.ErrDomainNotFound) {
		return nil, ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}

	if d.TransferStatus == nil || *d.TransferStatus != "pending" {
		return nil, ErrNoActiveTransfer
	}

	status := "completed"
	now := time.Now()
	if err := s.domains.UpdateTransferStatus(ctx, domainID, &status, d.TransferGainingRegistrar, d.TransferRequestedAt, &now); err != nil {
		return nil, fmt.Errorf("complete transfer: %w", err)
	}

	s.logger.Info("domain transfer completed", zap.Int64("domain_id", domainID))
	return s.domains.GetByID(ctx, domainID)
}

func (s *Service) CancelTransfer(ctx context.Context, domainID int64) (*postgres.Domain, error) {
	d, err := s.domains.GetByID(ctx, domainID)
	if errors.Is(err, postgres.ErrDomainNotFound) {
		return nil, ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}

	if d.TransferStatus == nil || *d.TransferStatus != "pending" {
		return nil, ErrNoActiveTransfer
	}

	status := "cancelled"
	if err := s.domains.UpdateTransferStatus(ctx, domainID, &status, nil, nil, nil); err != nil {
		return nil, fmt.Errorf("cancel transfer: %w", err)
	}

	s.logger.Info("domain transfer cancelled", zap.Int64("domain_id", domainID))
	return s.domains.GetByID(ctx, domainID)
}

// ── Stats & Expiry ────────────────────────────────────────────────────────────

func (s *Service) ListExpiring(ctx context.Context, days int) ([]postgres.Domain, error) {
	if days <= 0 {
		days = 30
	}
	return s.domains.ListExpiring(ctx, days)
}

func (s *Service) GetStats(ctx context.Context, projectID *int64) (*postgres.DomainStats, error) {
	return s.domains.GetStats(ctx, projectID)
}

// ── History ───────────────────────────────────────────────────────────────────

func (s *Service) GetHistory(ctx context.Context, domainID int64) ([]postgres.LifecycleHistoryRow, error) {
	return s.lifecycle.GetHistory(ctx, domainID, 100)
}

