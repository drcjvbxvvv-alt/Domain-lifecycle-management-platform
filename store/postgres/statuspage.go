package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ─── Errors ───────────────────────────────────────────────────────────────────

var (
	ErrStatusPageNotFound    = errors.New("status page not found")
	ErrStatusGroupNotFound   = errors.New("status page group not found")
	ErrStatusIncidentNotFound = errors.New("status page incident not found")
)

// ─── Models ───────────────────────────────────────────────────────────────────

// StatusPage represents a public status page.
type StatusPage struct {
	ID                  int64     `db:"id"`
	UUID                string    `db:"uuid"`
	Slug                string    `db:"slug"`
	Title               string    `db:"title"`
	Description         *string   `db:"description"`
	Published           bool      `db:"published"`
	PasswordHash        *string   `db:"password_hash"`
	CustomDomain        *string   `db:"custom_domain"`
	Theme               string    `db:"theme"`
	LogoURL             *string   `db:"logo_url"`
	FooterText          *string   `db:"footer_text"`
	CustomCSS           *string   `db:"custom_css"`
	AutoRefreshSeconds  int       `db:"auto_refresh_seconds"`
	CreatedBy           *int64    `db:"created_by"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

// StatusPageGroup is a named group within a status page.
type StatusPageGroup struct {
	ID           int64  `db:"id"`
	StatusPageID int64  `db:"status_page_id"`
	Name         string `db:"name"`
	SortOrder    int    `db:"sort_order"`
}

// StatusPageMonitor links a domain to a group for display.
type StatusPageMonitor struct {
	ID          int64   `db:"id"`
	GroupID     int64   `db:"group_id"`
	DomainID    int64   `db:"domain_id"`
	DisplayName *string `db:"display_name"`
	SortOrder   int     `db:"sort_order"`
}

// StatusPageIncident is an operator-written incident post for a status page.
type StatusPageIncident struct {
	ID           int64     `db:"id"`
	StatusPageID int64     `db:"status_page_id"`
	Title        string    `db:"title"`
	Content      *string   `db:"content"`
	Severity     string    `db:"severity"` // info, warning, danger
	Pinned       bool      `db:"pinned"`
	Active       bool      `db:"active"`
	CreatedBy    *int64    `db:"created_by"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// ─── StatusPageStore ──────────────────────────────────────────────────────────

// StatusPageStore handles persistence for status pages and related entities.
type StatusPageStore struct {
	db *sqlx.DB
}

// NewStatusPageStore constructs a StatusPageStore.
func NewStatusPageStore(db *sqlx.DB) *StatusPageStore {
	return &StatusPageStore{db: db}
}

// ── Status Page CRUD ──────────────────────────────────────────────────────────

const insertStatusPage = `
INSERT INTO status_pages
    (slug, title, description, published, password_hash, custom_domain,
     theme, logo_url, footer_text, custom_css, auto_refresh_seconds, created_by)
VALUES
    (:slug, :title, :description, :published, :password_hash, :custom_domain,
     :theme, :logo_url, :footer_text, :custom_css, :auto_refresh_seconds, :created_by)
RETURNING id, uuid, created_at, updated_at`

// CreatePage inserts a new status page.
func (s *StatusPageStore) CreatePage(ctx context.Context, p *StatusPage) (*StatusPage, error) {
	rows, err := s.db.NamedQueryContext(ctx, insertStatusPage, p)
	if err != nil {
		return nil, fmt.Errorf("create status page: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&p.ID, &p.UUID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan status page: %w", err)
		}
	}
	return p, nil
}

const updateStatusPage = `
UPDATE status_pages
SET slug=$2, title=$3, description=$4, published=$5, password_hash=$6,
    custom_domain=$7, theme=$8, logo_url=$9, footer_text=$10, custom_css=$11,
    auto_refresh_seconds=$12, updated_at=NOW()
WHERE id=$1
RETURNING updated_at`

