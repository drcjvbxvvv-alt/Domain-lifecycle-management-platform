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
	"domain-platform/pkg/agentprotocol"
	"domain-platform/pkg/storage"
	"domain-platform/store/postgres"
)

// Service implements release lifecycle business logic.
// All state mutations go through TransitionRelease() — see CLAUDE.md Critical Rule #1.
type Service struct {
	releases    *postgres.ReleaseStore
	domains     *postgres.DomainStore
	tmpl        *postgres.TemplateStore
	agents      *postgres.AgentStore
	artifacts   *postgres.ArtifactStore
	domainTasks *postgres.DomainTaskStore
	rollbacks   *postgres.RollbackStore
	storage     storage.Storage
	tasks       *asynq.Client
	logger      *zap.Logger
}

func NewService(
	releases *postgres.ReleaseStore,
	domains *postgres.DomainStore,
	tmpl *postgres.TemplateStore,
	agents *postgres.AgentStore,
	artifacts *postgres.ArtifactStore,
	domainTasks *postgres.DomainTaskStore,
	rollbacks *postgres.RollbackStore,
	storage storage.Storage,
	tasks *asynq.Client,
	logger *zap.Logger,
) *Service {
	return &Service{
		releases:    releases,
		domains:     domains,
		tmpl:        tmpl,
		agents:      agents,
		artifacts:   artifacts,
		domainTasks: domainTasks,
		rollbacks:   rollbacks,
		storage:     storage,
		tasks:       tasks,
		logger:      logger,
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
	ReleaseType       string        // "html" | "nginx" | "full"
	TriggerSource     string        // "ui" | "api" | "ci"
	ShardStrategy     ShardStrategy // how to split domains into shards; default: by_host_group
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
	strategy := in.ShardStrategy
	if strategy == "" {
		strategy = ShardStrategyByHostGroup
	}
	planPayload, _ := json.Marshal(ReleasePlanPayload{
		ReleaseID:     rel.ID,
		DomainIDs:     domainIDs,
		ShardStrategy: strategy,
	})

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
	ReleaseID     int64         `json:"release_id"`
	DomainIDs     []int64       `json:"domain_ids"`
	ShardStrategy ShardStrategy `json:"shard_strategy,omitempty"`
}

// ReleaseDispatchPayload is the JSON payload for TypeReleaseDispatchShard tasks.
type ReleaseDispatchPayload struct {
	ReleaseID int64 `json:"release_id"`
	ShardID   int64 `json:"shard_id"`
}

// ReleaseFinalizePayload is the JSON payload for TypeReleaseFinalize tasks.
type ReleaseFinalizePayload struct {
	ReleaseID        int64 `json:"release_id"`
	ShardID          int64 `json:"shard_id,omitempty"`           // >0: per-shard finalization (P2.2+)
	RetryNum         int   `json:"retry_num,omitempty"`          // tracks how many finalize polls
	IsRollback       bool  `json:"is_rollback,omitempty"`        // true when finalizing a rollback
	RollbackRecordID int64 `json:"rollback_record_id,omitempty"` // rollback_records.id to mark complete
}

// ── Plan ────────────────────────────────────────────────────────────────────

// Plan transitions pending → planning, validates domains, creates shards +
// domain_tasks using the given strategy, and enqueues the artifact build.
// The artifact build handler calls MarkReady() on completion, which transitions
// planning → ready and enqueues dispatch for shard 0.
func (s *Service) Plan(ctx context.Context, releaseDBID int64, strategy ShardStrategy) error {
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

	// Validate all domains are still active and collect their host_group info.
	type domainInfo struct {
		ID          int64
		FQDN        string
		HostGroupID *int64
	}
	var validDomains []domainInfo
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
		validDomains = append(validDomains, domainInfo{
			ID:          d.ID,
			FQDN:        d.FQDN,
			HostGroupID: scope.HostGroupID,
		})
	}

	if len(validDomains) == 0 {
		_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "no active domains remain", "system")
		return ErrNoDomainsInScope
	}

	// Build planner inputs and compute shard layout.
	planInputs := make([]domainPlanInput, 0, len(validDomains))
	for _, d := range validDomains {
		planInputs = append(planInputs, domainPlanInput{ID: d.ID, HostGroupID: d.HostGroupID})
	}
	if strategy == "" {
		strategy = ShardStrategyByHostGroup
	}
	plannedShards := PlanShards(strategy, planInputs)

	// Build a domain info map for O(1) lookups when creating domain_tasks.
	domainInfoMap := make(map[int64]domainInfo, len(validDomains))
	for _, d := range validDomains {
		domainInfoMap[d.ID] = d
	}

	// Create shard rows and domain_tasks for each planned shard.
	allDomainIDs := make([]int64, 0, len(validDomains))
	for _, ps := range plannedShards {
		shard, err := s.releases.CreateShard(ctx, &postgres.ReleaseShard{
			ReleaseID:   releaseDBID,
			ShardIndex:  ps.ShardIndex,
			IsCanary:    false,
			DomainCount: len(ps.DomainIDs),
		})
		if err != nil {
			_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "create shard failed", "system")
			return fmt.Errorf("create shard %d: %w", ps.ShardIndex, err)
		}

		dtBatch := make([]postgres.DomainTask, 0, len(ps.DomainIDs))
		for _, did := range ps.DomainIDs {
			di := domainInfoMap[did]
			dtBatch = append(dtBatch, postgres.DomainTask{
				ReleaseID:   releaseDBID,
				ShardID:     &shard.ID,
				DomainID:    did,
				HostGroupID: di.HostGroupID,
				TaskType:    "deploy_html",
			})
			allDomainIDs = append(allDomainIDs, did)
		}
		if _, err := s.domainTasks.CreateBatch(ctx, dtBatch); err != nil {
			_ = s.TransitionRelease(ctx, releaseDBID, "planning", "failed", "create domain tasks failed", "system")
			return fmt.Errorf("create domain tasks for shard %d: %w", ps.ShardIndex, err)
		}
	}

	// Set totals
	if err := s.releases.SetTotals(ctx, releaseDBID, len(allDomainIDs), len(plannedShards)); err != nil {
		s.logger.Error("set release totals", zap.Error(err))
	}

	// Enqueue artifact build — the build handler calls MarkReady() on completion.
	buildPayload, _ := json.Marshal(tasks.ArtifactBuildPayload{
		ProjectID:         rel.ProjectID,
		ProjectSlug:       rel.ReleaseID[:8],
		TemplateVersionID: rel.TemplateVersionID,
		ReleaseID:         &releaseDBID,
		BuiltBy:           rel.CreatedBy,
		DomainIDs:         allDomainIDs,
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

	s.logger.Info("release plan completed — awaiting artifact build",
		zap.Int64("release_id", releaseDBID),
		zap.Int("domain_count", len(allDomainIDs)),
		zap.Int("shard_count", len(plannedShards)),
		zap.String("strategy", string(strategy)),
	)

	return nil
}

// ── MarkReady ──────────────────────────────────────────────────────────────

// MarkReady is called by the artifact build handler after a successful build.
// It links the artifact to the release, transitions planning → ready, and
// enqueues the dispatch task for each shard.
func (s *Service) MarkReady(ctx context.Context, releaseDBID int64, artifactDBID int64) error {
	// Link artifact to release
	if err := s.releases.SetArtifactID(ctx, releaseDBID, artifactDBID); err != nil {
		return fmt.Errorf("set artifact_id: %w", err)
	}

	// planning → ready
	if err := s.TransitionRelease(ctx, releaseDBID, "planning", "ready", "artifact built", "system"); err != nil {
		return fmt.Errorf("transition to ready: %w", err)
	}

	// Sequential dispatch: only start shard 0 (index 0).
	// Finalize() dispatches subsequent shards as each one completes.
	shards, err := s.releases.ListShards(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("list shards: %w", err)
	}
	if len(shards) > 0 {
		// shards are ordered by shard_index; element 0 has the lowest index
		firstShard := shards[0]
		dispatchPayload, _ := json.Marshal(ReleaseDispatchPayload{
			ReleaseID: releaseDBID,
			ShardID:   firstShard.ID,
		})
		task := asynq.NewTask(tasks.TypeReleaseDispatchShard, dispatchPayload,
			asynq.MaxRetry(3),
			asynq.Timeout(120*time.Second),
			asynq.Queue("release"),
		)
		if _, err := s.tasks.Enqueue(task); err != nil {
			s.logger.Error("enqueue dispatch shard", zap.Error(err), zap.Int64("shard_id", firstShard.ID))
		}
	}

	s.logger.Info("release marked ready — shard 0 dispatch enqueued",
		zap.Int64("release_id", releaseDBID),
		zap.Int64("artifact_id", artifactDBID),
		zap.Int("shard_count", len(shards)),
	)

	return nil
}

// ── Dispatch ────────────────────────────────────────────────────────────────

// DispatchShard creates agent_tasks for every domain_task in the shard,
// assigns them to available agents, and transitions the release to executing.
func (s *Service) DispatchShard(ctx context.Context, releaseDBID int64, shardID int64) error {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}

	// Transition ready → executing (only on first shard dispatch)
	if rel.Status == "ready" {
		if err := s.TransitionRelease(ctx, releaseDBID, "ready", "executing", "dispatch started", "system"); err != nil {
			return fmt.Errorf("transition to executing: %w", err)
		}
		if err := s.releases.SetStartedAt(ctx, releaseDBID); err != nil {
			s.logger.Error("set started_at", zap.Error(err))
		}
	}

	// Update shard status
	if err := s.releases.UpdateShardStatus(ctx, shardID, "dispatching"); err != nil {
		s.logger.Error("update shard status to dispatching", zap.Error(err))
	}

	// Get the artifact for this release
	if rel.ArtifactID == nil {
		return fmt.Errorf("release %d has no artifact_id", releaseDBID)
	}
	art, err := s.artifacts.GetByID(ctx, *rel.ArtifactID)
	if err != nil {
		return fmt.Errorf("get artifact: %w", err)
	}

	// Parse the artifact manifest
	var manifest agentprotocol.Manifest
	if err := json.Unmarshal(art.Manifest, &manifest); err != nil {
		return fmt.Errorf("unmarshal manifest: %w", err)
	}

	// Generate a pre-signed URL for the artifact (120s TTL)
	artifactPrefix := fmt.Sprintf("artifacts/%s/%s", manifest.ProjectSlug, art.Checksum)
	presignedURL, err := s.storage.Presign(ctx, artifactPrefix+"/manifest.json", 2*time.Hour)
	if err != nil {
		s.logger.Warn("presign failed, using storage URI as fallback", zap.Error(err))
		presignedURL = art.StorageURI
	}

	// Get domain_tasks for this shard
	domainTaskRows, err := s.domainTasks.ListByShard(ctx, shardID)
	if err != nil {
		return fmt.Errorf("list domain tasks for shard: %w", err)
	}

	// Get available agents (for P2.1: all online agents; P2.2 will split by host_group)
	onlineAgents, err := s.agents.ListOnlineAgents(ctx)
	if err != nil {
		return fmt.Errorf("list online agents: %w", err)
	}
	if len(onlineAgents) == 0 {
		s.logger.Warn("no online agents available for dispatch", zap.Int64("release_id", releaseDBID))
		// Don't fail — agent_tasks will be created but won't be claimed until agents come online
	}

	// For P2.1: assign domain_tasks round-robin across available agents.
	// If no agents online, still create tasks assigned to the first agent found
	// (they'll be picked up when agents come online).
	for i, dt := range domainTaskRows {
		domain, err := s.domains.GetByID(ctx, dt.DomainID)
		if err != nil {
			s.logger.Error("get domain for task", zap.Error(err), zap.Int64("domain_id", dt.DomainID))
			continue
		}

		// Select agent: round-robin among online agents, or by host_group if specified
		var targetAgent *postgres.Agent
		if dt.HostGroupID != nil && *dt.HostGroupID > 0 {
			hgAgents, err := s.agents.ListOnlineByHostGroup(ctx, *dt.HostGroupID)
			if err == nil && len(hgAgents) > 0 {
				targetAgent = &hgAgents[i%len(hgAgents)]
			}
		}
		if targetAgent == nil && len(onlineAgents) > 0 {
			targetAgent = &onlineAgents[i%len(onlineAgents)]
		}
		if targetAgent == nil {
			s.logger.Warn("no agent available for domain task, skipping",
				zap.Int64("domain_task_id", dt.ID), zap.String("fqdn", domain.FQDN))
			if err := s.domainTasks.UpdateStatus(ctx, dt.ID, "failed", strPtr("no agent available")); err != nil {
				s.logger.Error("update domain task status", zap.Error(err))
			}
			continue
		}

		// Build the TaskEnvelope payload
		envelope := agentprotocol.TaskEnvelope{
			Type:        agentprotocol.TaskTypeDeployHTML,
			ReleaseID:   rel.ReleaseID,
			ArtifactURL: presignedURL,
			Manifest:    manifest,
			Domains:     []string{domain.FQDN},
			DeployPath:  "/var/www",
			NginxPath:   "/etc/nginx/conf.d",
			AllowReload: true,
			Verify: agentprotocol.VerifyConfig{
				Enabled:    true,
				URL:        fmt.Sprintf("http://localhost"),
				StatusCode: 200,
				TimeoutMs:  5000,
			},
		}
		envelopeJSON, _ := json.Marshal(envelope)

		// Deterministic task_id
		taskID := fmt.Sprintf("%s-%d-%d", rel.ReleaseID, dt.DomainID, targetAgent.ID)

		agentTask := &postgres.AgentTask{
			TaskID:       taskID,
			DomainTaskID: dt.ID,
			AgentDBID:    targetAgent.ID,
			ArtifactID:   *rel.ArtifactID,
			ArtifactURL:  &presignedURL,
			Payload:      string(envelopeJSON),
		}
		if _, err := s.agents.CreateAgentTask(ctx, agentTask); err != nil {
			s.logger.Error("create agent task", zap.Error(err),
				zap.String("fqdn", domain.FQDN), zap.String("agent", targetAgent.AgentID))
			continue
		}

		// Update domain_task status to dispatched
		if err := s.domainTasks.UpdateStatus(ctx, dt.ID, "dispatched", nil); err != nil {
			s.logger.Error("update domain task to dispatched", zap.Error(err))
		}
	}

	// Update shard status to running
	if err := s.releases.UpdateShardStatus(ctx, shardID, "running"); err != nil {
		s.logger.Error("update shard status to running", zap.Error(err))
	}

	// Enqueue per-shard finalize with a delay to check completion.
	finalizePayload, _ := json.Marshal(ReleaseFinalizePayload{ReleaseID: releaseDBID, ShardID: shardID})
	finalizeTask := asynq.NewTask(tasks.TypeReleaseFinalize, finalizePayload,
		asynq.MaxRetry(0),
		asynq.Timeout(60*time.Second),
		asynq.Queue("release"),
		asynq.ProcessIn(10*time.Second),
	)
	if _, err := s.tasks.Enqueue(finalizeTask); err != nil {
		s.logger.Error("enqueue finalize task", zap.Error(err))
	}

	s.logger.Info("shard dispatched",
		zap.Int64("release_id", releaseDBID),
		zap.Int64("shard_id", shardID),
		zap.Int("domain_tasks", len(domainTaskRows)),
	)
	return nil
}

