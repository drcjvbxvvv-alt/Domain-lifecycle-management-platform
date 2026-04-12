package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTManager_GenerateAndVerify(t *testing.T) {
	mgr := NewJWTManager("test-secret-key", 1*time.Hour)

	token, err := mgr.Generate(42, "testuser", []string{"admin", "operator"})
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := mgr.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, int64(42), claims.UserID)
	assert.Equal(t, "testuser", claims.Username)
	assert.Equal(t, []string{"admin", "operator"}, claims.Roles)
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	mgr := NewJWTManager("test-secret-key", -1*time.Hour) // already expired

	token, err := mgr.Generate(1, "expired", []string{"viewer"})
	require.NoError(t, err)

	_, err = mgr.Verify(token)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestJWTManager_InvalidToken(t *testing.T) {
	mgr := NewJWTManager("test-secret-key", 1*time.Hour)

	_, err := mgr.Verify("not-a-valid-token")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestJWTManager_WrongSecret(t *testing.T) {
	mgr1 := NewJWTManager("secret-one", 1*time.Hour)
	mgr2 := NewJWTManager("secret-two", 1*time.Hour)

	token, err := mgr1.Generate(1, "user", []string{"viewer"})
	require.NoError(t, err)

	_, err = mgr2.Verify(token)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestJWTManager_EmptyRoles(t *testing.T) {
	mgr := NewJWTManager("test-secret-key", 1*time.Hour)

	token, err := mgr.Generate(1, "noroles", nil)
	require.NoError(t, err)

	claims, err := mgr.Verify(token)
	require.NoError(t, err)
	assert.Nil(t, claims.Roles)
}