// UpdatePage saves changes to an existing status page.
func (s *StatusPageStore) UpdatePage(ctx context.Context, p *StatusPage) error {
	return s.db.QueryRowContext(ctx, updateStatusPage,
		p.ID, p.Slug, p.Title, p.Description, p.Published, p.PasswordHash,
		p.CustomDomain, p.Theme, p.LogoURL, p.FooterText, p.CustomCSS,
		p.AutoRefreshSeconds,
	).Scan(&p.UpdatedAt)
}

// DeletePage removes a status page (cascades to groups/monitors/incidents).
func (s *StatusPageStore) DeletePage(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM status_pages WHERE id=$1`, id)
	return err
}

const getStatusPageByID = `
SELECT id, uuid, slug, title, description, published, password_hash, custom_domain,
       theme, logo_url, footer_text, custom_css, auto_refresh_seconds,
       created_by, created_at, updated_at
FROM status_pages WHERE id=$1`

// GetPage returns a status page by primary key.
func (s *StatusPageStore) GetPage(ctx context.Context, id int64) (*StatusPage, error) {
	var p StatusPage
	if err := s.db.GetContext(ctx, &p, getStatusPageByID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStatusPageNotFound
		}
		return nil, fmt.Errorf("get status page %d: %w", id, err)
	}
	return &p, nil
}

const getStatusPageBySlug = `
SELECT id, uuid, slug, title, description, published, password_hash, custom_domain,
       theme, logo_url, footer_text, custom_css, auto_refresh_seconds,
       created_by, created_at, updated_at
FROM status_pages WHERE slug=$1`

// GetPageBySlug returns a status page by its URL slug.
func (s *StatusPageStore) GetPageBySlug(ctx context.Context, slug string) (*StatusPage, error) {
	var p StatusPage
	if err := s.db.GetContext(ctx, &p, getStatusPageBySlug, slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStatusPageNotFound
		}
		return nil, fmt.Errorf("get status page by slug %q: %w", slug, err)
	}
	return &p, nil
}

// ListPages returns all status pages ordered by creation date.
func (s *StatusPageStore) ListPages(ctx context.Context) ([]StatusPage, error) {
	var pages []StatusPage
	err := s.db.SelectContext(ctx, &pages, `
		SELECT id, uuid, slug, title, description, published, password_hash, custom_domain,
		       theme, logo_url, footer_text, custom_css, auto_refresh_seconds,
		       created_by, created_at, updated_at
		FROM status_pages ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list status pages: %w", err)
	}
	return pages, nil
}

// ── Group CRUD ────────────────────────────────────────────────────────────────

// CreateGroup inserts a new group into a status page.
func (s *StatusPageStore) CreateGroup(ctx context.Context, g *StatusPageGroup) (*StatusPageGroup, error) {
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO status_page_groups (status_page_id, name, sort_order)
		VALUES ($1, $2, $3)
		RETURNING id`,
		g.StatusPageID, g.Name, g.SortOrder,
	).Scan(&g.ID)
	if err != nil {
		return nil, fmt.Errorf("create status page group: %w", err)
	}
	return g, nil
}

// UpdateGroup saves changes to an existing group.
func (s *StatusPageStore) UpdateGroup(ctx context.Context, g *StatusPageGroup) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE status_page_groups SET name=$2, sort_order=$3 WHERE id=$1 AND status_page_id=$4`,
		g.ID, g.Name, g.SortOrder, g.StatusPageID)
	return err
}

