// Package statuspage manages public status pages, groups, monitors, and incidents.
package statuspage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"domain-platform/internal/maintenance"
	"domain-platform/store/postgres"
)

// ─── Public status types ──────────────────────────────────────────────────────

// OverallStatus is the aggregate health of a status page.
type OverallStatus string

const (
	OverallOperational  OverallStatus = "operational"   // all monitors up
	OverallDegraded     OverallStatus = "degraded"       // some monitors down
	OverallOutage       OverallStatus = "outage"         // most/all monitors down
	OverallMaintenance  OverallStatus = "maintenance"    // one or more in maintenance
)

// MonitorStatus is the resolved status for a single monitor entry.
type MonitorStatus struct {
	MonitorID   int64
	DomainID    int64
	DisplayName string   // display_name override or FQDN
	Status      string   // "up", "down", "maintenance", "unknown"
	UptimePct   float64  // 0–100 over the configured window (default 30d)
	ResponseMS  *int64
}

// GroupStatus aggregates monitors under one group.
type GroupStatus struct {
	GroupID   int64
	GroupName string
	Status    string // worst status among members
	Monitors  []MonitorStatus
}

// PublicStatusResponse is the full response returned for /status/:slug.
type PublicStatusResponse struct {
	Page      *postgres.StatusPage
	Overall   OverallStatus
	Groups    []GroupStatus
	Incidents []postgres.StatusPageIncident
}

// ─── Service ──────────────────────────────────────────────────────────────────

// Service provides status page business logic.
type Service struct {
	store       *postgres.StatusPageStore
	maintenance *maintenance.Service
	logger      *zap.Logger
}

// NewService constructs a status page Service.
func NewService(
	store *postgres.StatusPageStore,
	maintenanceSvc *maintenance.Service,
	logger *zap.Logger,
) *Service {
	return &Service{store: store, maintenance: maintenanceSvc, logger: logger}
}

// ── Status Page CRUD ──────────────────────────────────────────────────────────

// CreatePageInput carries fields for creating a status page.
type CreatePageInput struct {
	Slug               string
	Title              string
	Description        *string
	Published          bool
	Password           *string // plaintext; stored as bcrypt hash
	CustomDomain       *string
	Theme              string
	LogoURL            *string
	FooterText         *string
	CustomCSS          *string
	AutoRefreshSeconds int
	CreatedBy          *int64
}

// CreatePage validates and persists a new status page.
func (s *Service) CreatePage(ctx context.Context, in CreatePageInput) (*postgres.StatusPage, error) {
	if in.Slug == "" {
		return nil, errors.New("slug is required")
	}
	if in.Title == "" {
		return nil, errors.New("title is required")
	}
	if in.AutoRefreshSeconds <= 0 {
		in.AutoRefreshSeconds = 60
	}
	theme := in.Theme
	if theme == "" {
		theme = "default"
	}
	p := &postgres.StatusPage{
		Slug:               in.Slug,
		Title:              in.Title,
		Description:        in.Description,
		Published:          in.Published,
		CustomDomain:       in.CustomDomain,
		Theme:              theme,
		LogoURL:            in.LogoURL,
		FooterText:         in.FooterText,
		CustomCSS:          in.CustomCSS,
		AutoRefreshSeconds: in.AutoRefreshSeconds,
		CreatedBy:          in.CreatedBy,
	}
	if in.Password != nil && *in.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		hs := string(hash)
		p.PasswordHash = &hs
	}
	return s.store.CreatePage(ctx, p)
}

// UpdatePageInput carries updatable fields.
type UpdatePageInput struct {
	Slug               string
	Title              string
	Description        *string
	Published          bool
	Password           *string // empty = keep existing; set to clear
	ClearPassword      bool    // explicitly clears existing password
	CustomDomain       *string
	Theme              string
	LogoURL            *string
	FooterText         *string
	CustomCSS          *string
	AutoRefreshSeconds int
}

// UpdatePage saves changes to an existing status page.
func (s *Service) UpdatePage(ctx context.Context, id int64, in UpdatePageInput) (*postgres.StatusPage, error) {
	p, err := s.store.GetPage(ctx, id)
	if err != nil {
		return nil, err
	}
	p.Slug = in.Slug
	p.Title = in.Title
	p.Description = in.Description
	p.Published = in.Published
	p.CustomDomain = in.CustomDomain
	p.Theme = in.Theme
	if p.Theme == "" {
		p.Theme = "default"
	}
	p.LogoURL = in.LogoURL
	p.FooterText = in.FooterText
	p.CustomCSS = in.CustomCSS
	p.AutoRefreshSeconds = in.AutoRefreshSeconds
	if in.AutoRefreshSeconds <= 0 {
		p.AutoRefreshSeconds = 60
	}
	if in.ClearPassword {
		p.PasswordHash = nil
	} else if in.Password != nil && *in.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		hs := string(hash)
		p.PasswordHash = &hs
	}
	if err := s.store.UpdatePage(ctx, p); err != nil {
		return nil, fmt.Errorf("update status page %d: %w", id, err)
	}
	return p, nil
}

