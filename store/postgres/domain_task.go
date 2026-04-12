package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// DomainTask maps to the domain_tasks table row.
type DomainTask struct {
	ID          int64      `db:"id"`
	UUID        string     `db:"uuid"`
	ReleaseID   int64      `db:"release_id"`
	ShardID     *int64     `db:"shard_id"`
	DomainID    int64      `db:"domain_id"`
	HostGroupID *int64     `db:"host_group_id"`
	TaskType    string     `db:"task_type"`
	Status      string     `db:"status"`
	StartedAt   *time.Time `db:"started_at"`
	EndedAt     *time.Time `db:"ended_at"`
	LastError   *string    `db:"last_error"`
	CreatedAt   time.Time  `db:"created_at"`
}

// DomainTaskStore handles domain_tasks persistence.
type DomainTaskStore struct {
	db *sqlx.DB
}

func NewDomainTaskStore(db *sqlx.DB) *DomainTaskStore {
	return &DomainTaskStore{db: db}
}

const domainTaskColumns = `id, uuid, release_id, shard_id, domain_id, host_group_id,
	task_type, status, started_at, ended_at, last_error, created_at`

// CreateBatch inserts multiple domain_tasks in a single statement.
func (s *DomainTaskStore) CreateBatch(ctx context.Context, tasks []DomainTask) ([]DomainTask, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	query := `INSERT INTO domain_tasks (release_id, shard_id, domain_id, host_group_id, task_type)
		VALUES `
	args := make([]interface{}, 0, len(tasks)*5)
	for i, t := range tasks {
		if i > 0 {
			query += ", "
		}
		base := i * 5
		query += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4, base+5)
		args = append(args, t.ReleaseID, t.ShardID, t.DomainID, t.HostGroupID, t.TaskType)
	}
	query += " RETURNING " + domainTaskColumns

	var out []DomainTask
	if err := s.db.SelectContext(ctx, &out, query, args...); err != nil {
		return nil, fmt.Errorf("create domain tasks batch: %w", err)
	}
	return out, nil
}

// ListByRelease returns all domain_tasks for a release.
func (s *DomainTaskStore) ListByRelease(ctx context.Context, releaseID int64) ([]DomainTask, error) {
	var items []DomainTask
	err := s.db.SelectContext(ctx, &items,
		`SELECT `+domainTaskColumns+` FROM domain_tasks WHERE release_id = $1 ORDER BY id`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list domain tasks by release: %w", err)
	}
	return items, nil
}

// ListByShard returns all domain_tasks for a shard.
func (s *DomainTaskStore) ListByShard(ctx context.Context, shardID int64) ([]DomainTask, error) {
	var items []DomainTask
	err := s.db.SelectContext(ctx, &items,
		`SELECT `+domainTaskColumns+` FROM domain_tasks WHERE shard_id = $1 ORDER BY id`, shardID)
	if err != nil {
		return nil, fmt.Errorf("list domain tasks by shard: %w", err)
	}
	return items, nil
}

// UpdateStatus updates a domain_task's status and optional error.
func (s *DomainTaskStore) UpdateStatus(ctx context.Context, id int64, status string, lastError *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE domain_tasks SET status = $1, last_error = $2,
		 ended_at = CASE WHEN $1 IN ('succeeded', 'failed', 'cancelled') THEN NOW() ELSE ended_at END
		 WHERE id = $3`,
		status, lastError, id)
	if err != nil {
		return fmt.Errorf("update domain task %d status: %w", id, err)
	}
	return nil
}

// GetByID returns a domain_task by its DB ID.
func (s *DomainTaskStore) GetByID(ctx context.Context, id int64) (*DomainTask, error) {
	var t DomainTask
	err := s.db.GetContext(ctx, &t,
		`SELECT `+domainTaskColumns+` FROM domain_tasks WHERE id = $1`, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get domain task %d: %w", id, err)
	}
	return &t, nil
}
