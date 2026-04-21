package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrDNSTemplateNotFound = errors.New("dns record template not found")

// TemplateRecord is one record entry inside a dns_record_templates.records JSONB array.
type TemplateRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority,omitempty"`
}

// DNSRecordTemplate mirrors the dns_record_templates table row.
type DNSRecordTemplate struct {
	ID          int64           `db:"id"`
	UUID        string          `db:"uuid"`
	Name        string          `db:"name"`
	Description *string         `db:"description"`
	Records     json.RawMessage `db:"records"`
	Variables   json.RawMessage `db:"variables"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// DNSTemplateStore is the data-access object for dns_record_templates.
type DNSTemplateStore struct {
	db *sqlx.DB
}

// NewDNSTemplateStore creates a new DNSTemplateStore.
func NewDNSTemplateStore(db *sqlx.DB) *DNSTemplateStore {
	return &DNSTemplateStore{db: db}
}

// Create inserts a new template and returns it with generated id/uuid/timestamps.
func (s *DNSTemplateStore) Create(ctx context.Context, name string, description *string, records, variables json.RawMessage) (*DNSRecordTemplate, error) {
	const q = `
		INSERT INTO dns_record_templates (name, description, records, variables, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		RETURNING id, uuid, name, description, records, variables, created_at, updated_at`
	var t DNSRecordTemplate
	if err := s.db.QueryRowxContext(ctx, q, name, description, records, variables).StructScan(&t); err != nil {
		return nil, fmt.Errorf("create dns record template: %w", err)
	}
	return &t, nil
}

// GetByID fetches a template by its primary key.
func (s *DNSTemplateStore) GetByID(ctx context.Context, id int64) (*DNSRecordTemplate, error) {
	const q = `SELECT id, uuid, name, description, records, variables, created_at, updated_at
	           FROM dns_record_templates WHERE id = $1`
	var t DNSRecordTemplate
	if err := s.db.GetContext(ctx, &t, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDNSTemplateNotFound
		}
		return nil, fmt.Errorf("get dns record template: %w", err)
	}
	return &t, nil
}

// List returns all templates ordered by name.
func (s *DNSTemplateStore) List(ctx context.Context) ([]DNSRecordTemplate, error) {
	const q = `SELECT id, uuid, name, description, records, variables, created_at, updated_at
	           FROM dns_record_templates ORDER BY name ASC`
	var rows []DNSRecordTemplate
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, fmt.Errorf("list dns record templates: %w", err)
	}
	return rows, nil
}

// Update replaces the mutable fields of a template.
func (s *DNSTemplateStore) Update(ctx context.Context, id int64, name string, description *string, records, variables json.RawMessage) (*DNSRecordTemplate, error) {
	const q = `
		UPDATE dns_record_templates
		SET name = $2, description = $3, records = $4, variables = $5, updated_at = NOW()
		WHERE id = $1
		RETURNING id, uuid, name, description, records, variables, created_at, updated_at`
	var t DNSRecordTemplate
	if err := s.db.QueryRowxContext(ctx, q, id, name, description, records, variables).StructScan(&t); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDNSTemplateNotFound
		}
		return nil, fmt.Errorf("update dns record template: %w", err)
	}
	return &t, nil
}

// Delete removes a template by ID. Returns ErrDNSTemplateNotFound if not present.
func (s *DNSTemplateStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dns_record_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete dns record template: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDNSTemplateNotFound
	}
	return nil
}
