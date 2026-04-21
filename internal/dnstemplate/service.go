// Package dnstemplate manages DNS record templates with {{variable}} substitution.
// Templates are reusable blueprints that can be applied to a domain to produce
// a set of staged records for the plan/apply workflow.
package dnstemplate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

var (
	ErrTemplateNotFound   = errors.New("dns template not found")
	ErrMissingVariable    = errors.New("missing required variable")
	ErrInvalidTemplate    = errors.New("invalid template")
)

// varPattern matches {{variable_name}} placeholders.
var varPattern = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

// TemplateStore is the subset of DNSTemplateStore used by this service.
type TemplateStore interface {
	Create(ctx context.Context, name string, description *string, records, variables json.RawMessage) (*postgres.DNSRecordTemplate, error)
	GetByID(ctx context.Context, id int64) (*postgres.DNSRecordTemplate, error)
	List(ctx context.Context) ([]postgres.DNSRecordTemplate, error)
	Update(ctx context.Context, id int64, name string, description *string, records, variables json.RawMessage) (*postgres.DNSRecordTemplate, error)
	Delete(ctx context.Context, id int64) error
}

// RenderedRecord is one record after variable substitution.
type RenderedRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority,omitempty"`
}

// Service manages DNS record templates.
type Service struct {
	store  TemplateStore
	logger *zap.Logger
}

// NewService constructs a Service.
func NewService(store TemplateStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

// CreateInput is the validated input for creating a template.
type CreateInput struct {
	Name        string
	Description *string
	Records     []postgres.TemplateRecord
	Variables   map[string]string // name → description/default
}

// Create creates a new DNS record template.
func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.DNSRecordTemplate, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidTemplate)
	}

	recsJSON, err := json.Marshal(in.Records)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal records: %v", ErrInvalidTemplate, err)
	}

	// Auto-extract variables from records if not provided
	if in.Variables == nil {
		in.Variables = ExtractVariables(in.Records)
	}
	varsJSON, err := json.Marshal(in.Variables)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal variables: %v", ErrInvalidTemplate, err)
	}

	t, err := s.store.Create(ctx, in.Name, in.Description, recsJSON, varsJSON)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	s.logger.Info("dns template created", zap.String("name", t.Name), zap.Int64("id", t.ID))
	return t, nil
}

// Get fetches a template by ID.
func (s *Service) Get(ctx context.Context, id int64) (*postgres.DNSRecordTemplate, error) {
	t, err := s.store.GetByID(ctx, id)
	if errors.Is(err, postgres.ErrDNSTemplateNotFound) {
		return nil, ErrTemplateNotFound
	}
	return t, err
}

// List returns all templates.
func (s *Service) List(ctx context.Context) ([]postgres.DNSRecordTemplate, error) {
	return s.store.List(ctx)
}

// UpdateInput is the input for updating a template.
type UpdateInput = CreateInput

// Update replaces a template's mutable fields.
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (*postgres.DNSRecordTemplate, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidTemplate)
	}

	recsJSON, err := json.Marshal(in.Records)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal records: %v", ErrInvalidTemplate, err)
	}
	if in.Variables == nil {
		in.Variables = ExtractVariables(in.Records)
	}
	varsJSON, err := json.Marshal(in.Variables)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal variables: %v", ErrInvalidTemplate, err)
	}

	t, err := s.store.Update(ctx, id, in.Name, in.Description, recsJSON, varsJSON)
	if errors.Is(err, postgres.ErrDNSTemplateNotFound) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	s.logger.Info("dns template updated", zap.Int64("id", id))
	return t, nil
}

// Delete removes a template.
func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.store.Delete(ctx, id)
	if errors.Is(err, postgres.ErrDNSTemplateNotFound) {
		return ErrTemplateNotFound
	}
	return err
}

// ── Apply ─────────────────────────────────────────────────────────────────────

// ApplyTemplate renders a template's records by substituting all {{var}} placeholders.
// Returns ErrMissingVariable if any placeholder is not covered by provided vars.
func (s *Service) ApplyTemplate(ctx context.Context, id int64, provided map[string]string) ([]RenderedRecord, error) {
	tmpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	var records []postgres.TemplateRecord
	if err := json.Unmarshal(tmpl.Records, &records); err != nil {
		return nil, fmt.Errorf("%w: unmarshal records: %v", ErrInvalidTemplate, err)
	}

	return renderRecords(records, provided)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// ExtractVariables scans all TemplateRecord fields for {{var}} patterns
// and returns a map of var_name → "" (empty default).
func ExtractVariables(records []postgres.TemplateRecord) map[string]string {
	seen := make(map[string]string)
	for _, r := range records {
		for _, m := range varPattern.FindAllStringSubmatch(r.Name, -1) {
			seen[m[1]] = ""
		}
		for _, m := range varPattern.FindAllStringSubmatch(r.Content, -1) {
			seen[m[1]] = ""
		}
	}
	return seen
}

// renderRecords applies variable substitution to every record.
func renderRecords(records []postgres.TemplateRecord, vars map[string]string) ([]RenderedRecord, error) {
	// Check all required variables are provided
	required := ExtractVariables(records)
	var missing []string
	for k := range required {
		if _, ok := vars[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissingVariable, strings.Join(missing, ", "))
	}

	out := make([]RenderedRecord, 0, len(records))
	for _, r := range records {
		out = append(out, RenderedRecord{
			Name:     substitute(r.Name, vars),
			Type:     strings.ToUpper(r.Type),
			Content:  substitute(r.Content, vars),
			TTL:      r.TTL,
			Priority: r.Priority,
		})
	}
	return out, nil
}

// substitute replaces all {{key}} in s with values from vars.
func substitute(s string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-2] // strip {{ and }}
		if val, ok := vars[key]; ok {
			return val
		}
		return match // leave unreplaced if not found (shouldn't happen after validation)
	})
}
