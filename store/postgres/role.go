package postgres

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type RoleStore struct {
	db *sqlx.DB
}

func NewRoleStore(db *sqlx.DB) *RoleStore {
	return &RoleStore{db: db}
}

// GetUserRoles returns the role names for a given user ID.
func (s *RoleStore) GetUserRoles(ctx context.Context, userID int64) ([]string, error) {
	var roles []string
	err := s.db.SelectContext(ctx, &roles,
		`SELECT r.name FROM roles r
		 JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	return roles, nil
}

// HasRole checks whether a user has a specific role.
func (s *RoleStore) HasRole(ctx context.Context, userID int64, role string) (bool, error) {
	var count int
	err := s.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM user_roles ur
		 JOIN roles r ON r.id = ur.role_id
		 WHERE ur.user_id = $1 AND r.name = $2`, userID, role)
	if err != nil {
		return false, fmt.Errorf("has role: %w", err)
	}
	return count > 0, nil
}

// GrantRole grants a role to a user. Idempotent (ON CONFLICT DO NOTHING).
func (s *RoleStore) GrantRole(ctx context.Context, userID int64, roleName string, grantedBy *int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role_id, granted_by)
		 SELECT $1, r.id, $3 FROM roles r WHERE r.name = $2
		 ON CONFLICT (user_id, role_id) DO NOTHING`,
		userID, roleName, grantedBy)
	if err != nil {
		return fmt.Errorf("grant role: %w", err)
	}
	return nil
}

// RevokeRole removes a role from a user.
func (s *RoleStore) RevokeRole(ctx context.Context, userID int64, roleName string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_roles
		 WHERE user_id = $1 AND role_id = (SELECT id FROM roles WHERE name = $2)`,
		userID, roleName)
	if err != nil {
		return fmt.Errorf("revoke role: %w", err)
	}
	return nil
}
