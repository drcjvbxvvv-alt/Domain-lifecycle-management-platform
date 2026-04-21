// Package importer handles bulk CSV import of domain registrations.
// It creates a domain_import_jobs row, enqueues an asynq worker task,
// and tracks progress as each row is processed.
package importer

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/lifecycle"
	"domain-platform/internal/tasks"
	"domain-platform/store/postgres"
)

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrJobNotFound   = errors.New("import job not found")
	ErrEmptyCSV      = errors.New("CSV file is empty or has no data rows")
	ErrTooManyRows   = errors.New("CSV exceeds maximum allowed rows")
	ErrInvalidHeader = errors.New("CSV header is missing required columns")
)

// MaxRows is the safety cap on a single import batch.
const MaxRows = 5000

// requiredHeaders lists columns the CSV must contain.
var requiredHeaders = []string{"fqdn"}

// ── Parsed row ────────────────────────────────────────────────────────────────

// ParsedRow is a successfully validated CSV row ready for domain registration.
type ParsedRow struct {
	FQDN               string
	ExpiryDate         *time.Time
	AutoRenew          bool
	RegistrarAccountID *int64
	DNSProviderID      *int64
	Tags               []string // semicolon-separated in CSV, split here
	Notes              string
}

// RowError records a row that failed validation.
type RowError struct {
	Line    int    `json:"line"`
	FQDN    string `json:"fqdn,omitempty"`
	Reason  string `json:"reason"`
}