// DeleteGroup removes a group (cascades to monitors).
func (s *StatusPageStore) DeleteGroup(ctx context.Context, pageID, groupID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM status_page_groups WHERE id=$1 AND status_page_id=$2`,
		groupID, pageID)
	return err
}

// ListGroups returns all groups for a page, ordered by sort_order.
func (s *StatusPageStore) ListGroups(ctx context.Context, pageID int64) ([]StatusPageGroup, error) {
	var gs []StatusPageGroup
	err := s.db.SelectContext(ctx, &gs,
		`SELECT id, status_page_id, name, sort_order
		 FROM status_page_groups WHERE status_page_id=$1 ORDER BY sort_order, id`,
		pageID)
	if err != nil {
		return nil, fmt.Errorf("list groups for page %d: %w", pageID, err)
	}
	return gs, nil
}

// ── Monitor CRUD ──────────────────────────────────────────────────────────────

// AddMonitor links a domain to a group.
func (s *StatusPageStore) AddMonitor(ctx context.Context, m *StatusPageMonitor) (*StatusPageMonitor, error) {
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO status_page_monitors (group_id, domain_id, display_name, sort_order)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (group_id, domain_id) DO UPDATE
		    SET display_name=EXCLUDED.display_name, sort_order=EXCLUDED.sort_order
		RETURNING id`,
		m.GroupID, m.DomainID, m.DisplayName, m.SortOrder,
	).Scan(&m.ID)
	if err != nil {
		return nil, fmt.Errorf("add monitor: %w", err)
	}
	return m, nil
}

// RemoveMonitor unlinks a domain from a group.
func (s *StatusPageStore) RemoveMonitor(ctx context.Context, groupID, monitorID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM status_page_monitors WHERE id=$1 AND group_id=$2`,
		monitorID, groupID)
	return err
}

// ListMonitors returns all monitors in a group, ordered by sort_order.
func (s *StatusPageStore) ListMonitors(ctx context.Context, groupID int64) ([]StatusPageMonitor, error) {
	var ms []StatusPageMonitor
	err := s.db.SelectContext(ctx, &ms,
		`SELECT id, group_id, domain_id, display_name, sort_order
		 FROM status_page_monitors WHERE group_id=$1 ORDER BY sort_order, id`,
		groupID)
	if err != nil {
		return nil, fmt.Errorf("list monitors for group %d: %w", groupID, err)
	}
	return ms, nil
}

// ListMonitorsByPage returns all monitors across all groups of a page.
// Includes join to get FQDN for convenience.
type MonitorRow struct {
	StatusPageMonitor
	FQDN      string `db:"fqdn"`
	GroupName string `db:"group_name"`
}

func (s *StatusPageStore) ListMonitorsByPage(ctx context.Context, pageID int64) ([]MonitorRow, error) {
	var rows []MonitorRow
	err := s.db.SelectContext(ctx, &rows, `
		SELECT m.id, m.group_id, m.domain_id, m.display_name, m.sort_order,
		       d.fqdn, g.name AS group_name
		FROM status_page_monitors m
		JOIN status_page_groups g ON g.id = m.group_id
		JOIN domains d ON d.id = m.domain_id
		WHERE g.status_page_id = $1
		ORDER BY g.sort_order, g.id, m.sort_order, m.id`,
		pageID)
	if err != nil {
		return nil, fmt.Errorf("list monitors by page %d: %w", pageID, err)
	}
	return rows, nil
}

// ── Incident CRUD ─────────────────────────────────────────────────────────────

// CreateIncident inserts a new incident.
func (s *StatusPageStore) CreateIncident(ctx context.Context, inc *StatusPageIncident) (*StatusPageIncident, error) {
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO status_page_incidents
		    (status_page_id, title, content, severity, pinned, active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`,
		inc.StatusPageID, inc.Title, inc.Content, inc.Severity, inc.Pinned, inc.Active, inc.CreatedBy,
	).Scan(&inc.ID, &inc.CreatedAt, &inc.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create incident: %w", err)
	}
	return inc, nil
}

// UpdateIncident saves changes to an existing incident.
func (s *StatusPageStore) UpdateIncident(ctx context.Context, inc *StatusPageIncident) error {
	return s.db.QueryRowContext(ctx, `
		UPDATE status_page_incidents
		SET title=$2, content=$3, severity=$4, pinned=$5, active=$6, updated_at=NOW()
		WHERE id=$1 AND status_page_id=$7
		RETURNING updated_at`,
		inc.ID, inc.Title, inc.Content, inc.Severity, inc.Pinned, inc.Active, inc.StatusPageID,
	).Scan(&inc.UpdatedAt)
}

