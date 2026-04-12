package artifact

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
	"domain-platform/store/postgres"
)

// HandleBuild is the asynq task handler for TypeArtifactBuild.
// It fetches domains, merges variables, and delegates to Builder.Build.
type HandleBuild struct {
	builder     *Builder
	domainStore *postgres.DomainStore
	tmplStore   *postgres.TemplateStore
	logger      *zap.Logger
}

// NewHandleBuild creates a new artifact build task handler.
func NewHandleBuild(
	builder *Builder,
	domainStore *postgres.DomainStore,
	tmplStore *postgres.TemplateStore,
	logger *zap.Logger,
) *HandleBuild {
	return &HandleBuild{
		builder:     builder,
		domainStore: domainStore,
		tmplStore:   tmplStore,
		logger:      logger,
	}
}

// ProcessTask implements asynq.Handler.
func (h *HandleBuild) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ArtifactBuildPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal artifact build payload: %w", err)
	}

	h.logger.Info("artifact build started",
		zap.Int64("project_id", payload.ProjectID),
		zap.String("project_slug", payload.ProjectSlug),
		zap.Int64("template_version_id", payload.TemplateVersionID),
		zap.Int("domain_count", len(payload.DomainIDs)),
	)

	// Fetch template version to get default variables
	ver, err := h.tmplStore.GetVersion(ctx, payload.TemplateVersionID)
	if err != nil {
		return fmt.Errorf("get template version: %w", err)
	}

	var defaultVars map[string]any
	if len(ver.DefaultVariables) > 0 {
		if err := json.Unmarshal(ver.DefaultVariables, &defaultVars); err != nil {
			return fmt.Errorf("unmarshal default variables: %w", err)
		}
	}

	// Fetch domains and their variables
	domains := make([]DomainRenderInput, 0, len(payload.DomainIDs))
	for _, domainID := range payload.DomainIDs {
		d, err := h.domainStore.GetByID(ctx, domainID)
		if err != nil {
			return fmt.Errorf("get domain %d: %w", domainID, err)
		}

		// Merge: template defaults ← domain variables
		merged := make(map[string]any)
		for k, v := range defaultVars {
			merged[k] = v
		}

		// Fetch domain-specific variables (if store method available)
		domainVars, err := h.domainStore.GetVariables(ctx, domainID)
		if err != nil {
			h.logger.Warn("get domain variables",
				zap.Int64("domain_id", domainID),
				zap.Error(err))
			// Continue without domain variables — use defaults only
		} else {
			for k, v := range domainVars {
				merged[k] = v
			}
		}

		domains = append(domains, DomainRenderInput{
			FQDN:      d.FQDN,
			Variables: merged,
		})
	}

	result, err := h.builder.Build(ctx, BuildRequest{
		ProjectID:         payload.ProjectID,
		ProjectSlug:       payload.ProjectSlug,
		TemplateVersionID: payload.TemplateVersionID,
		ReleaseID:         payload.ReleaseID,
		BuiltBy:           payload.BuiltBy,
		Domains:           domains,
	})
	if err != nil {
		return fmt.Errorf("build artifact: %w", err)
	}

	h.logger.Info("artifact build completed",
		zap.String("artifact_id", result.Artifact.ArtifactID),
		zap.String("checksum", result.Artifact.Checksum),
	)

	return nil
}
