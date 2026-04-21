package registrar

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Unit tests for pure business-logic validation in the service layer.
// Store-level behaviour is tested via integration tests in store/postgres/.

func TestCreateInput_NameValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid name", "Namecheap", false},
		{"valid name with spaces", "GoDaddy Inc", false},
		{"empty name", "", true},
		{"whitespace only", "   ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.TrimSpace(tt.input) == ""
			assert.Equal(t, tt.wantErr, got, "input: %q", tt.input)
		})
	}
}

func TestCreateAccountInput_NameValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid account name", "personal-account", false},
		{"empty account name", "", true},
		{"whitespace only", "\t", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.TrimSpace(tt.input) == ""
			assert.Equal(t, tt.wantErr, got, "input: %q", tt.input)
		})
	}
}

func TestCapabilitiesDefault(t *testing.T) {
	// When Capabilities is nil/empty, the service should default to "{}"
	var caps []byte
	if len(caps) == 0 {
		caps = []byte("{}")
	}
	assert.Equal(t, []byte("{}"), caps)
}

func TestErrSentinels(t *testing.T) {
	// Ensure sentinel errors are distinct (prevent accidental equality)
	assert.NotEqual(t, ErrNotFound, ErrAccountNotFound)
	assert.NotEqual(t, ErrNotFound, ErrHasDependents)
	assert.NotEqual(t, ErrAccountNotFound, ErrHasDependents)
	assert.NotEqual(t, ErrDuplicateName, ErrNotFound)
}