// ParseResult is the output of ParseCSV.
type ParseResult struct {
	Rows   []ParsedRow
	Errors []RowError
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service implements the import queue logic.
type Service struct {
	jobs      *postgres.ImportJobStore
	domains   *postgres.DomainStore
	lifecycle *lifecycle.Service
	asynq     *asynq.Client
	logger    *zap.Logger
}

// NewService constructs an importer Service.
func NewService(
	jobs *postgres.ImportJobStore,
	domains *postgres.DomainStore,
	lifecycle *lifecycle.Service,
	asynq *asynq.Client,
	logger *zap.Logger,
) *Service {
	return &Service{
		jobs:      jobs,
		domains:   domains,
		lifecycle: lifecycle,
		asynq:     asynq,
		logger:    logger,
	}
}

// ── CSV Parsing ───────────────────────────────────────────────────────────────

// ParseCSV parses raw CSV content and returns validated rows + per-row errors.
// It validates FQDN format, date syntax, and numeric columns.
// It does NOT hit the database — dedup happens in the worker.
func ParseCSV(content string) (*ParseResult, error) {
	r := csv.NewReader(strings.NewReader(content))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1 // allow varying column counts

	all, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(all) == 0 {
		return nil, ErrEmptyCSV
	}

	// Parse header (first row)
	headers := make(map[string]int)
	for i, h := range all[0] {
		headers[strings.ToLower(strings.TrimSpace(h))] = i
	}

	// Verify required columns
	for _, req := range requiredHeaders {
		if _, ok := headers[req]; !ok {
			return nil, fmt.Errorf("%w: column %q not found", ErrInvalidHeader, req)
		}
	}

	dataRows := all[1:]
	if len(dataRows) == 0 {
		return nil, ErrEmptyCSV
	}
	if len(dataRows) > MaxRows {
		return nil, fmt.Errorf("%w: %d rows (max %d)", ErrTooManyRows, len(dataRows), MaxRows)
	}

	result := &ParseResult{}

	col := func(row []string, name string) string {
		idx, ok := headers[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for lineIdx, row := range dataRows {
		lineNum := lineIdx + 2 // 1-indexed, +1 for header

		fqdn := strings.ToLower(col(row, "fqdn"))
		if fqdn == "" {
			result.Errors = append(result.Errors, RowError{Line: lineNum, Reason: "fqdn is empty"})
			continue
		}
		if err := validateFQDN(fqdn); err != nil {
			result.Errors = append(result.Errors, RowError{Line: lineNum, FQDN: fqdn, Reason: err.Error()})
			continue
		}

		pr := ParsedRow{
			FQDN:      fqdn,
			AutoRenew: true, // default
		}

		// expiry_date (optional)
		if raw := col(row, "expiry_date"); raw != "" {
			t, parseErr := time.Parse("2006-01-02", raw)
			if parseErr != nil {
				result.Errors = append(result.Errors, RowError{Line: lineNum, FQDN: fqdn, Reason: "invalid expiry_date: " + raw})
				continue
			}
			pr.ExpiryDate = &t
		}

		// auto_renew (optional, default true)
		if raw := col(row, "auto_renew"); raw != "" {
			b, parseErr := strconv.ParseBool(raw)
			if parseErr != nil {
				result.Errors = append(result.Errors, RowError{Line: lineNum, FQDN: fqdn, Reason: "invalid auto_renew: " + raw})
				continue
			}
			pr.AutoRenew = b
		}

		// registrar_account_id (optional)
		if raw := col(row, "registrar_account_id"); raw != "" {
			id, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil || id <= 0 {
				result.Errors = append(result.Errors, RowError{Line: lineNum, FQDN: fqdn, Reason: "invalid registrar_account_id: " + raw})
				continue
			}
			pr.RegistrarAccountID = &id
		}

		// dns_provider_id (optional)
		if raw := col(row, "dns_provider_id"); raw != "" {
			id, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil || id <= 0 {
				result.Errors = append(result.Errors, RowError{Line: lineNum, FQDN: fqdn, Reason: "invalid dns_provider_id: " + raw})
				continue
			}
			pr.DNSProviderID = &id
		}

		// tags (optional, semicolon-delimited)
		if raw := col(row, "tags"); raw != "" {
			for _, tag := range strings.Split(raw, ";") {
				if t := strings.TrimSpace(tag); t != "" {
					pr.Tags = append(pr.Tags, t)
				}
			}
		}

		// notes (optional)
		pr.Notes = col(row, "notes")

		result.Rows = append(result.Rows, pr)
	}

	return result, nil
}

// validateFQDN enforces minimal FQDN syntax.
// Full DNS validation is out of scope; we just block obvious garbage.
func validateFQDN(fqdn string) error {
	if len(fqdn) > 253 {
		return errors.New("fqdn exceeds 253 characters")
	}
	if !strings.Contains(fqdn, ".") {
		return errors.New("fqdn has no dot (must be fully-qualified)")
	}
	for _, ch := range fqdn {
		if !(unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '.' || ch == '-') {
			return fmt.Errorf("fqdn contains invalid character: %q", ch)
		}
	}
	// Labels must not start or end with a hyphen
	for _, label := range strings.Split(fqdn, ".") {
		if len(label) == 0 {
			return errors.New("fqdn contains empty label")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("fqdn label %q starts or ends with hyphen", label)
		}
	}
	return nil
}

// ── Public API ────────────────────────────────────────────────────────────────

// CreateJobInput contains everything needed to create an import job.
type CreateJobInput struct {
	ProjectID          int64
	RegistrarAccountID *int64
	RawCSV             string
	CreatedBy          *int64
}

// CreateJob validates the CSV, creates a pending job row, and enqueues the asynq task.
// Returns the created job on success.
func (s *Service) CreateJob(ctx context.Context, in CreateJobInput) (*postgres.DomainImportJob, error) {
	// Validate CSV before touching the DB
	result, err := ParseCSV(in.RawCSV)
	if err != nil {
		return nil, err
	}

	totalCount := len(result.Rows) + len(result.Errors)
	if totalCount == 0 {
		return nil, ErrEmptyCSV
	}

	raw := in.RawCSV
	job := &postgres.DomainImportJob{
		ProjectID:          in.ProjectID,
		RegistrarAccountID: in.RegistrarAccountID,
		SourceType:         "csv_upload",
		Status:             "pending",
		TotalCount:         totalCount,
		RawCSV:             &raw,
		CreatedBy:          in.CreatedBy,
	}

	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("create import job: %w", err)
	}

	// Enqueue the worker task
	payload, _ := json.Marshal(tasks.DomainImportPayload{JobID: job.ID})
	task := asynq.NewTask(tasks.TypeDomainImport, payload,
		asynq.MaxRetry(1),            // import is not safe to retry from scratch
		asynq.Timeout(30*time.Minute),
		asynq.Queue("default"),
	)
	if _, err := s.asynq.EnqueueContext(ctx, task); err != nil {
		// Roll back: mark the job as failed so the user sees it
		errStr := fmt.Sprintf(`{"error":%q}`, err.Error())
		_ = s.jobs.MarkCompleted(ctx, job.ID, "failed", 0, 0, 0, &errStr)
		return nil, fmt.Errorf("enqueue import task: %w", err)
	}

	s.logger.Info("import job created",
		zap.Int64("job_id", job.ID),
		zap.Int64("project_id", in.ProjectID),
		zap.Int("total_rows", totalCount),
	)

	return job, nil
}

// GetJob returns a single import job by ID.
func (s *Service) GetJob(ctx context.Context, id int64) (*postgres.DomainImportJob, error) {
	job, err := s.jobs.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJobNotFound, err)
	}
	return job, nil
}

