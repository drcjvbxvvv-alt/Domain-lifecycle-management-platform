package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Template maps to the templates table.
type Template struct {
	ID          int64      `db:"id"`
	UUID        string     `db:"uuid"`
	ProjectID   int64      `db:"project_id"`
	Name        string     `db:"name"`
	Description *string    `db:"description"`
	Kind        string     `db:"kind"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	DeletedAt   *time.Time `db:"deleted_at"`
}

// TemplateVersion maps to the template_versions table.
// Immutable once published_at IS NOT NULL (Critical Rule #9).
type TemplateVersion struct {
	ID               int64      `db:"id"`
	UUID             string     `db:"uuid"`
	TemplateID       int64      `db:"template_id"`
	VersionLabel     string     `db:"version_label"`
	ContentHTML      *string    `db:"content_html"`
	ContentNginx     *string    `db:"content_nginx"`
	DefaultVariables []byte     `db:"default_variables"` // raw JSONB
	Checksum         string     `db:"checksum"`
	PublishedAt      *time.Time `db:"published_at"`
	PublishedBy      *int64     `db:"published_by"`
	CreatedAt        time.Time  `db:"created_at"`
	CreatedBy        *int64     `db:"created_by"`
}

var (
	ErrTemplateNotFound        = errors.New("template not found")
	ErrTemplateVersionNotFound = errors.New("template version not found")
	ErrVersionImmutable        = errors.New("template version is published and immutable")
)

type TemplateStore struct {
	db *sqlx.DB
}

func NewTemplateStore(db *sqlx.DB) *TemplateStore {
	return &TemplateStore{db: db}
}

// ── Template CRUD ─────────────────────────────────────────────────────────────

func (s *TemplateStore) Create(ctx context.Context, t *Template) (*Template, error) {
	var out Template
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO templates (project_id, name, description, kind)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, uuid, project_id, name, description, kind, created_at, updated_at, deleted_at`,
		t.ProjectID, t.Name, t.Description, t.Kind).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return &out, nil
}

func (s *TemplateStore) GetByID(ctx context.Context, id int64) (*Template, error) {
	var t Template
	err := s.db.GetContext(ctx, &t,
		`SELECT id, uuid, project_id, name, description, kind, created_at, updated_at, deleted_at
		 FROM templates WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template by id: %w", err)
	}
	return &t, nil
}

func (s *TemplateStore) ListByProject(ctx context.Context, projectID int64, cursor int64, limit int) ([]Template, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var items []Template
	err := s.db.SelectContext(ctx, &items,
		`SELECT id, uuid, project_id, name, description, kind, created_at, updated_at, deleted_at
		 FROM templates
		 WHERE project_id = $1 AND deleted_at IS NULL AND id > $2
		 ORDER BY id ASC LIMIT $3`, projectID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list templates by project: %w", err)
	}
	return items, nil
}

func (s *TemplateStore) CountByProject(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM templates WHERE project_id = $1 AND deleted_at IS NULL`, projectID)
	if err != nil {
		return 0, fmt.Errorf("count templates: %w", err)
	}
	return count, nil
}

func (s *TemplateStore) Update(ctx context.Context, id int64, name string, description *string) (*Template, error) {
	var out Template
	err := s.db.QueryRowxContext(ctx,
		`UPDATE templates SET name = $1, description = $2, updated_at = NOW()
		 WHERE id = $3 AND deleted_at IS NULL
		 RETURNING id, uuid, project_id, name, description, kind, created_at, updated_at, deleted_at`,
		name, description, id).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return &out, nil
}

func (s *TemplateStore) SoftDelete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE templates SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// ── TemplateVersion CRUD ──────────────────────────────────────────────────────

// CreateVersion inserts a new (unpublished) template version.
func (s *TemplateStore) CreateVersion(ctx context.Context, v *TemplateVersion) (*TemplateVersion, error) {
	var out TemplateVersion
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO template_versions
		   (template_id, version_label, content_html, content_nginx, default_variables, checksum, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, uuid, template_id, version_label, content_html, content_nginx,
		           default_variables, checksum, published_at, published_by, created_at, created_by`,
		v.TemplateID, v.VersionLabel, v.ContentHTML, v.ContentNginx,
		v.DefaultVariables, v.Checksum, v.CreatedBy).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create template version: %w", err)
	}
	return &out, nil
}

// GetVersion fetches a single template version by ID.
func (s *TemplateStore) GetVersion(ctx context.Context, id int64) (*TemplateVersion, error) {
	var v TemplateVersion
	err := s.db.GetContext(ctx, &v,
		`SELECT id, uuid, template_id, version_label, content_html, content_nginx,
		        default_variables, checksum, published_at, published_by, created_at, created_by
		 FROM template_versions WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template version: %w", err)
	}
	return &v, nil
}

// ListVersions returns all versions for a template, newest first.
func (s *TemplateStore) ListVersions(ctx context.Context, templateID int64) ([]TemplateVersion, error) {
	var items []TemplateVersion
	err := s.db.SelectContext(ctx, &items,
		`SELECT id, uuid, template_id, version_label, content_html, content_nginx,
		        default_variables, checksum, published_at, published_by, created_at, created_by
		 FROM template_versions
		 WHERE template_id = $1
		 ORDER BY created_at DESC`, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template versions: %w", err)
	}
	return items, nil
}

// Publish sets published_at = NOW() on a version. Idempotent check: if already
// published, returns ErrVersionImmutable — callers must never re-publish.
func (s *TemplateStore) Publish(ctx context.Context, versionID int64, publishedBy int64) (*TemplateVersion, error) {
	// Check current state first
	v, err := s.GetVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if v.PublishedAt != nil {
		return nil, ErrVersionImmutable
	}

	var out TemplateVersion
	err = s.db.QueryRowxContext(ctx,
		`UPDATE template_versions
		 SET published_at = NOW(), published_by = $1
		 WHERE id = $2 AND published_at IS NULL
		 RETURNING id, uuid, template_id, version_label, content_html, content_nginx,
		           default_variables, checksum, published_at, published_by, created_at, created_by`,
		publishedBy, versionID).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		// Another concurrent call published it between our read and update
		return nil, ErrVersionImmutable
	}
	if err != nil {
		return nil, fmt.Errorf("publish template version: %w", err)
	}
	return &out, nil
}

// UpdateVersion rejects updates to published versions (immutability enforcement).
// Only unpublished versions may be updated.
func (s *TemplateStore) UpdateVersion(ctx context.Context, id int64, contentHTML, contentNginx *string, defaultVariables []byte, checksum string) (*TemplateVersion, error) {
	// Immutability guard — CLAUDE.md Critical Rule #9
	var publishedAt *time.Time
	err := s.db.GetContext(ctx, &publishedAt,
		`SELECT published_at FROM template_versions WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("check version immutability: %w", err)
	}
	if publishedAt != nil {
		return nil, ErrVersionImmutable
	}

	var out TemplateVersion
	err = s.db.QueryRowxContext(ctx,
		`UPDATE template_versions
		 SET content_html = $1, content_nginx = $2, default_variables = $3, checksum = $4
		 WHERE id = $5 AND published_at IS NULL
		 RETURNING id, uuid, template_id, version_label, content_html, content_nginx,
		           default_variables, checksum, published_at, published_by, created_at, created_by`,
		contentHTML, contentNginx, defaultVariables, checksum, id).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVersionImmutable
	}
	if err != nil {
		return nil, fmt.Errorf("update template version: %w", err)
	}
	return &out, nil
}
