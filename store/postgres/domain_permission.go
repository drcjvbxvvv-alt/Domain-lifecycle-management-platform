package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ErrDomainPermissionNotFound is returned when no permission row exists.
var ErrDomainPermissionNotFound = errors.New("domain permission not found")

// DomainPermission mirrors the domain_permissions table row.
type DomainPermission struct {
	ID         int64     `db:"id"`
	DomainID   int64     `db:"domain_id"`
	UserID     int64     `db:"user_id"`
	Permission string    `db:"permission"` // viewer | editor | admin
	GrantedBy  *int64    `db:"granted_by"`
	GrantedAt  time.Time `db:"granted_at"`
}

// DomainPermissionWithUser joins basic user info for list responses.
type DomainPermissionWithUser struct {
	DomainPermission
	Username    string  `db:"username"`
	DisplayName *string `db:"display_name"`
}

// DomainPermissionStore is the data-access object for domain_permissions.
type DomainPermissionStore struct {
	db *sqlx.DB
}

// NewDomainPermissionStore creates a new DomainPermissionStore.
func NewDomainPermissionStore(db *sqlx.DB) *DomainPermissionStore {
	return &DomainPermissionStore{db: db}
}

// Upsert inserts or updates a permission row (ON CONFLICT DO UPDATE).
func (s *DomainPermissionStore) Upsert(ctx context.Context, domainID, userID int64, permission string, grantedBy *int64) error {
	const q = `
		INSERT INTO domain_permissions (domain_id, user_id, permission, granted_by, granted_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (domain_id, user_id) DO UPDATE
		    SET permission  = EXCLUDED.permission,
		        granted_by  = EXCLUDED.granted_by,
		        granted_at  = NOW()`
	_, err := s.db.ExecContext(ctx, q, domainID, userID, permission, grantedBy)
	if err != nil {
		return fmt.Errorf("upsert domain permission: %w", err)
	}
	return nil
}

// Delete removes a permission grant. Returns ErrDomainPermissionNotFound if none existed.
func (s *DomainPermissionStore) Delete(ctx context.Context, domainID, userID int64) error {
	const q = `DELETE FROM domain_permissions WHERE domain_id = $1 AND user_id = $2`
	res, err := s.db.ExecContext(ctx, q, domainID, userID)
	if err != nil {
		return fmt.Errorf("delete domain permission: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDomainPermissionNotFound
	}
	return nil
}

// Get returns the permission level for a specific (domain, user) pair.
// Returns ErrDomainPermissionNotFound when no row exists.
func (s *DomainPermissionStore) Get(ctx context.Context, domainID, userID int64) (*DomainPermission, error) {
	const q = `SELECT id, domain_id, user_id, permission, granted_by, granted_at
	           FROM domain_permissions
	           WHERE domain_id = $1 AND user_id = $2`
	var p DomainPermission
	if err := s.db.GetContext(ctx, &p, q, domainID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDomainPermissionNotFound
		}
		return nil, fmt.Errorf("get domain permission: %w", err)
	}
	return &p, nil
}

// List returns all permission grants for a domain, joined with username.
func (s *DomainPermissionStore) List(ctx context.Context, domainID int64) ([]DomainPermissionWithUser, error) {
	const q = `
		SELECT dp.id, dp.domain_id, dp.user_id, dp.permission,
		       dp.granted_by, dp.granted_at,
		       u.username, u.display_name
		FROM domain_permissions dp
		JOIN users u ON u.id = dp.user_id
		WHERE dp.domain_id = $1
		ORDER BY dp.granted_at DESC`
	var rows []DomainPermissionWithUser
	if err := s.db.SelectContext(ctx, &rows, q, domainID); err != nil {
		return nil, fmt.Errorf("list domain permissions: %w", err)
	}
	return rows, nil
}
