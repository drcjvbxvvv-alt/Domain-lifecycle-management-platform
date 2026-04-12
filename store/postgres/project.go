package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type Project struct {
	ID          int64      `db:"id"`
	UUID        string     `db:"uuid"`
	Name        string     `db:"name"`
	Slug        string     `db:"slug"`
	Description *string    `db:"description"`
	IsProd      bool       `db:"is_prod"`
	OwnerID     *int64     `db:"owner_id"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	DeletedAt   *time.Time `db:"deleted_at"`
}

var ErrProjectNotFound = errors.New("project not found")

type ProjectStore struct {
	db *sqlx.DB
}

func NewProjectStore(db *sqlx.DB) *ProjectStore {
	return &ProjectStore{db: db}
}

func (s *ProjectStore) Create(ctx context.Context, p *Project) (*Project, error) {
	var out Project
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO projects (name, slug, description, is_prod, owner_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, uuid, name, slug, description, is_prod, owner_id, created_at, updated_at, deleted_at`,
		p.Name, p.Slug, p.Description, p.IsProd, p.OwnerID).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return &out, nil
}

func (s *ProjectStore) GetByID(ctx context.Context, id int64) (*Project, error) {
	var p Project
	err := s.db.GetContext(ctx, &p,
		`SELECT id, uuid, name, slug, description, is_prod, owner_id, created_at, updated_at, deleted_at
		 FROM projects WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project by id: %w", err)
	}
	return &p, nil
}

func (s *ProjectStore) GetBySlug(ctx context.Context, slug string) (*Project, error) {
	var p Project
	err := s.db.GetContext(ctx, &p,
		`SELECT id, uuid, name, slug, description, is_prod, owner_id, created_at, updated_at, deleted_at
		 FROM projects WHERE slug = $1 AND deleted_at IS NULL`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project by slug: %w", err)
	}
	return &p, nil
}

// List returns projects with cursor-based pagination.
// cursor is the last seen project ID (0 for first page). limit defaults to 20.
func (s *ProjectStore) List(ctx context.Context, cursor int64, limit int) ([]Project, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var projects []Project
	err := s.db.SelectContext(ctx, &projects,
		`SELECT id, uuid, name, slug, description, is_prod, owner_id, created_at, updated_at, deleted_at
		 FROM projects
		 WHERE deleted_at IS NULL AND id > $1
		 ORDER BY id ASC
		 LIMIT $2`, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

func (s *ProjectStore) Update(ctx context.Context, id int64, name, slug string, description *string, isProd bool) (*Project, error) {
	var p Project
	err := s.db.QueryRowxContext(ctx,
		`UPDATE projects SET name = $2, slug = $3, description = $4, is_prod = $5, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING id, uuid, name, slug, description, is_prod, owner_id, created_at, updated_at, deleted_at`,
		id, name, slug, description, isProd).StructScan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return &p, nil
}

func (s *ProjectStore) SoftDelete(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE projects SET deleted_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete project: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrProjectNotFound
	}
	return nil
}

// Count returns the total number of non-deleted projects.
func (s *ProjectStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM projects WHERE deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("count projects: %w", err)
	}
	return count, nil
}
