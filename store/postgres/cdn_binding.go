package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ── Sentinel errors ────────────────────────────────────────────────────────────

var (
	ErrCDNBindingNotFound      = errors.New("cdn binding not found")
	ErrCDNBindingAlreadyExists = errors.New("cdn binding already exists for this domain and account")
	ErrCDNConfigNotFound       = errors.New("cdn domain config not found")
	ErrCDNContentTaskNotFound  = errors.New("cdn content task not found")
)

// ── Model structs ──────────────────────────────────────────────────────────────

// DomainCDNBinding maps to the domain_cdn_bindings table.
// It records the link between a platform domain and a CDN account, and stores
// the CNAME that the CDN provider assigned so DNS can be pointed at the CDN.
type DomainCDNBinding struct {
	ID            int64      `db:"id"`
	UUID          string     `db:"uuid"`
	DomainID      int64      `db:"domain_id"`
	CDNAccountID  int64      `db:"cdn_account_id"`
	CDNCNAME      *string    `db:"cdn_cname"`
	BusinessType  string     `db:"business_type"`
	Status        string     `db:"status"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	DeletedAt     *time.Time `db:"deleted_at"`
}

// CDNDomainConfig maps to the cdn_domain_configs table.
// Each row stores a JSONB snapshot of one configuration category
// (cache | origin | access_control | https | performance) for a binding.
type CDNDomainConfig struct {
	ID         int64           `db:"id"`
	BindingID  int64           `db:"binding_id"`
	ConfigType string          `db:"config_type"`
	Config     json.RawMessage `db:"config"`
	SyncedAt   *time.Time      `db:"synced_at"`
	UpdatedAt  time.Time       `db:"updated_at"`
}

// CDNContentTask maps to the cdn_content_tasks table.
// Represents a single purge or prefetch operation submitted to a CDN provider.
type CDNContentTask struct {
	ID             int64      `db:"id"`
	UUID           string     `db:"uuid"`
	BindingID      int64      `db:"binding_id"`
	TaskType       string     `db:"task_type"`
	ProviderTaskID *string    `db:"provider_task_id"`
	Status         string     `db:"status"`
	Targets        pq.StringArray `db:"targets"`
	CreatedBy      *int64     `db:"created_by"`
	CreatedAt      time.Time  `db:"created_at"`
	CompletedAt    *time.Time `db:"completed_at"`
}

// ── Column lists ───────────────────────────────────────────────────────────────

const bindingCols = `id, uuid, domain_id, cdn_account_id, cdn_cname,
	business_type, status, created_at, updated_at, deleted_at`

const configCols = `id, binding_id, config_type, config, synced_at, updated_at`

const contentTaskCols = `id, uuid, binding_id, task_type, provider_task_id,
	status, targets, created_by, created_at, completed_at`

// ── Store ──────────────────────────────────────────────────────────────────────

// CDNBindingStore handles persistence for domain_cdn_bindings, cdn_domain_configs,
// and cdn_content_tasks.
type CDNBindingStore struct {
	db *sqlx.DB
}

// NewCDNBindingStore creates a CDNBindingStore backed by db.
func NewCDNBindingStore(db *sqlx.DB) *CDNBindingStore {
	return &CDNBindingStore{db: db}
}

// ── domain_cdn_bindings CRUD ──────────────────────────────────────────────────

// CreateBinding inserts a new CDN domain binding.
// Returns ErrCDNBindingAlreadyExists if an active binding already exists for
// the same (domain_id, cdn_account_id) pair.
func (s *CDNBindingStore) CreateBinding(ctx context.Context, b *DomainCDNBinding) (*DomainCDNBinding, error) {
	var out DomainCDNBinding
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO domain_cdn_bindings
		    (domain_id, cdn_account_id, cdn_cname, business_type, status)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+bindingCols,
		b.DomainID, b.CDNAccountID, b.CDNCNAME, b.BusinessType, b.Status,
	).StructScan(&out)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCDNBindingAlreadyExists
		}
		return nil, fmt.Errorf("create cdn binding: %w", err)
	}
	return &out, nil
}

// GetBindingByID returns a single active binding by its primary key.
func (s *CDNBindingStore) GetBindingByID(ctx context.Context, id int64) (*DomainCDNBinding, error) {
	var b DomainCDNBinding
	err := s.db.GetContext(ctx, &b,
		`SELECT `+bindingCols+`
		 FROM domain_cdn_bindings
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cdn binding by id: %w", err)
	}
	return &b, nil
}

// GetBindingByDomainAndAccount returns the active binding for a specific
// (domain_id, cdn_account_id) pair.
func (s *CDNBindingStore) GetBindingByDomainAndAccount(ctx context.Context, domainID, accountID int64) (*DomainCDNBinding, error) {
	var b DomainCDNBinding
	err := s.db.GetContext(ctx, &b,
		`SELECT `+bindingCols+`
		 FROM domain_cdn_bindings
		 WHERE domain_id = $1 AND cdn_account_id = $2 AND deleted_at IS NULL`,
		domainID, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cdn binding by domain and account: %w", err)
	}
	return &b, nil
}

// ListBindingsByDomain returns all active bindings for a domain, ordered by
// creation time ascending.
func (s *CDNBindingStore) ListBindingsByDomain(ctx context.Context, domainID int64) ([]DomainCDNBinding, error) {
	var bindings []DomainCDNBinding
	err := s.db.SelectContext(ctx, &bindings,
		`SELECT `+bindingCols+`
		 FROM domain_cdn_bindings
		 WHERE domain_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at ASC`, domainID)
	if err != nil {
		return nil, fmt.Errorf("list cdn bindings by domain: %w", err)
	}
	return bindings, nil
}

