package template

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeVersionChecksum_Deterministic(t *testing.T) {
	html := "<html>{{ .ReleaseVersion }}</html>"
	nginx := "server { listen 80; }"
	vars := []byte(`{"key":"value"}`)

	c1 := computeVersionChecksum(&html, &nginx, vars)
	c2 := computeVersionChecksum(&html, &nginx, vars)
	assert.Equal(t, c1, c2, "checksum must be deterministic")
}

func TestComputeVersionChecksum_DifferentContent(t *testing.T) {
	html1 := "<html>v1</html>"
	html2 := "<html>v2</html>"
	vars := []byte(`{}`)

	c1 := computeVersionChecksum(&html1, nil, vars)
	c2 := computeVersionChecksum(&html2, nil, vars)
	assert.NotEqual(t, c1, c2, "different content must produce different checksum")
}

func TestComputeVersionChecksum_NilContent(t *testing.T) {
	vars := []byte(`{}`)
	// nil HTML and nginx should not panic
	c := computeVersionChecksum(nil, nil, vars)
	assert.NotEmpty(t, c)

	// Verify it matches manual calculation
	h := sha256.New()
	h.Write([]byte("|"))
	h.Write([]byte("|"))
	h.Write(vars)
	expected := fmt.Sprintf("%x", h.Sum(nil))
	assert.Equal(t, expected, c)
}

func TestComputeVersionChecksum_Format(t *testing.T) {
	c := computeVersionChecksum(nil, nil, []byte("{}"))
	// SHA256 produces 64 hex characters
	assert.Len(t, c, 64)
	assert.True(t, isHex(c), "checksum must be hex")
}

func isHex(s string) bool {
	for _, r := range strings.ToLower(s) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
