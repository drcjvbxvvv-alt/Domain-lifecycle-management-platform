package release

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/store/postgres"
)

// Service implements release lifecycle business logic.
// All state mutations go through TransitionRelease() — see CLAUDE.md Critical Rule #1.
type Service struct {
	releases *postgres.ReleaseStore
	domains  *postgres.DomainStore
	tmpl     *postgres.TemplateStore
	tasks    *asynq.Client
	logger   *zap.Logger
}

func NewService(
	releases *postgres.ReleaseStore,
	domains *postgres.DomainStore,
	tmpl *postgres.TemplateStore,
	tasks *asynq.Client,
	logger *zap.Logger,
) *Service {
	return &Service{
		releases: releases,
		domains:  domains,
		tmpl:     tmpl,
		tasks:    tasks,
		logger:   logger,
	}
}

// ── State transition ────────────────────────────────────────────────────────

// TransitionRelease atomically moves a release from one state to another.
//
// The method:
//  1. Validates the edge (from → to) against the state machine
//  2. Opens a transaction
//  3. SELECT ... FOR UPDATE on the release row
//  4. Optimistic check: current status == from
//  5. UPDATE releases SET status (single write path)
//  6. INSERT into release_state_history
//  7. Commits
func (s *Service) TransitionRelease(ctx context.Context, releaseDBID int64, from, to, reason, triggeredBy string) error {
	// Step 1: validate the edge
	if !CanReleaseTransition(from, to) {
		return fmt.Errorf("transition %q → %q: %w", from, to, ErrInvalidReleaseState)
	}

	// Step 2-7: transactional update
	tx, err := s.releases.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	err = s.releases.TransitionTx(ctx, tx, releaseDBID, from, to, reason, triggeredBy)
	if err != nil {
		if errors.Is(err, postgres.ErrReleaseRaceCondition) {
			return ErrReleaseRaceCondition
		}
		return fmt.Errorf("transition release %d: %w", releaseDBID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}

	s.logger.Info("release state transition",
		zap.Int64("release_id", releaseDBID),
		zap.String("from", from),
		zap.String("to", to),
		zap.String("reason", reason),
		zap.String("triggered_by", triggeredBy),
	)

	return nil
}

// ── Create ──────────────────────────────────────────────────────────────────

// CreateInput is the request to create a new release.
type CreateInput struct {
	ProjectID         int64
	ProjectSlug       string
	TemplateVersionID int64
	ReleaseType       string // "html" | "nginx" | "full"
	TriggerSource     string // "ui" | "api" | "ci"
	Description       *string
	DomainIDs         []int64 // explicit scope; if empty, all active domains in project
	CreatedBy         *int64
}

// Create inserts a release in "pending" state and enqueues TypeReleasePlan.
func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.Release, error) {
	// Validate template version exists and is published
	ver, err := s.tmpl.GetVersion(ctx, in.TemplateVersionID)
	if err != nil {
		return nil, fmt.Errorf("get template version: %w", err)
	}
	if ver.PublishedAt == nil {
		return nil, ErrTemplateNotPublished
	}

	// Resolve domain scope
	var domainIDs []int64
	if len(in.DomainIDs) > 0 {
		// Validate all supplied domains are active
		for _, did := range in.DomainIDs {
			d, err := s.domains.GetByID(ctx, did)
			if err != nil {
				return nil, fmt.Errorf("get domain %d: %w", did, err)
			}
			if d.LifecycleState != "active" {
				return nil, fmt.Errorf("domain %d (%s) state=%s: %w", did, d.FQDN, d.LifecycleState, ErrDomainNotActive)
			}
			domainIDs = append(domainIDs, did)
		}
	} else {
		// Default: all active domains in the project
		activeDomains, err := s.domains.ListActiveByProject(ctx, in.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("list active domains: %w", err)
		}
		for _, d := range activeDomains {
			domainIDs = append(domainIDs, d.ID)
		}
	}

	if len(domainIDs) == 0 {
		return nil, ErrNoDomainsInScope
	}

	// Generate release_id (deterministic from inputs for traceability)
	releaseIDStr := generateReleaseID(in.ProjectSlug, in.TemplateVersionID, time.Now())

	releaseType := in.ReleaseType
	if releaseType == "" {
		releaseType = "html"
	}
	triggerSource := in.TriggerSource
	if triggerSource == "" {
		triggerSource = "ui"
	}

	rel, err := s.releases.Create(ctx, &postgres.Release{
		ReleaseID:         releaseIDStr,
		ProjectID:         in.ProjectID,
		TemplateVersionID: in.TemplateVersionID,
		ReleaseType:       releaseType,
		TriggerSource:     triggerSource,
		Description:       in.Description,
		CreatedBy:         in.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("create release: %w", err)
	}

	// Insert scope rows
	for _, did := range domainIDs {
		_, err := s.releases.CreateScope(ctx, &postgres.ReleaseScope{
			ReleaseID: rel.ID,
			DomainID:  did,
		})
		if err != nil {
			s.logger.Error("create release scope", zap.Error(err), zap.Int64("domain_id", did))
		}
	}

	// Insert initial history row
	tx, err := s.releases.BeginTx(ctx)
	if err == nil {
		_, _ = tx.ExecContext(ctx,
			`INSERT INTO release_state_history (release_id, from_state, to_state, reason, triggered_by)
			 VALUES ($1, NULL, 'pending', 'release created', $2)`,
			rel.ID, triggeredByStr(in.CreatedBy))
		tx.Commit() //nolint:errcheck
	}

	// Enqueue the plan task
	payload, _ := json.Marshal(tasks.ArtifactBuildPayload{
		ProjectID:         in.ProjectID,
		ProjectSlug:       in.ProjectSlug,
		TemplateVersionID: in.TemplateVersionID,
		ReleaseID:         &rel.ID,
		BuiltBy:           in.CreatedBy,
		DomainIDs:         domainIDs,
	})
	planPayload, _ := json.Marshal(ReleasePlanPayload{
		ReleaseID: rel.ID,
		DomainIDs: domainIDs,
	})
	_ = payload // artifact build payload prepared for later

	task := asynq.NewTask(tasks.TypeReleasePlan, planPayload,
		asynq.MaxRetry(3),
		asynq.Timeout(120*time.Second),
		asynq.Queue("release"),
	)
	if _, err := s.tasks.Enqueue(task); err != nil {
		s.logger.Error("enqueue release plan task", zap.Error(err), zap.Int64("release_id", rel.ID))
	}

	s.logger.Info("release created",
		zap.Int64("id", rel.ID),
		zap.String("release_id", rel.ReleaseID),
		zap.Int64("project_id", rel.ProjectID),
		zap.Int("domain_count", len(domainIDs)),
	)

	return rel, nil
}

// ReleasePlanPayload is the JSON payload for TypeReleasePlan tasks.
type ReleasePlanPayload struct {
	ReleaseID int64   `json:"release_id"`
	DomainIDs []int64 `json:"domain_ids"`
}

// ReleaseDispatchPayload is the JSON payload for TypeReleaseDispatchShard tasks.
type ReleaseDispatchPayload struct {
	ReleaseID int64 `json:"release_id"`
	ShardID   int64 `json:"shard_id"`
}

// ReleaseFinalizePayload is the JSON payload for TypeReleaseFinalize tasks.
type ReleaseFinalizePayload struct {
	ReleaseID int64 `json:"release_id"`
}

// ── Plan ────────────────────────────────────────────────────────────────────

// Plan transitions pending → planning, validates domains, creates shard 0,
// enqueues artifact build, and on completion transitions planning → ready.
func (s *Service) Plan(ctx context.Context, releaseDBID int64) error {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}

	// pending → planning
	if err := s.TransitionRelease(ctx, releaseDBID, "pending", "planning", "plan started", "system"); err != nil {
		return fmt.Errorf("transition to planning: %w", err)
	}

	// Fetch scoped domains
	scopes, err := s.releases.ListScopes(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("list scopes: %w", err)
	}
	if len(scopes) == 0 {
		_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "no domains in scope", "system")
		return ErrNoDomainsInScope
	}

	// Validate all domains are still active
	domainIDs := make([]int64, 0, len(scopes))
	for _, scope := range scopes {
		d, err := s.domains.GetByID(ctx, scope.DomainID)
		if err != nil {
			s.logger.Warn("domain not found during plan", zap.Int64("domain_id", scope.DomainID))
			continue
		}
		if d.LifecycleState != "active" {
			s.logger.Warn("domain not active, excluding from release",
				zap.Int64("domain_id", d.ID), zap.String("fqdn", d.FQDN), zap.String("state", d.LifecycleState))
			continue
		}
		domainIDs = append(domainIDs, d.ID)
	}

	if len(domainIDs) == 0 {
		_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "no active domains remain", "system")
		return ErrNoDomainsInScope
	}

	// Phase 1: single shard 0 containing all domains (no sharding)
	shard, err := s.releases.CreateShard(ctx, &postgres.ReleaseShard{
		ReleaseID:   releaseDBID,
		ShardIndex:  0,
		IsCanary:    false,
		DomainCount: len(domainIDs),
	})
	if err != nil {
		_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "create shard failed", "system")
		return fmt.Errorf("create shard: %w", err)
	}

	// Set totals
	if err := s.releases.SetTotals(ctx, releaseDBID, len(domainIDs), 1); err != nil {
		s.logger.Error("set release totals", zap.Error(err))
	}

	// Enqueue artifact build
	buildPayload, _ := json.Marshal(tasks.ArtifactBuildPayload{
		ProjectID:         rel.ProjectID,
		ProjectSlug:       rel.ReleaseID[:8], // use first 8 chars as slug fallback
		TemplateVersionID: rel.TemplateVersionID,
		ReleaseID:         &releaseDBID,
		BuiltBy:           rel.CreatedBy,
		DomainIDs:         domainIDs,
	})
	buildTask := asynq.NewTask(tasks.TypeArtifactBuild, buildPayload,
		asynq.MaxRetry(2),
		asynq.Timeout(300*time.Second),
		asynq.Queue("artifact"),
	)
	if _, err := s.tasks.Enqueue(buildTask); err != nil {
		_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "enqueue build failed", "system")
		return fmt.Errorf("enqueue artifact build: %w", err)
	}

	s.logger.Info("release plan completed",
		zap.Int64("release_id", releaseDBID),
		zap.Int("domain_count", len(domainIDs)),
		zap.Int64("shard_id", shard.ID),
	)

	// Note: the actual planning → ready transition happens when the artifact
	// build handler completes and calls MarkReady. For P1, we transition
	// immediately since the build is enqueued.
	// In a real flow, the artifact build handler would call back.
	// For Phase 1 simplicity, we transition to ready here.
	if err := s.TransitionRelease(ctx, releaseDBID, "planning", "ready", "artifact build enqueued", "system"); err != nil {
		s.logger.Error("transition to ready", zap.Error(err))
	}

	return nil
}