// DeletePage removes a status page and all its children.
func (s *Service) DeletePage(ctx context.Context, id int64) error {
	return s.store.DeletePage(ctx, id)
}

// GetPage returns a status page by ID.
func (s *Service) GetPage(ctx context.Context, id int64) (*postgres.StatusPage, error) {
	return s.store.GetPage(ctx, id)
}

// ListPages returns all status pages.
func (s *Service) ListPages(ctx context.Context) ([]postgres.StatusPage, error) {
	return s.store.ListPages(ctx)
}

// VerifyPassword checks whether the provided password matches the stored hash.
// Returns true if the page has no password (public).
func (s *Service) VerifyPassword(page *postgres.StatusPage, password string) bool {
	if page.PasswordHash == nil {
		return true
	}
	return bcrypt.CompareHashAndPassword([]byte(*page.PasswordHash), []byte(password)) == nil
}

// ── Group / Monitor / Incident delegations ────────────────────────────────────

// CreateGroup creates a group within a page.
func (s *Service) CreateGroup(ctx context.Context, pageID int64, name string, sortOrder int) (*postgres.StatusPageGroup, error) {
	return s.store.CreateGroup(ctx, &postgres.StatusPageGroup{
		StatusPageID: pageID,
		Name:         name,
		SortOrder:    sortOrder,
	})
}

// UpdateGroup saves changes to a group.
func (s *Service) UpdateGroup(ctx context.Context, g *postgres.StatusPageGroup) error {
	return s.store.UpdateGroup(ctx, g)
}

// DeleteGroup removes a group.
func (s *Service) DeleteGroup(ctx context.Context, pageID, groupID int64) error {
	return s.store.DeleteGroup(ctx, pageID, groupID)
}

// ListGroups returns all groups for a page.
func (s *Service) ListGroups(ctx context.Context, pageID int64) ([]postgres.StatusPageGroup, error) {
	return s.store.ListGroups(ctx, pageID)
}

// AddMonitor links a domain to a group.
func (s *Service) AddMonitor(ctx context.Context, groupID, domainID int64, displayName *string, sortOrder int) (*postgres.StatusPageMonitor, error) {
	return s.store.AddMonitor(ctx, &postgres.StatusPageMonitor{
		GroupID:     groupID,
		DomainID:    domainID,
		DisplayName: displayName,
		SortOrder:   sortOrder,
	})
}

// RemoveMonitor unlinks a monitor.
func (s *Service) RemoveMonitor(ctx context.Context, groupID, monitorID int64) error {
	return s.store.RemoveMonitor(ctx, groupID, monitorID)
}

// CreateIncident creates a new incident post.
func (s *Service) CreateIncident(ctx context.Context, pageID int64, title string, content *string, severity string, pinned bool, createdBy *int64) (*postgres.StatusPageIncident, error) {
	if !validSeverity(severity) {
		return nil, fmt.Errorf("invalid severity %q: must be info, warning, or danger", severity)
	}
	return s.store.CreateIncident(ctx, &postgres.StatusPageIncident{
		StatusPageID: pageID,
		Title:        title,
		Content:      content,
		Severity:     severity,
		Pinned:       pinned,
		Active:       true,
		CreatedBy:    createdBy,
	})
}

// UpdateIncident saves changes to an incident.
func (s *Service) UpdateIncident(ctx context.Context, inc *postgres.StatusPageIncident) error {
	if !validSeverity(inc.Severity) {
		return fmt.Errorf("invalid severity %q", inc.Severity)
	}
	return s.store.UpdateIncident(ctx, inc)
}

// DeleteIncident removes an incident.
func (s *Service) DeleteIncident(ctx context.Context, pageID, incidentID int64) error {
	return s.store.DeleteIncident(ctx, pageID, incidentID)
}

// ListIncidents returns incidents for a page.
func (s *Service) ListIncidents(ctx context.Context, pageID int64, activeOnly bool) ([]postgres.StatusPageIncident, error) {
	return s.store.ListIncidents(ctx, pageID, activeOnly)
}

// ── GetPublicStatus ───────────────────────────────────────────────────────────

// uptimeWindow is the rolling window used for uptime % calculation.
const uptimeWindow = 30 * 24 * time.Hour

