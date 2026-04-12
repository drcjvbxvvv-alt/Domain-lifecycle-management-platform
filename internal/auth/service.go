package auth

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserDisabled       = errors.New("user account is disabled")
)

// Service handles authentication logic.
type Service struct {
	users  *postgres.UserStore
	roles  *postgres.RoleStore
	jwt    *JWTManager
	logger *zap.Logger
}

func NewService(users *postgres.UserStore, roles *postgres.RoleStore, jwt *JWTManager, logger *zap.Logger) *Service {
	return &Service{users: users, roles: roles, jwt: jwt, logger: logger}
}

// LoginResult contains the token and user info returned after successful login.
type LoginResult struct {
	Token    string   `json:"token"`
	UserID   int64    `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
}

// Login authenticates a user by username + password and returns a JWT.
func (s *Service) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	user, err := s.users.GetByUsername(ctx, username)
	if errors.Is(err, postgres.ErrUserNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("login lookup: %w", err)
	}

	if user.Status != "active" {
		return nil, ErrUserDisabled
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	roles, err := s.roles.GetUserRoles(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("login get roles: %w", err)
	}

	token, err := s.jwt.Generate(user.ID, user.Username, roles)
	if err != nil {
		return nil, fmt.Errorf("login generate token: %w", err)
	}

	s.logger.Info("user logged in",
		zap.Int64("user_id", user.ID),
		zap.String("username", user.Username),
		zap.Strings("roles", roles),
	)

	return &LoginResult{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
		Roles:    roles,
	}, nil
}

// HashPassword returns a bcrypt hash of the given password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies a password against a bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
