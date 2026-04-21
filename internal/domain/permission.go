package domain

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// Permission levels — ordered from lowest to highest.
const (
	PermViewer = "viewer"
	PermEditor = "editor"
	PermAdmin  = "admin"
)

// permissionRank maps permission strings to numeric rank for comparison.
var permissionRank = map[string]int{
	PermViewer: 1,
	PermEditor: 2,
	PermAdmin:  3,
}

// globalRoleToPermission maps global roles to their equivalent domain permission level.
// Global admin/release_manager → domain admin; operator → editor; viewer/auditor → viewer.
var globalRoleToPermission = map[string]string{
	"admin":           PermAdmin,
	"release_manager": PermAdmin,
	"operator":        PermEditor,
	"viewer":          PermViewer,
	"auditor":         PermViewer,
}

// PermissionStore is the subset of DomainPermissionStore used by the service.
type PermissionStore interface {
	Upsert(ctx context.Context, domainID, userID int64, permission string, grantedBy *int64) error
	Delete(ctx context.Context, domainID, userID int64) error
	Get(ctx context.Context, domainID, userID int64) (*postgres.DomainPermission, error)
	List(ctx context.Context, domainID int64) ([]postgres.DomainPermissionWithUser, error)
}

// RoleStore is the subset of RoleStore used for global-role lookups.
type RoleStore interface {
	GetUserRoles(ctx context.Context, userID int64) ([]string, error)
}

// PermissionService manages zone-level RBAC for domains.
type PermissionService struct {
	perms  PermissionStore
	roles  RoleStore
	logger *zap.Logger
}

// NewPermissionService constructs a PermissionService.
func NewPermissionService(perms PermissionStore, roles RoleStore, logger *zap.Logger) *PermissionService {
	return &PermissionService{perms: perms, roles: roles, logger: logger}
}

// GrantPermission grants (or updates) a user's permission on a domain.
func (s *PermissionService) GrantPermission(ctx context.Context, domainID, userID int64, permission string, grantedBy int64) error {
	if _, ok := permissionRank[permission]; !ok {
		return fmt.Errorf("invalid permission level %q: must be viewer, editor, or admin", permission)
	}
	if err := s.perms.Upsert(ctx, domainID, userID, permission, &grantedBy); err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	s.logger.Info("domain permission granted",
		zap.Int64("domain_id", domainID),
		zap.Int64("user_id", userID),
		zap.String("permission", permission),
		zap.Int64("granted_by", grantedBy),
	)
	return nil
}

// RevokePermission removes a user's direct permission grant on a domain.
// Returns ErrDomainPermissionNotFound if no grant existed.
func (s *PermissionService) RevokePermission(ctx context.Context, domainID, userID int64) error {
	if err := s.perms.Delete(ctx, domainID, userID); err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}
	s.logger.Info("domain permission revoked",
		zap.Int64("domain_id", domainID),
		zap.Int64("user_id", userID),
	)
	return nil
}

// GetPermission returns the direct domain-level permission for a user.
// Returns ErrDomainPermissionNotFound if none is set.
func (s *PermissionService) GetPermission(ctx context.Context, domainID, userID int64) (*postgres.DomainPermission, error) {
	return s.perms.Get(ctx, domainID, userID)
}

// ListPermissions returns all explicit permission grants for a domain.
func (s *PermissionService) ListPermissions(ctx context.Context, domainID int64) ([]postgres.DomainPermissionWithUser, error) {
	return s.perms.List(ctx, domainID)
}

// EffectivePermission returns the highest permission level a user holds on a domain,
// considering both their global role and any explicit domain permission.
// Returns "" if the user has no access at all.
func (s *PermissionService) EffectivePermission(ctx context.Context, domainID, userID int64) (string, error) {
	best := ""

	// 1. Check global role — maps to a domain permission level.
	globalRoles, err := s.roles.GetUserRoles(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("effective permission — get roles: %w", err)
	}
	for _, r := range globalRoles {
		if mapped, ok := globalRoleToPermission[r]; ok {
			if permissionRank[mapped] > permissionRank[best] {
				best = mapped
			}
		}
	}

	// 2. Check direct domain permission.
	dp, err := s.perms.Get(ctx, domainID, userID)
	if err != nil && !errors.Is(err, postgres.ErrDomainPermissionNotFound) {
		return "", fmt.Errorf("effective permission — get domain perm: %w", err)
	}
	if dp != nil {
		if permissionRank[dp.Permission] > permissionRank[best] {
			best = dp.Permission
		}
	}

	return best, nil
}

// HasPermission returns true if the user's effective permission is at least minLevel.
func (s *PermissionService) HasPermission(ctx context.Context, domainID, userID int64, minLevel string) (bool, error) {
	eff, err := s.EffectivePermission(ctx, domainID, userID)
	if err != nil {
		return false, err
	}
	return permissionRank[eff] >= permissionRank[minLevel], nil
}
