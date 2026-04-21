package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// DomainImportJob maps to the domain_import_jobs table.
type DomainImportJob struct {
	ID                  int64      `db:"id"`
	UUID                string     `db:"uuid"`
	ProjectID           int64      `db:"project_id"`
	RegistrarAccountID  *int64     `db:"registrar_account_id"`
	SourceType          string     `db:"source_type"`
	Status              string     `db:"status"`
	TotalCount          int        `db:"total_count"`
	ImportedCount       int        `db:"imported_count"`
	SkippedCount        int        `db:"skipped_count"`
	FailedCount         int        `db:"failed_count"`
	ErrorDetails        *string    `db:"error_details"` // JSONB serialised as string
	RawCSV              *string    `db:"raw_csv"`
	CreatedBy           *int64     `db:"created_by"`
	StartedAt           *time.Time `db:"started_at"`
	CompletedAt         *time.Time `db:"completed_at"`
	CreatedAt           time.Time  `db:"created_at"`
}

// ImportJobStore handles persistence for domain_import_jobs.
type ImportJobStore struct {
	db *sqlx.DB
}

// NewImportJobStore returns an ImportJobStore backed by db.
func NewImportJobStore(db *sqlx.DB) *ImportJobStore {
	return &ImportJobStore{db: db}
}

const insertImportJob = `
INSERT INTO domain_import_jobs (
    project_id, registrar_account_id, source_type, status,
    total_count, raw_csv, created_by
) VALUES (
    :project_id, :registrar_account_id, :source_type, :status,
    :total_count, :raw_csv, :created_by
)
RETURNING id, uuid, created_at`

// Create inserts a new import job row and populates ID, UUID, CreatedAt.
func (s *ImportJobStore) Create(ctx context.Context, job *DomainImportJob) error {
	rows, err := s.db.NamedQueryContext(ctx, insertImportJob, job)
	if err != nil {
		return fmt.Errorf("create import job: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&job.ID, &job.UUID, &job.CreatedAt); err != nil {
			return fmt.Errorf("scan import job id: %w", err)
		}
	}
	return rows.Err()
}

const getImportJob = `
SELECT id, uuid, project_id, registrar_account_id, source_type, status,
       total_count, imported_count, skipped_count, failed_count,
       error_details, raw_csv, created_by, started_at, completed_at, created_at
FROM domain_import_jobs
WHERE id = $1`

// Get returns a single import job by ID.
func (s *ImportJobStore) Get(ctx context.Context, id int64) (*DomainImportJob, error) {
	var job DomainImportJob
	if err := s.db.GetContext(ctx, &job, getImportJob, id); err != nil {
		return nil, fmt.Errorf("get import job %d: %w", id, err)
	}
	return &job, nil
}

const listImportJobs = `
SELECT id, uuid, project_id, registrar_account_id, source_type, status,
       total_count, imported_count, skipped_count, failed_count,
       error_details, raw_csv, created_by, started_at, completed_at, created_at
FROM domain_import_jobs
WHERE ($1 = 0 OR project_id = $1)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3`

// List returns paginated import jobs. Pass projectID=0 to list across all projects.
func (s *ImportJobStore) List(ctx context.Context, projectID int64, limit, offset int) ([]DomainImportJob, error) {
	if limit <= 0 {
		limit = 50
	}
	var jobs []DomainImportJob
	if err := s.db.SelectContext(ctx, &jobs, listImportJobs, projectID, limit, offset); err != nil {
		return nil, fmt.Errorf("list import jobs: %w", err)
	}
	return jobs, nil
}

const startImportJob = `
UPDATE domain_import_jobs
SET status = 'processing', started_at = NOW()
WHERE id = $1`

// MarkStarted transitions a job from pending → processing.
func (s *ImportJobStore) MarkStarted(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, startImportJob, id); err != nil {
		return fmt.Errorf("mark import job %d started: %w", id, err)
	}
	return nil
}

const updateProgress = `
UPDATE domain_import_jobs
SET imported_count = $2, skipped_count = $3, failed_count = $4
WHERE id = $1`

// UpdateProgress writes incremental counters during processing.
func (s *ImportJobStore) UpdateProgress(ctx context.Context, id int64, imported, skipped, failed int) error {
	if _, err := s.db.ExecContext(ctx, updateProgress, id, imported, skipped, failed); err != nil {
		return fmt.Errorf("update progress for job %d: %w", id, err)
	}
	return nil
}

const completeImportJob = `
UPDATE domain_import_jobs
SET status = $2, completed_at = NOW(),
    imported_count = $3, skipped_count = $4, failed_count = $5,
    error_details  = $6
WHERE id = $1`

// MarkCompleted sets a job to completed or failed with final counts and optional error JSON.
func (s *ImportJobStore) MarkCompleted(ctx context.Context, id int64, status string, imported, skipped, failed int, errDetails *string) error {
	if _, err := s.db.ExecContext(ctx, completeImportJob, id, status, imported, skipped, failed, errDetails); err != nil {
		return fmt.Errorf("mark import job %d %s: %w", id, status, err)
	}
	return nil
}
