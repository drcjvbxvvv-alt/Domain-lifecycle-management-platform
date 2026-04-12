package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// User maps to the users table row.
type User struct {
	ID           int64      `db:"id"`
	UUID         string     `db:"uuid"`
	Username     string     `db:"username"`
	PasswordHash string     `db:"password_hash"`
	DisplayName  *string    `db:"display_name"`
	Status       string     `db:"status"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	DeletedAt    *time.Time `db:"deleted_at"`
}

var ErrUserNotFound = errors.New("user not found")

type UserStore struct {
	db *sqlx.DB
}

func NewUserStore(db *sqlx.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u,
		`SELECT id, uuid, username, password_hash, display_name, status, created_at, updated_at, deleted_at
		 FROM users WHERE username = $1 AND deleted_at IS NULL`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return &u, nil
}

func (s *UserStore) GetByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u,
		`SELECT id, uuid, username, password_hash, display_name, status, created_at, updated_at, deleted_at
		 FROM users WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &u, nil
}

func (s *UserStore) Create(ctx context.Context, username, passwordHash string, displayName *string) (*User, error) {
	var u User
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO users (username, password_hash, display_name)
		 VALUES ($1, $2, $3)
		 RETURNING id, uuid, username, password_hash, display_name, status, created_at, updated_at, deleted_at`,
		username, passwordHash, displayName).StructScan(&u)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &u, nil
}

func (s *UserStore) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`,
		passwordHash, userID)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}