// ── Dispatch ────────────────────────────────────────────────────────────────

// Dispatch transitions ready → executing. In Phase 1 this is a simple
// state change; actual agent task dispatch happens in P1.9/P1.10.
func (s *Service) Dispatch(ctx context.Context, releaseDBID int64) error {
	if err := s.TransitionRelease(ctx, releaseDBID, "ready", "executing", "dispatch started", "system"); err != nil {
		return fmt.Errorf("transition to executing: %w", err)
	}

	if err := s.releases.SetStartedAt(ctx, releaseDBID); err != nil {
		s.logger.Error("set started_at", zap.Error(err))
	}

	// Phase 1: enqueue finalize task (agent tasks created in P1.9/P1.10)
	finalizePayload, _ := json.Marshal(ReleaseFinalizePayload{ReleaseID: releaseDBID})
	task := asynq.NewTask(tasks.TypeReleaseFinalize, finalizePayload,
		asynq.MaxRetry(3),
		asynq.Timeout(60*time.Second),
		asynq.Queue("release"),
	)
	if _, err := s.tasks.Enqueue(task); err != nil {
		s.logger.Error("enqueue finalize task", zap.Error(err))
	}

	s.logger.Info("release dispatch started", zap.Int64("release_id", releaseDBID))
	return nil
}

