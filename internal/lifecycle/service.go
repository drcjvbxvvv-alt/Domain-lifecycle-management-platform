package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	// Step 1: validate the edge
	if !CanLifecycleTransition(from, to) {
		return fmt.Errorf("transition %q → %q: %w", from, to, ErrInvalidLifecycleState)
	}

	// Step 2-7: transactional update
	tx, err := s.lifecycle.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	err = s.lifecycle.TransitionTx(ctx, tx, domainID, from, to, reason, triggeredBy)
	if err != nil {
		// Unwrap the store-level race error and re-wrap with our domain error
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

// Register creates a new domain in the "requested" state.
// This is the documented exception to the Transition() rule: there is no
// nil → requested edge. The domain_lifecycle_history row is inserted manually.
type RegisterInput struct {
	ProjectID     int64
	FQDN          string
	OwnerUserID   *int64
	DNSProviderID *int64
	TriggeredBy   string
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*postgres.Domain, error) {
	d := &postgres.Domain{
		ProjectID:     in.ProjectID,
		FQDN:          in.FQDN,
		OwnerUserID:   in.OwnerUserID,
		DNSProviderID: in.DNSProviderID,
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
		zap.Int64("project_id", created.ProjectID),
	)

	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.Domain, error) {
	return s.domains.GetByID(ctx, id)
}

type ListInput struct {
	ProjectID int64
	Cursor    int64
	Limit     int
}

type ListResult struct {
	Items  []postgres.Domain `json:"items"`
	Total  int64             `json:"total"`
	Cursor int64             `json:"cursor"`
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	items, err := s.domains.ListByProject(ctx, in.ProjectID, in.Cursor, in.Limit)
	if err != nil {
		return nil, err
	}
	total, err := s.domains.CountByProject(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}
	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return &ListResult{Items: items, Total: total, Cursor: nextCursor}, nil
}

func (s *Service) GetHistory(ctx context.Context, domainID int64) ([]postgres.LifecycleHistoryRow, error) {
	return s.lifecycle.GetHistory(ctx, domainID, 100)
}
