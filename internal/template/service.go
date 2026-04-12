package template

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// Service implements template and template-version business logic.
// Critical Rule #9: template_versions are immutable once published_at IS NOT NULL.
type Service struct {
	store  *postgres.TemplateStore
	logger *zap.Logger
}

func NewService(store *postgres.TemplateStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

var (
	ErrTemplateNotFound        = postgres.ErrTemplateNotFound
	ErrTemplateVersionNotFound = postgres.ErrTemplateVersionNotFound
	ErrVersionImmutable        = postgres.ErrVersionImmutable
)

// ── Template CRUD ─────────────────────────────────────────────────────────────

type CreateInput struct {
	ProjectID   int64
	Name        string
	Description *string
	Kind        string // "html" | "nginx" | "full"
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.Template, error) {
	kind := in.Kind
	if kind == "" {
		kind = "full"
	}
	t := &postgres.Template{
		ProjectID:   in.ProjectID,
		Name:        in.Name,
		Description: in.Description,
		Kind:        kind,
	}
	created, err := s.store.Create(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	s.logger.Info("template created",
		zap.Int64("id", created.ID),
		zap.String("name", created.Name),
		zap.Int64("project_id", created.ProjectID),
	)
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.Template, error) {
	return s.store.GetByID(ctx, id)
}

type ListInput struct {
	ProjectID int64
	Cursor    int64
	Limit     int
}

type ListResult struct {
	Items  []postgres.Template
	Total  int64
	Cursor int64
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	items, err := s.store.ListByProject(ctx, in.ProjectID, in.Cursor, in.Limit)
	if err != nil {
		return nil, err
	}
	total, err := s.store.CountByProject(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}
	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return &ListResult{Items: items, Total: total, Cursor: nextCursor}, nil
}

type UpdateInput struct {
	ID          int64
	Name        string
	Description *string
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*postgres.Template, error) {
	t, err := s.store.Update(ctx, in.ID, in.Name, in.Description)
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.store.SoftDelete(ctx, id)
}

// ── TemplateVersion ───────────────────────────────────────────────────────────

type PublishVersionInput struct {
	TemplateID       int64
	ContentHTML      *string
	ContentNginx     *string
	DefaultVariables map[string]any
	PublishedBy      int64
}

// PublishVersion creates a new template_version row and immediately marks it
// published. A new version is always a new row — existing versions are never
// mutated (Critical Rule #9).
//
// The version_label is auto-generated as "v<unix_timestamp>" for P1.
// The checksum covers: content_html + content_nginx + default_variables JSON (sorted keys).
func (s *Service) PublishVersion(ctx context.Context, in PublishVersionInput) (*postgres.TemplateVersion, error) {
	// Warn if {{ .ReleaseVersion }} is missing from HTML (will be hard error in P3)
	if in.ContentHTML != nil && !strings.Contains(*in.ContentHTML, "{{ .ReleaseVersion }}") {
		s.logger.Warn("template HTML missing {{ .ReleaseVersion }} probe tag",
			zap.Int64("template_id", in.TemplateID))
	}

	// Serialize default_variables to JSON (nil → "{}")
	var varJSON []byte
	if len(in.DefaultVariables) > 0 {
		b, err := json.Marshal(in.DefaultVariables)
		if err != nil {
			return nil, fmt.Errorf("marshal default_variables: %w", err)
		}
		varJSON = b
	} else {
		varJSON = []byte("{}")
	}

	// Compute checksum over content + variables
	checksum := computeVersionChecksum(in.ContentHTML, in.ContentNginx, varJSON)

	// Auto-generate version label
	label := fmt.Sprintf("v%d", time.Now().UnixMilli())

	v := &postgres.TemplateVersion{
		TemplateID:       in.TemplateID,
		VersionLabel:     label,
		ContentHTML:      in.ContentHTML,
		ContentNginx:     in.ContentNginx,
		DefaultVariables: varJSON,
		Checksum:         checksum,
		CreatedBy:        &in.PublishedBy,
	}

	created, err := s.store.CreateVersion(ctx, v)
	if err != nil {
		return nil, fmt.Errorf("create version: %w", err)
	}

	published, err := s.store.Publish(ctx, created.ID, in.PublishedBy)
	if err != nil {
		return nil, fmt.Errorf("publish version: %w", err)
	}

	s.logger.Info("template version published",
		zap.Int64("template_id", in.TemplateID),
		zap.Int64("version_id", published.ID),
		zap.String("label", published.VersionLabel),
		zap.String("checksum", published.Checksum),
	)
	return published, nil
}

func (s *Service) GetVersion(ctx context.Context, id int64) (*postgres.TemplateVersion, error) {
	return s.store.GetVersion(ctx, id)
}

func (s *Service) ListVersions(ctx context.Context, templateID int64) ([]postgres.TemplateVersion, error) {
	return s.store.ListVersions(ctx, templateID)
}

// UpdateVersion allows editing an unpublished version's content.
// Returns ErrVersionImmutable if the version is already published.
type UpdateVersionInput struct {
	ID               int64
	ContentHTML      *string
	ContentNginx     *string
	DefaultVariables map[string]any
}

func (s *Service) UpdateVersion(ctx context.Context, in UpdateVersionInput) (*postgres.TemplateVersion, error) {
	var varJSON []byte
	if len(in.DefaultVariables) > 0 {
		b, err := json.Marshal(in.DefaultVariables)
		if err != nil {
			return nil, fmt.Errorf("marshal default_variables: %w", err)
		}
		varJSON = b
	} else {
		varJSON = []byte("{}")
	}
	checksum := computeVersionChecksum(in.ContentHTML, in.ContentNginx, varJSON)
	v, err := s.store.UpdateVersion(ctx, in.ID, in.ContentHTML, in.ContentNginx, varJSON, checksum)
	if errors.Is(err, postgres.ErrVersionImmutable) {
		return nil, ErrVersionImmutable
	}
	return v, err
}

// computeVersionChecksum returns sha256(content_html + "|" + content_nginx + "|" + variables_json).
func computeVersionChecksum(contentHTML, contentNginx *string, varJSON []byte) string {
	h := sha256.New()
	if contentHTML != nil {
		h.Write([]byte(*contentHTML))
	}
	h.Write([]byte("|"))
	if contentNginx != nil {
		h.Write([]byte(*contentNginx))
	}
	h.Write([]byte("|"))
	h.Write(varJSON)
	return fmt.Sprintf("%x", h.Sum(nil))
}