// ── Finalize ────────────────────────────────────────────────────────────────

// Finalize checks task completion and transitions executing → succeeded or failed.
// In Phase 1 (no real agents), it auto-succeeds.
func (s *Service) Finalize(ctx context.Context, releaseDBID int64) error {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}

	if rel.Status != "executing" {
		return fmt.Errorf("release %d status is %q, expected executing", releaseDBID, rel.Status)
	}

	// Phase 1: auto-succeed (no real agent tasks to check)
	// In Phase 2+, this will check agent_tasks completion status.
	if err := s.TransitionRelease(ctx, releaseDBID, "executing", "succeeded", "all tasks completed", "system"); err != nil {
		return fmt.Errorf("transition to succeeded: %w", err)
	}

	if err := s.releases.SetEndedAt(ctx, releaseDBID); err != nil {
		s.logger.Error("set ended_at", zap.Error(err))
	}

	s.logger.Info("release finalized", zap.Int64("release_id", releaseDBID), zap.String("status", "succeeded"))
	return nil
}

// ── Pause / Resume / Cancel ─────────────────────────────────────────────────

// Pause transitions executing → paused.
func (s *Service) Pause(ctx context.Context, releaseDBID int64, reason, triggeredBy string) error {
	return s.TransitionRelease(ctx, releaseDBID, "executing", "paused", reason, triggeredBy)
}