// DeleteIncident removes an incident.
func (s *StatusPageStore) DeleteIncident(ctx context.Context, pageID, incidentID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM status_page_incidents WHERE id=$1 AND status_page_id=$2`,
		incidentID, pageID)
	return err
}

// ListIncidents returns incidents for a page, optionally filtered to active only.
func (s *StatusPageStore) ListIncidents(ctx context.Context, pageID int64, activeOnly bool) ([]StatusPageIncident, error) {
	query := `
		SELECT id, status_page_id, title, content, severity, pinned, active, created_by, created_at, updated_at
		FROM status_page_incidents WHERE status_page_id=$1`
	if activeOnly {
		query += ` AND active=true`
	}
	query += ` ORDER BY pinned DESC, created_at DESC LIMIT 20`
	var incs []StatusPageIncident
	if err := s.db.SelectContext(ctx, &incs, query, pageID); err != nil {
		return nil, fmt.Errorf("list incidents for page %d: %w", pageID, err)
	}
	return incs, nil
}

// ── Public status helpers ─────────────────────────────────────────────────────

// LatestProbeStatus holds the most recent probe status for a domain.
type LatestProbeStatus struct {
	DomainID   int64   `db:"domain_id"`
	Status     string  `db:"status"`     // "up", "down", "maintenance"
	ProbeType  string  `db:"probe_type"` // "l1"
	ResponseMS *int64  `db:"response_time_ms"`
	MeasuredAt time.Time `db:"measured_at"`
}

// GetLatestProbeStatuses returns the most recent L1 probe result for each
// domain in the given list.
func (s *StatusPageStore) GetLatestProbeStatuses(ctx context.Context, domainIDs []int64) ([]LatestProbeStatus, error) {
	if len(domainIDs) == 0 {
		return nil, nil
	}
	query, args, err := sqlx.In(`
		SELECT DISTINCT ON (domain_id)
		       domain_id, status, probe_type, response_time_ms, measured_at
		FROM probe_results
		WHERE domain_id IN (?) AND probe_type='l1'
		ORDER BY domain_id, measured_at DESC`,
		domainIDs)
	if err != nil {
		return nil, fmt.Errorf("build latest probe query: %w", err)
	}
	query = s.db.Rebind(query)
	var rows []LatestProbeStatus
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("get latest probe statuses: %w", err)
	}
	return rows, nil
}

// DomainUptimeStat is a rolling uptime percentage for a domain.
type DomainUptimeStat struct {
	DomainID  int64   `db:"domain_id"`
	UpCount   int64   `db:"up_count"`
	TotalCount int64  `db:"total_count"`
}

// GetUptimeStats returns uptime counts for each domain over the given duration.
// Excludes "maintenance" from total (doesn't count against uptime).
func (s *StatusPageStore) GetUptimeStats(ctx context.Context, domainIDs []int64, since time.Time) ([]DomainUptimeStat, error) {
	if len(domainIDs) == 0 {
		return nil, nil
	}
	query, args, err := sqlx.In(`
		SELECT domain_id,
		       COUNT(*) FILTER (WHERE status='up') AS up_count,
		       COUNT(*) FILTER (WHERE status IN ('up','down')) AS total_count
		FROM probe_results
		WHERE domain_id IN (?) AND probe_type='l1' AND measured_at >= ?
		GROUP BY domain_id`,
		domainIDs, since)
	if err != nil {
		return nil, fmt.Errorf("build uptime query: %w", err)
	}
	query = s.db.Rebind(query)
	var stats []DomainUptimeStat
	if err := s.db.SelectContext(ctx, &stats, query, args...); err != nil {
		return nil, fmt.Errorf("get uptime stats: %w", err)
	}
	return stats, nil
}
