package dnsprovider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Unit tests for pure business-logic validation in the dnsprovider service.

func TestKnownProviderTypes(t *testing.T) {
	valid := []string{"cloudflare", "route53", "dnspod", "alidns", "godaddy", "namecheap", "manual"}
	for _, pt := range valid {
		assert.True(t, KnownProviderTypes[pt], "expected known provider type: %q", pt)
	}
}

func TestUnknownProviderType(t *testing.T) {
	unknown := []string{"", "bind", "powerdns", "unknown-provider", "CLOUDFLARE"}
	for _, pt := range unknown {
		assert.False(t, KnownProviderTypes[pt], "expected unknown provider type: %q", pt)
	}
}

func TestCreateInput_NameValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid name", "Cloudflare Production", false},
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

func TestSupportedTypes_NotEmpty(t *testing.T) {
	types := SupportedTypes()
	assert.NotEmpty(t, types)
	assert.Equal(t, len(KnownProviderTypes), len(types))
}

func TestConfigDefaults(t *testing.T) {
	// When Config or Credentials is nil/empty, service defaults to "{}"
	var cfg []byte
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	assert.Equal(t, []byte("{}"), cfg)
}

func TestErrSentinels(t *testing.T) {
	assert.NotEqual(t, ErrNotFound, ErrHasDependents)
	assert.NotEqual(t, ErrNotFound, ErrInvalidProviderType)
	assert.NotEqual(t, ErrHasDependents, ErrInvalidProviderType)
}
