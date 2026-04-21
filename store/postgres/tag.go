package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Tag struct {
	ID    int64   `db:"id"`
	Name  string  `db:"name"`
	Color *string `db:"color"`
}

var ErrTagNotFound = errors.New("tag not found")

type TagStore struct {
	db *sqlx.DB
}

func NewTagStore(db *sqlx.DB) *TagStore {
	return &TagStore{db: db}
}

func (s *TagStore) Create(ctx context.Context, t *Tag) (*Tag, error) {
	var out Tag
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO tags (name, color) VALUES ($1, $2) RETURNING id, name, color`,
		t.Name, t.Color,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return &out, nil
}

func (s *TagStore) GetByID(ctx context.Context, id int64) (*Tag, error) {
	var t Tag
	err := s.db.GetContext(ctx, &t, `SELECT id, name, color FROM tags WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tag: %w", err)
	}
	return &t, nil
}

func (s *TagStore) List(ctx context.Context) ([]Tag, error) {
	var tags []Tag
	err := s.db.SelectContext(ctx, &tags, `SELECT id, name, color FROM tags ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tags, nil
}

func (s *TagStore) Update(ctx context.Context, t *Tag) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tags SET name = $2, color = $3 WHERE id = $1`, t.ID, t.Name, t.Color)
	if err != nil {
		return fmt.Errorf("update tag: %w", err)
	}
	return nil
}

func (s *TagStore) Delete(ctx context.Context, id int64) error {
	// domain_tags rows cascade-deleted via ON DELETE CASCADE
	_, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// --- Domain-Tag associations ---

// SetDomainTags replaces all tags for a domain with the given set.
func (s *TagStore) SetDomainTags(ctx context.Context, domainID int64, tagIDs []int64) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Remove existing tags
	_, err = tx.ExecContext(ctx, `DELETE FROM domain_tags WHERE domain_id = $1`, domainID)
	if err != nil {
		return fmt.Errorf("clear domain tags: %w", err)
	}

	// Insert new tags
	if len(tagIDs) > 0 {
		stmt, err := tx.PreparexContext(ctx, `INSERT INTO domain_tags (domain_id, tag_id) VALUES ($1, $2)`)
		if err != nil {
			return fmt.Errorf("prepare domain tag insert: %w", err)
		}
		defer stmt.Close()
		for _, tagID := range tagIDs {
			_, err = stmt.ExecContext(ctx, domainID, tagID)
			if err != nil {
				return fmt.Errorf("insert domain tag %d: %w", tagID, err)
			}
		}
	}

	return tx.Commit()
}

// GetDomainTags returns all tags for a domain.
func (s *TagStore) GetDomainTags(ctx context.Context, domainID int64) ([]Tag, error) {
	var tags []Tag
	err := s.db.SelectContext(ctx, &tags,
		`SELECT t.id, t.name, t.color
		 FROM tags t JOIN domain_tags dt ON t.id = dt.tag_id
		 WHERE dt.domain_id = $1
		 ORDER BY t.name ASC`, domainID)
	if err != nil {
		return nil, fmt.Errorf("get domain tags: %w", err)
	}
	return tags, nil
}

// TagWithCount includes the number of domains using this tag.
type TagWithCount struct {
	Tag
	DomainCount int64 `db:"domain_count"`
}

// ListWithCounts returns all tags with their associated domain counts.
func (s *TagStore) ListWithCounts(ctx context.Context) ([]TagWithCount, error) {
	var tags []TagWithCount
	err := s.db.SelectContext(ctx, &tags,
		`SELECT t.id, t.name, t.color, COUNT(dt.domain_id) AS domain_count
		 FROM tags t
		 LEFT JOIN domain_tags dt ON t.id = dt.tag_id
		 GROUP BY t.id, t.name, t.color
		 ORDER BY t.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list tags with counts: %w", err)
	}
	return tags, nil
}