// GetPublicStatus computes the full public status for a page identified by slug.
// Returns ErrStatusPageNotFound if the page doesn't exist or is not published.
func (s *Service) GetPublicStatus(ctx context.Context, slug string) (*PublicStatusResponse, error) {
	page, err := s.store.GetPageBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !page.Published {
		return nil, postgres.ErrStatusPageNotFound
	}

	// Collect all monitors and their domain IDs.
	monitorRows, err := s.store.ListMonitorsByPage(ctx, page.ID)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}

	domainIDs := make([]int64, 0, len(monitorRows))
	seen := map[int64]bool{}
	for _, m := range monitorRows {
		if !seen[m.DomainID] {
			domainIDs = append(domainIDs, m.DomainID)
			seen[m.DomainID] = true
		}
	}

	// Fetch latest probe status and uptime stats for all domains.
	probeStatuses, err := s.store.GetLatestProbeStatuses(ctx, domainIDs)
	if err != nil {
		s.logger.Warn("failed to fetch probe statuses", zap.Error(err))
		probeStatuses = nil
	}
	probeMap := make(map[int64]postgres.LatestProbeStatus, len(probeStatuses))
	for _, p := range probeStatuses {
		probeMap[p.DomainID] = p
	}

	since := time.Now().Add(-uptimeWindow)
	uptimeStats, err := s.store.GetUptimeStats(ctx, domainIDs, since)
	if err != nil {
		s.logger.Warn("failed to fetch uptime stats", zap.Error(err))
		uptimeStats = nil
	}
	uptimeMap := make(map[int64]float64, len(uptimeStats))
	for _, u := range uptimeStats {
		if u.TotalCount > 0 {
			uptimeMap[u.DomainID] = float64(u.UpCount) / float64(u.TotalCount) * 100
		}
	}

	// Check maintenance for all domains.
	now := time.Now()
	maintenanceMap := make(map[int64]bool, len(domainIDs))
	for _, did := range domainIDs {
		inMaint, _, err := s.maintenance.IsInMaintenance(ctx, did, now)
		if err != nil {
			s.logger.Warn("maintenance check failed", zap.Int64("domain_id", did), zap.Error(err))
		}
		maintenanceMap[did] = inMaint
	}

	// Build group index: groupID → list of monitor rows.
	type groupKey struct{ id int64; name string }
	groupOrder := []groupKey{}
	groupMonitors := map[int64][]postgres.MonitorRow{}
	for _, m := range monitorRows {
		if _, exists := groupMonitors[m.GroupID]; !exists {
			groupOrder = append(groupOrder, groupKey{m.GroupID, m.GroupName})
		}
		groupMonitors[m.GroupID] = append(groupMonitors[m.GroupID], m)
	}

	// Build groups with resolved statuses.
	groups := make([]GroupStatus, 0, len(groupOrder))
	for _, gk := range groupOrder {
		monitors := groupMonitors[gk.id]
		ms := make([]MonitorStatus, 0, len(monitors))
		for _, m := range monitors {
			name := m.FQDN
			if m.DisplayName != nil {
				name = *m.DisplayName
			}
			status := "unknown"
			var respMS *int64
			if ps, ok := probeMap[m.DomainID]; ok {
				status = ps.Status
				respMS = ps.ResponseMS
			}
			// Override with maintenance if applicable.
			if maintenanceMap[m.DomainID] {
				status = "maintenance"
			}
			ms = append(ms, MonitorStatus{
				MonitorID:   m.ID,
				DomainID:    m.DomainID,
				DisplayName: name,
				Status:      status,
				UptimePct:   uptimeMap[m.DomainID],
				ResponseMS:  respMS,
			})
		}
		groups = append(groups, GroupStatus{
			GroupID:   gk.id,
			GroupName: gk.name,
			Status:    worstStatus(ms),
			Monitors:  ms,
		})
	}

	// Active incidents.
	incidents, err := s.store.ListIncidents(ctx, page.ID, true)
	if err != nil {
		s.logger.Warn("failed to fetch incidents", zap.Error(err))
		incidents = nil
	}

	return &PublicStatusResponse{
		Page:      page,
		Overall:   computeOverall(groups),
		Groups:    groups,
		Incidents: incidents,
	}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

var statusRank = map[string]int{"down": 3, "unknown": 2, "maintenance": 1, "up": 0}

func worstStatus(monitors []MonitorStatus) string {
	worst := "up"
	for _, m := range monitors {
		if statusRank[m.Status] > statusRank[worst] {
			worst = m.Status
		}
	}
	return worst
}

func computeOverall(groups []GroupStatus) OverallStatus {
	downCount := 0
	total := 0
	hasMaintenance := false
	for _, g := range groups {
		for _, m := range g.Monitors {
			total++
			switch m.Status {
			case "down":
				downCount++
			case "maintenance":
				hasMaintenance = true
			}
		}
	}
	if total == 0 {
		return OverallOperational
	}
	if downCount == 0 && !hasMaintenance {
		return OverallOperational
	}
	if downCount == 0 && hasMaintenance {
		return OverallMaintenance
	}
	if downCount*2 >= total { // >= 50% down
		return OverallOutage
	}
	return OverallDegraded
}

func validSeverity(s string) bool {
	return s == "info" || s == "warning" || s == "danger"
}