// UpdateBindingStatus updates the status and CDN CNAME of an existing binding.
// cname may be empty string (stored as NULL when empty).
func (s *CDNBindingStore) UpdateBindingStatus(ctx context.Context, id int64, status, cname string) (*DomainCDNBinding, error) {
	var cnamePtr *string
	if cname != "" {
		cnamePtr = &cname
	}
	var out DomainCDNBinding
	err := s.db.QueryRowxContext(ctx,
		`UPDATE domain_cdn_bindings
		 SET status = $2, cdn_cname = $3, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING `+bindingCols,
		id, status, cnamePtr,
	).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update cdn binding status: %w", err)
	}
	return &out, nil
}

// SoftDeleteBinding marks a binding as deleted (deleted_at = NOW()).
func (s *CDNBindingStore) SoftDeleteBinding(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE domain_cdn_bindings
		 SET deleted_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete cdn binding: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCDNBindingNotFound
	}
	return nil
}

// ── cdn_domain_configs ────────────────────────────────────────────────────────

// UpsertConfig creates or replaces the config snapshot for a (binding, config_type) pair.
// synced_at is set to NOW() on every call.
func (s *CDNBindingStore) UpsertConfig(ctx context.Context, c *CDNDomainConfig) (*CDNDomainConfig, error) {
	if c.Config == nil {
		c.Config = json.RawMessage(`{}`)
	}
	var out CDNDomainConfig
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO cdn_domain_configs (binding_id, config_type, config, synced_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT ON CONSTRAINT uq_cdn_domain_config
		 DO UPDATE SET
		     config    = EXCLUDED.config,
		     synced_at = NOW(),
		     updated_at = NOW()
		 RETURNING `+configCols,
		c.BindingID, c.ConfigType, c.Config,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("upsert cdn domain config: %w", err)
	}
	return &out, nil
}

// GetConfig retrieves the config snapshot for a (binding, config_type) pair.
func (s *CDNBindingStore) GetConfig(ctx context.Context, bindingID int64, configType string) (*CDNDomainConfig, error) {
	var c CDNDomainConfig
	err := s.db.GetContext(ctx, &c,
		`SELECT `+configCols+`
		 FROM cdn_domain_configs
		 WHERE binding_id = $1 AND config_type = $2`,
		bindingID, configType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cdn domain config: %w", err)
	}
	return &c, nil
}

// ListConfigs returns all config snapshots for a binding, ordered by config_type.
func (s *CDNBindingStore) ListConfigs(ctx context.Context, bindingID int64) ([]CDNDomainConfig, error) {
	var configs []CDNDomainConfig
	err := s.db.SelectContext(ctx, &configs,
		`SELECT `+configCols+`
		 FROM cdn_domain_configs
		 WHERE binding_id = $1
		 ORDER BY config_type ASC`, bindingID)
	if err != nil {
		return nil, fmt.Errorf("list cdn domain configs: %w", err)
	}
	return configs, nil
}

// ── cdn_content_tasks ─────────────────────────────────────────────────────────

// CreateContentTask inserts a new CDN content task (purge or prefetch).
func (s *CDNBindingStore) CreateContentTask(ctx context.Context, t *CDNContentTask) (*CDNContentTask, error) {
	if t.Targets == nil {
		t.Targets = pq.StringArray{}
	}
	var out CDNContentTask
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO cdn_content_tasks
		    (binding_id, task_type, provider_task_id, status, targets, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+contentTaskCols,
		t.BindingID, t.TaskType, t.ProviderTaskID,
		t.Status, t.Targets, t.CreatedBy,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create cdn content task: %w", err)
	}
	return &out, nil
}

// GetContentTaskByID returns a single content task by its primary key.
func (s *CDNBindingStore) GetContentTaskByID(ctx context.Context, id int64) (*CDNContentTask, error) {
	var t CDNContentTask
	err := s.db.GetContext(ctx, &t,
		`SELECT `+contentTaskCols+`
		 FROM cdn_content_tasks WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNContentTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cdn content task: %w", err)
	}
	return &t, nil
}

// UpdateContentTaskStatus updates the status (and optionally completed_at) of a task.
func (s *CDNBindingStore) UpdateContentTaskStatus(ctx context.Context, id int64, status string, providerTaskID *string) (*CDNContentTask, error) {
	var completedAt *time.Time
	if status == "done" || status == "failed" {
		now := time.Now()
		completedAt = &now
	}
	var out CDNContentTask
	err := s.db.QueryRowxContext(ctx,
		`UPDATE cdn_content_tasks
		 SET status = $2, provider_task_id = COALESCE($3, provider_task_id),
		     completed_at = $4
		 WHERE id = $1
		 RETURNING `+contentTaskCols,
		id, status, providerTaskID, completedAt,
	).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCDNContentTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update cdn content task status: %w", err)
	}
	return &out, nil
}

// ListContentTasksByBinding returns all tasks for a binding, newest first.
func (s *CDNBindingStore) ListContentTasksByBinding(ctx context.Context, bindingID int64) ([]CDNContentTask, error) {
	var tasks []CDNContentTask
	err := s.db.SelectContext(ctx, &tasks,
		`SELECT `+contentTaskCols+`
		 FROM cdn_content_tasks
		 WHERE binding_id = $1
		 ORDER BY created_at DESC`, bindingID)
	if err != nil {
		return nil, fmt.Errorf("list cdn content tasks: %w", err)
	}
	return tasks, nil
}