// ListJobs returns paginated import jobs, optionally filtered by project.
func (s *Service) ListJobs(ctx context.Context, projectID int64, limit, offset int) ([]postgres.DomainImportJob, error) {
	return s.jobs.List(ctx, projectID, limit, offset)
}

// ── Worker-facing processing ───────────────────────────────────────────────────

// ProcessImportPayload is called from the asynq task handler.
// It reads the job row, re-parses the stored CSV, and registers each domain.
func (s *Service) ProcessImportPayload(ctx context.Context, jobID int64) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get import job %d: %w", jobID, err)
	}
	if job.RawCSV == nil || *job.RawCSV == "" {
		return fmt.Errorf("job %d has no raw CSV", jobID)
	}

	if err := s.jobs.MarkStarted(ctx, jobID); err != nil {
		return fmt.Errorf("mark job %d started: %w", jobID, err)
	}

	result, err := ParseCSV(*job.RawCSV)
	if err != nil {
		errStr := fmt.Sprintf(`{"parse_error":%q}`, err.Error())
		_ = s.jobs.MarkCompleted(ctx, jobID, "failed", 0, 0, 0, &errStr)
		return fmt.Errorf("re-parse csv for job %d: %w", jobID, err)
	}

	// Collect all FQDNs from parsed rows for dedup check
	fqdns := make([]string, len(result.Rows))
	for i, r := range result.Rows {
		fqdns[i] = r.FQDN
	}

	existing, err := s.domains.ExistingFQDNs(ctx, job.ProjectID, fqdns)
	if err != nil {
		return fmt.Errorf("existing fqdns check: %w", err)
	}

	var (
		imported int
		skipped  int
		failed   int
		rowErrs  []RowError
	)

	// Pre-count already-invalid rows
	skipped += len(result.Errors)
	rowErrs = append(rowErrs, result.Errors...)

	for lineIdx, row := range result.Rows {
		if _, dup := existing[row.FQDN]; dup {
			skipped++
			rowErrs = append(rowErrs, RowError{
				Line:   lineIdx + 2,
				FQDN:   row.FQDN,
				Reason: "already exists",
			})
			continue
		}

		in := lifecycle.RegisterInput{
			ProjectID:          job.ProjectID,
			FQDN:               row.FQDN,
			ExpiryDate:         row.ExpiryDate,
			AutoRenew:          row.AutoRenew,
			RegistrarAccountID: row.RegistrarAccountID,
			DNSProviderID:      row.DNSProviderID,
			Notes:              strPtr(row.Notes),
			TriggeredBy:        fmt.Sprintf("import-job:%d", jobID),
		}

		_, regErr := s.lifecycle.Register(ctx, in)
		if regErr != nil {
			failed++
			rowErrs = append(rowErrs, RowError{
				Line:   lineIdx + 2,
				FQDN:   row.FQDN,
				Reason: regErr.Error(),
			})
			s.logger.Warn("import row failed",
				zap.Int64("job_id", jobID),
				zap.String("fqdn", row.FQDN),
				zap.Error(regErr),
			)
			continue
		}
		imported++

		// Update progress every 100 rows so the UI has live counters
		if (imported+skipped+failed)%100 == 0 {
			_ = s.jobs.UpdateProgress(ctx, jobID, imported, skipped, failed)
		}
	}

	// Final status
	finalStatus := "completed"
	if failed > 0 && imported == 0 {
		finalStatus = "failed"
	}

	var errDetailsPtr *string
	if len(rowErrs) > 0 {
		b, _ := json.Marshal(rowErrs)
		s := string(b)
		errDetailsPtr = &s
	}

	if err := s.jobs.MarkCompleted(ctx, jobID, finalStatus, imported, skipped, failed, errDetailsPtr); err != nil {
		s.logger.Error("mark import job completed failed", zap.Int64("job_id", jobID), zap.Error(err))
	}

	s.logger.Info("import job processed",
		zap.Int64("job_id", jobID),
		zap.String("status", finalStatus),
		zap.Int("imported", imported),
		zap.Int("skipped", skipped),
		zap.Int("failed", failed),
	)

	return nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
