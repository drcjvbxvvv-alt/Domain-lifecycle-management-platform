package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword_And_CheckPassword(t *testing.T) {
	password := "mysecretpassword"

	hash, err := HashPassword(password)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, password, hash) // hash differs from plain text

	assert.True(t, CheckPassword(hash, password))
	assert.False(t, CheckPassword(hash, "wrongpassword"))
}

func TestHashPassword_DifferentHashesEachTime(t *testing.T) {
	password := "samepassword"

	hash1, err := HashPassword(password)
	require.NoError(t, err)

	hash2, err := HashPassword(password)
	require.NoError(t, err)

	// bcrypt uses random salt — same input produces different hashes
	assert.NotEqual(t, hash1, hash2)

	// Both should still verify against the original password
	assert.True(t, CheckPassword(hash1, password))
	assert.True(t, CheckPassword(hash2, password))
}