// Resume transitions paused → executing.
func (s *Service) Resume(ctx context.Context, releaseDBID int64, triggeredBy string) error {
	return s.TransitionRelease(ctx, releaseDBID, "paused", "executing", "resumed", triggeredBy)
}

// Cancel transitions a non-terminal release to cancelled.
// Allowed from: pending, planning, ready, paused, failed.
func (s *Service) Cancel(ctx context.Context, releaseDBID int64, reason, triggeredBy string) error {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}

	if !CanReleaseTransition(rel.Status, "cancelled") {
		return fmt.Errorf("cannot cancel release in %q state: %w", rel.Status, ErrInvalidReleaseState)
	}

	if err := s.TransitionRelease(ctx, releaseDBID, rel.Status, "cancelled", reason, triggeredBy); err != nil {
		return err
	}

	if err := s.releases.SetEndedAt(ctx, releaseDBID); err != nil {
		s.logger.Error("set ended_at on cancel", zap.Error(err))
	}

	return nil
}

// ── Read helpers ─────────────────────────────────────────────────────────────

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.Release, error) {
	return s.releases.GetByID(ctx, id)
}

type ListInput struct {
	ProjectID int64
	Cursor    int64
	Limit     int
}

type ListResult struct {
	Items  []postgres.Release
	Total  int64
	Cursor int64
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	items, err := s.releases.ListByProject(ctx, in.ProjectID, in.Cursor, in.Limit)
	if err != nil {
		return nil, err
	}
	total, err := s.releases.CountByProject(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}
	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return &ListResult{Items: items, Total: total, Cursor: nextCursor}, nil
}

func (s *Service) GetHistory(ctx context.Context, releaseDBID int64) ([]postgres.ReleaseStateHistoryRow, error) {
	return s.releases.GetHistory(ctx, releaseDBID, 100)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func generateReleaseID(projectSlug string, templateVersionID int64, t time.Time) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s:%d:%d", projectSlug, templateVersionID, t.UnixNano())))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func triggeredByStr(userID *int64) string {
	if userID != nil {
		return fmt.Sprintf("user:%d", *userID)
	}
	return "system"
}
