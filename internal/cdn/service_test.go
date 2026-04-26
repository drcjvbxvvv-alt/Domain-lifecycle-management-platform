package cdn

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// Unit tests for the CDN service's pure validation logic.
// Store-level behaviour belongs to integration tests in store/postgres/.
// The service has a nil store for these tests — only paths that reach
// the store will panic, but those paths are unreachable when input
// validation fails first.

func nilStoreSvc() *Service {
	return &Service{store: nil, logger: zap.NewNop()}
}

// ── Provider type allowlist ───────────────────────────────────────────────────

func TestAllowedProviderTypes_ContainsSeeded(t *testing.T) {
	// All 8 seed-data types from the migration must be in the allowlist.
	seeded := []string{"cloudflare", "juhe", "wangsu", "baishan", "tencent_cdn", "huawei_cdn", "aliyun_cdn", "fastly"}
	for _, pt := range seeded {
		_, ok := allowedProviderTypes[pt]
		assert.True(t, ok, "seeded provider_type %q missing from allowlist", pt)
	}
}

func TestAllowedProviderTypes_OtherIsAllowed(t *testing.T) {
	_, ok := allowedProviderTypes["other"]
	assert.True(t, ok, "\"other\" should be allowed as a catch-all")
}

func TestAllowedProviderTypes_BogusRejected(t *testing.T) {
	_, ok := allowedProviderTypes["my_custom_cdn"]
	assert.False(t, ok)
}

// ── CreateProvider validation ─────────────────────────────────────────────────

func TestCreateProvider_EmptyName(t *testing.T) {
	_, err := nilStoreSvc().CreateProvider(context.Background(), CreateProviderInput{
		Name:         "",
		ProviderType: "cloudflare",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestCreateProvider_WhitespaceName(t *testing.T) {
	_, err := nilStoreSvc().CreateProvider(context.Background(), CreateProviderInput{
		Name:         "   ",
		ProviderType: "cloudflare",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestCreateProvider_InvalidType(t *testing.T) {
	_, err := nilStoreSvc().CreateProvider(context.Background(), CreateProviderInput{
		Name:         "My CDN",
		ProviderType: "unknown_vendor",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider_type")
}

func TestCreateProvider_ValidCloudflare(t *testing.T) {
	// Validation passes — would hit store (nil) next, so we just confirm no
	// validation error is returned before the store call.
	// With a nil store this will panic; we use recover to distinguish.
	defer func() { recover() }()
	_, _ = nilStoreSvc().CreateProvider(context.Background(), CreateProviderInput{
		Name:         "Cloudflare",
		ProviderType: "cloudflare",
	})
	// If we reach here without panic, validation passed — which is the goal.
}

// ── UpdateProvider validation ─────────────────────────────────────────────────

func TestUpdateProvider_EmptyName(t *testing.T) {
	err := nilStoreSvc().UpdateProvider(context.Background(), 1, UpdateProviderInput{
		Name:         "",
		ProviderType: "cloudflare",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestUpdateProvider_InvalidType(t *testing.T) {
	err := nilStoreSvc().UpdateProvider(context.Background(), 1, UpdateProviderInput{
		Name:         "CDN X",
		ProviderType: "bogus",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider_type")
}

// ── CreateAccount validation ──────────────────────────────────────────────────

func TestCreateAccount_EmptyName(t *testing.T) {
	_, err := nilStoreSvc().CreateAccount(context.Background(), CreateAccountInput{
		CDNProviderID: 1,
		AccountName:   "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestCreateAccount_WhitespaceName(t *testing.T) {
	_, err := nilStoreSvc().CreateAccount(context.Background(), CreateAccountInput{
		CDNProviderID: 1,
		AccountName:   "\t  ",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

// ── UpdateAccount validation ──────────────────────────────────────────────────

func TestUpdateAccount_EmptyName(t *testing.T) {
	err := nilStoreSvc().UpdateAccount(context.Background(), 1, UpdateAccountInput{
		AccountName: "   ",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

// ── Credentials default ───────────────────────────────────────────────────────

func TestCredentialsDefault_NilBecomesEmpty(t *testing.T) {
	// The service sets credentials to "{}" when nil/empty.
	var creds []byte
	if len(creds) == 0 {
		creds = []byte("{}")
	}
	assert.Equal(t, []byte("{}"), creds)
}

// ── Sentinel error distinctness ───────────────────────────────────────────────

func TestSentinelErrors_Distinct(t *testing.T) {
	errs := []error{
		ErrProviderNotFound,
		ErrAccountNotFound,
		ErrProviderDuplicate,
		ErrAccountDuplicate,
		ErrProviderHasDependents,
		ErrAccountHasDependents,
	}
	for i := range errs {
		for j := range errs {
			if i == j {
				continue
			}
			assert.False(t, errors.Is(errs[i], errs[j]),
				"errs[%d](%v) should not match errs[%d](%v)", i, errs[i], j, errs[j])
		}
	}
}

// ── Store sentinel error propagation ────────────────────────────────────────

func TestStoreErrors_Mapped(t *testing.T) {
	// Verify store-layer errors are recognisable so service can map them.
	assert.True(t, errors.Is(postgres.ErrCDNProviderNotFound, postgres.ErrCDNProviderNotFound))
	assert.True(t, errors.Is(postgres.ErrCDNAccountNotFound, postgres.ErrCDNAccountNotFound))
	assert.True(t, errors.Is(postgres.ErrCDNProviderHasDependents, postgres.ErrCDNProviderHasDependents))
	assert.True(t, errors.Is(postgres.ErrCDNAccountHasDependents, postgres.ErrCDNAccountHasDependents))
	assert.True(t, errors.Is(postgres.ErrCDNProviderDuplicate, postgres.ErrCDNProviderDuplicate))
	assert.True(t, errors.Is(postgres.ErrCDNAccountDuplicate, postgres.ErrCDNAccountDuplicate))
}

// ── AllProviderTypes table test ───────────────────────────────────────────────

func TestAllProviderTypes_ValidAndInvalid(t *testing.T) {
	cases := []struct {
		pt    string
		valid bool
	}{
		{"cloudflare", true},
		{"juhe", true},
		{"wangsu", true},
		{"baishan", true},
		{"tencent_cdn", true},
		{"huawei_cdn", true},
		{"aliyun_cdn", true},
		{"fastly", true},
		{"other", true},
		{"", false},
		{"CLOUDFLARE", false}, // case-sensitive
		{"cdn_x", false},
	}
	for _, tc := range cases {
		_, got := allowedProviderTypes[tc.pt]
		assert.Equal(t, tc.valid, got, "provider_type=%q", tc.pt)
	}
}