// ── Finalize ────────────────────────────────────────────────────────────────

const maxFinalizeRetries = 360 // 360 × 10s = 1 hour max wait

// Finalize checks agent_task completion for a shard (or for the whole release in
// rollback mode) and advances the release state accordingly.
//
//   - shardID > 0: per-shard finalization (P2.2 normal dispatch path).
//     On success, dispatches the next pending shard or finalizes the release.
//     On failure, transitions the release to failed.
//   - isRollback=true: whole-release stats, transitions rolling_back → rolled_back/failed.
//   - shardID == 0 && !isRollback: legacy path (backward compat with pre-P2.2 tasks).
//
// If tasks are still running, Finalize re-enqueues itself with a delay.
func (s *Service) Finalize(ctx context.Context, releaseDBID int64, shardID int64, retryNum int, isRollback bool, rollbackRecordID int64) error {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}

	expectedStatus := "executing"
	if isRollback {
		expectedStatus = "rolling_back"
	}

	if rel.Status != expectedStatus {
		s.logger.Info("finalize skipped — release not in expected state",
			zap.Int64("release_id", releaseDBID),
			zap.String("status", rel.Status),
			zap.String("expected", expectedStatus))
		return nil
	}

	// Fetch stats: per-shard for normal dispatch, per-release for rollback/legacy.
	var stats *postgres.AgentTaskStats
	if !isRollback && shardID > 0 {
		stats, err = s.agents.GetAgentTaskStatsByShard(ctx, shardID)
	} else {
		stats, err = s.agents.GetAgentTaskStatsByRelease(ctx, releaseDBID)
	}
	if err != nil {
		return fmt.Errorf("get agent task stats: %w", err)
	}

	s.logger.Info("finalize check",
		zap.Int64("release_id", releaseDBID),
		zap.Int64("shard_id", shardID),
		zap.Int("total", stats.Total),
		zap.Int("succeeded", stats.Succeeded),
		zap.Int("failed", stats.Failed),
		zap.Int("in_progress", stats.Pending+stats.Claimed+stats.Running),
		zap.Int("retry", retryNum),
	)

	inProgress := stats.Pending + stats.Claimed + stats.Running
	hasFailed := stats.Failed > 0 || stats.Timeout > 0 || (inProgress > 0 && retryNum >= maxFinalizeRetries)

	// Still running — re-enqueue with delay.
	if inProgress > 0 && retryNum < maxFinalizeRetries {
		queue := "release"
		if isRollback {
			queue = "critical"
		}
		payload, _ := json.Marshal(ReleaseFinalizePayload{
			ReleaseID:        releaseDBID,
			ShardID:          shardID,
			RetryNum:         retryNum + 1,
			IsRollback:       isRollback,
			RollbackRecordID: rollbackRecordID,
		})
		task := asynq.NewTask(tasks.TypeReleaseFinalize, payload,
			asynq.MaxRetry(0),
			asynq.Timeout(60*time.Second),
			asynq.Queue(queue),
			asynq.ProcessIn(10*time.Second),
		)
		if _, err := s.tasks.Enqueue(task); err != nil {
			s.logger.Error("re-enqueue finalize", zap.Error(err))
		}
		return nil
	}

	// ── Rollback finalization ────────────────────────────────────────────────
	if isRollback {
		_ = s.releases.IncrementSuccessBy(ctx, releaseDBID, stats.Succeeded)
		_ = s.releases.IncrementFailureBy(ctx, releaseDBID, stats.Failed+stats.Timeout)
		allShards, _ := s.releases.ListShards(ctx, releaseDBID)
		for _, sh := range allShards {
			_ = s.releases.UpdateShardCounts(ctx, sh.ID, stats.Succeeded, stats.Failed+stats.Timeout)
		}
		if hasFailed {
			reason := fmt.Sprintf("failed=%d timeout=%d timed_out=%v",
				stats.Failed, stats.Timeout, retryNum >= maxFinalizeRetries)
			if err := s.TransitionRelease(ctx, releaseDBID, "rolling_back", "failed", reason, "system"); err != nil {
				return fmt.Errorf("transition to failed: %w", err)
			}
			for _, sh := range allShards {
				_ = s.releases.UpdateShardStatus(ctx, sh.ID, "failed")
			}
			if rollbackRecordID > 0 {
				_ = s.rollbacks.Complete(ctx, rollbackRecordID, false)
			}
		} else {
			if err := s.TransitionRelease(ctx, releaseDBID, "rolling_back", "rolled_back", "all rollback tasks completed", "system"); err != nil {
				return fmt.Errorf("transition to rolled_back: %w", err)
			}
			for _, sh := range allShards {
				_ = s.releases.UpdateShardStatus(ctx, sh.ID, "succeeded")
			}
			if rollbackRecordID > 0 {
				_ = s.rollbacks.Complete(ctx, rollbackRecordID, true)
			}
		}
		_ = s.releases.SetEndedAt(ctx, releaseDBID)
		s.logger.Info("rollback finalized",
			zap.Int64("release_id", releaseDBID),
			zap.Bool("success", !hasFailed),
		)
		return nil
	}

	// ── Per-shard finalization (P2.2 normal path) ────────────────────────────
	if shardID > 0 {
		_ = s.releases.UpdateShardCounts(ctx, shardID, stats.Succeeded, stats.Failed+stats.Timeout)
		_ = s.releases.IncrementSuccessBy(ctx, releaseDBID, stats.Succeeded)
		_ = s.releases.IncrementFailureBy(ctx, releaseDBID, stats.Failed+stats.Timeout)

		if hasFailed {
			reason := fmt.Sprintf("shard %d: failed=%d timeout=%d timed_out=%v",
				shardID, stats.Failed, stats.Timeout, retryNum >= maxFinalizeRetries)
			_ = s.releases.UpdateShardStatus(ctx, shardID, "failed")
			if err := s.TransitionRelease(ctx, releaseDBID, "executing", "failed", reason, "system"); err != nil {
				return fmt.Errorf("transition to failed: %w", err)
			}
			_ = s.releases.SetEndedAt(ctx, releaseDBID)
			s.logger.Info("shard failed — release failed",
				zap.Int64("release_id", releaseDBID), zap.Int64("shard_id", shardID))
			return nil
		}

		// Shard succeeded — find and dispatch the next pending shard.
		_ = s.releases.UpdateShardStatus(ctx, shardID, "succeeded")

		currentShard, err := s.releases.GetShardByID(ctx, shardID)
		if err != nil {
			s.logger.Error("get current shard", zap.Error(err))
		}
		afterIndex := 0
		if currentShard != nil {
			afterIndex = currentShard.ShardIndex
		}

		nextShard, err := s.releases.GetNextPendingShard(ctx, releaseDBID, afterIndex)
		if err != nil {
			s.logger.Error("get next pending shard", zap.Error(err))
		}

		if nextShard != nil {
			dispatchPayload, _ := json.Marshal(ReleaseDispatchPayload{
				ReleaseID: releaseDBID,
				ShardID:   nextShard.ID,
			})
			task := asynq.NewTask(tasks.TypeReleaseDispatchShard, dispatchPayload,
				asynq.MaxRetry(3),
				asynq.Timeout(120*time.Second),
				asynq.Queue("release"),
			)
			if _, err := s.tasks.Enqueue(task); err != nil {
				s.logger.Error("enqueue next shard dispatch", zap.Error(err), zap.Int64("shard_id", nextShard.ID))
			}
			s.logger.Info("shard complete — dispatching next shard",
				zap.Int64("release_id", releaseDBID),
				zap.Int64("completed_shard", shardID),
				zap.Int64("next_shard", nextShard.ID),
				zap.Int("next_index", nextShard.ShardIndex),
			)
			return nil
		}

		// All shards completed — finalize the release.
		if err := s.TransitionRelease(ctx, releaseDBID, "executing", "succeeded", "all shards completed", "system"); err != nil {
			return fmt.Errorf("transition to succeeded: %w", err)
		}
		_ = s.releases.SetEndedAt(ctx, releaseDBID)
		s.logger.Info("release succeeded — all shards completed",
			zap.Int64("release_id", releaseDBID),
		)
		return nil
	}

	// ── Legacy path: shardID == 0, not a rollback ────────────────────────────
	// Handles pre-P2.2 finalize tasks or edge cases without a shard context.
	_ = s.releases.IncrementSuccessBy(ctx, releaseDBID, stats.Succeeded)
	_ = s.releases.IncrementFailureBy(ctx, releaseDBID, stats.Failed+stats.Timeout)
	allShards, _ := s.releases.ListShards(ctx, releaseDBID)
	for _, sh := range allShards {
		_ = s.releases.UpdateShardCounts(ctx, sh.ID, stats.Succeeded, stats.Failed+stats.Timeout)
	}
	if hasFailed {
		reason := fmt.Sprintf("failed=%d timeout=%d timed_out=%v",
			stats.Failed, stats.Timeout, retryNum >= maxFinalizeRetries)
		if err := s.TransitionRelease(ctx, releaseDBID, "executing", "failed", reason, "system"); err != nil {
			return fmt.Errorf("transition to failed: %w", err)
		}
		for _, sh := range allShards {
			_ = s.releases.UpdateShardStatus(ctx, sh.ID, "failed")
		}
	} else {
		if err := s.TransitionRelease(ctx, releaseDBID, "executing", "succeeded", "all tasks completed", "system"); err != nil {
			return fmt.Errorf("transition to succeeded: %w", err)
		}
		for _, sh := range allShards {
			_ = s.releases.UpdateShardStatus(ctx, sh.ID, "succeeded")
		}
	}
	_ = s.releases.SetEndedAt(ctx, releaseDBID)
	s.logger.Info("release finalized (legacy path)",
		zap.Int64("release_id", releaseDBID),
		zap.Int("succeeded", stats.Succeeded),
		zap.Int("failed", stats.Failed),
	)
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

func strPtr(s string) *string {
	return &s
}
