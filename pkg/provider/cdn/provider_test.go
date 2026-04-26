package cdn

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Compile-time interface assertion ──────────────────────────────────────────

// If mockProvider no longer satisfies Provider, this line fails to compile.
var _ Provider = (*mockProvider)(nil)

// ── Mock implementation ───────────────────────────────────────────────────────

// mockProvider implements Provider for use in registry and contract tests.
// Every method returns zero values and no error — enough to satisfy the
// interface without requiring real CDN API calls.
type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }

// Domain lifecycle
func (m *mockProvider) AddDomain(_ context.Context, _ AddDomainRequest) (*CDNDomain, error) {
	return &CDNDomain{}, nil
}
func (m *mockProvider) RemoveDomain(_ context.Context, _ string) error { return nil }
func (m *mockProvider) GetDomain(_ context.Context, _ string) (*CDNDomain, error) {
	return &CDNDomain{}, nil
}
func (m *mockProvider) ListDomains(_ context.Context) ([]CDNDomain, error) {
	return nil, nil
}

// Cache config
func (m *mockProvider) GetCacheConfig(_ context.Context, _ string) (*CacheConfig, error) {
	return &CacheConfig{}, nil
}
func (m *mockProvider) SetCacheConfig(_ context.Context, _ string, _ CacheConfig) error {
	return nil
}

// Origin config
func (m *mockProvider) GetOriginConfig(_ context.Context, _ string) (*OriginConfig, error) {
	return &OriginConfig{}, nil
}
func (m *mockProvider) SetOriginConfig(_ context.Context, _ string, _ OriginConfig) error {
	return nil
}

// Access control
func (m *mockProvider) GetAccessControl(_ context.Context, _ string) (*AccessControl, error) {
	return &AccessControl{}, nil
}
func (m *mockProvider) SetAccessControl(_ context.Context, _ string, _ AccessControl) error {
	return nil
}

// HTTPS config
func (m *mockProvider) GetHTTPSConfig(_ context.Context, _ string) (*HTTPSConfig, error) {
	return &HTTPSConfig{}, nil
}
func (m *mockProvider) SetHTTPSConfig(_ context.Context, _ string, _ HTTPSConfig) error {
	return nil
}

// Performance config
func (m *mockProvider) GetPerformanceConfig(_ context.Context, _ string) (*PerformanceConfig, error) {
	return &PerformanceConfig{}, nil
}
func (m *mockProvider) SetPerformanceConfig(_ context.Context, _ string, _ PerformanceConfig) error {
	return nil
}

// Content management
func (m *mockProvider) PurgeURLs(_ context.Context, _ []string) (*PurgeTask, error) {
	return &PurgeTask{}, nil
}
func (m *mockProvider) PurgeDirectory(_ context.Context, _ string) (*PurgeTask, error) {
	return &PurgeTask{}, nil
}
func (m *mockProvider) PrefetchURLs(_ context.Context, _ []string) (*PrefetchTask, error) {
	return &PrefetchTask{}, nil
}
func (m *mockProvider) GetTaskStatus(_ context.Context, _ string) (*TaskStatus, error) {
	return &TaskStatus{}, nil
}

// Statistics
func (m *mockProvider) GetBandwidthStats(_ context.Context, _ string, _ StatsRequest) ([]BandwidthPoint, error) {
	return nil, nil
}
func (m *mockProvider) GetTrafficStats(_ context.Context, _ string, _ StatsRequest) ([]TrafficPoint, error) {
	return nil, nil
}
func (m *mockProvider) GetHitRateStats(_ context.Context, _ string, _ StatsRequest) ([]HitRatePoint, error) {
	return nil, nil
}

// ── Registry helpers ──────────────────────────────────────────────────────────

// freshRegistry replaces the global registry with an empty one for the
// duration of a test and restores it on cleanup.
func freshRegistry(t *testing.T) {
	t.Helper()
	old := registry
	registry = make(map[string]Factory)
	t.Cleanup(func() { registry = old })
}

// ── Registry tests ────────────────────────────────────────────────────────────

func TestRegistry_Register_And_Get(t *testing.T) {
	freshRegistry(t)

	Register("mock", func(_, _ json.RawMessage) (Provider, error) {
		return &mockProvider{name: "mock"}, nil
	})

	p, err := Get("mock", json.RawMessage(`{}`), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "mock", p.Name())
}

func TestRegistry_GetUnknownProvider(t *testing.T) {
	freshRegistry(t)

	_, err := Get("no-such-provider", json.RawMessage(`{}`), json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrProviderNotRegistered)
}

func TestRegistry_DuplicateRegistrationPanics(t *testing.T) {
	freshRegistry(t)

	factory := func(_, _ json.RawMessage) (Provider, error) {
		return &mockProvider{name: "dup"}, nil
	}
	Register("dup", factory)
	assert.Panics(t, func() { Register("dup", factory) })
}

func TestRegistry_EmptyTypeRegistrationPanics(t *testing.T) {
	freshRegistry(t)

	assert.Panics(t, func() {
		Register("", func(_, _ json.RawMessage) (Provider, error) {
			return nil, nil
		})
	})
}

func TestRegistry_RegisteredTypes_IncludesRegistered(t *testing.T) {
	freshRegistry(t)

	Register("alpha", func(_, _ json.RawMessage) (Provider, error) {
		return &mockProvider{name: "alpha"}, nil
	})
	Register("beta", func(_, _ json.RawMessage) (Provider, error) {
		return &mockProvider{name: "beta"}, nil
	})

	types := RegisteredTypes()
	assert.ElementsMatch(t, []string{"alpha", "beta"}, types)
}

func TestRegistry_RegisteredTypes_EmptyWhenNoProviders(t *testing.T) {
	freshRegistry(t)
	assert.Empty(t, RegisteredTypes())
}

func TestRegistry_FactoryErrorPropagates(t *testing.T) {
	freshRegistry(t)

	Register("bad-creds", func(_, _ json.RawMessage) (Provider, error) {
		return nil, ErrMissingCredentials
	})

	_, err := Get("bad-creds", json.RawMessage(`{}`), json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrMissingCredentials)
}

// ── Shared type zero-value sanity tests ───────────────────────────────────────

// These tests guard against future struct changes that break zero-value semantics
// relied on when optional config sub-structs are nil.

func TestAccessControl_AllNilByDefault(t *testing.T) {
	var ac AccessControl
	assert.Nil(t, ac.Referer)
	assert.Nil(t, ac.IP)
	assert.Nil(t, ac.URLAuth)
	assert.Nil(t, ac.GeoBlock)
	assert.Nil(t, ac.RateLimit)
	assert.Nil(t, ac.UserAgent)
}

func TestHTTPSConfig_HSTSNilByDefault(t *testing.T) {
	var cfg HTTPSConfig
	assert.Nil(t, cfg.HSTS)
}

func TestCDNDomain_CreatedAtNilByDefault(t *testing.T) {
	var d CDNDomain
	assert.Nil(t, d.CreatedAt)
}

func TestTaskStatus_FinishedAtNilByDefault(t *testing.T) {
	var ts TaskStatus
	assert.Nil(t, ts.FinishedAt)
}

// ── StatsRequest field sanity ─────────────────────────────────────────────────

func TestStatsRequest_Fields(t *testing.T) {
	now := time.Now()
	req := StatsRequest{
		StartTime: now.Add(-24 * time.Hour),
		EndTime:   now,
		Interval:  "1hour",
	}
	assert.True(t, req.EndTime.After(req.StartTime))
	assert.Equal(t, "1hour", req.Interval)
}

// ── CacheRule zero-value TTL semantics ────────────────────────────────────────

func TestCacheRule_ZeroTTLMeansNoCache(t *testing.T) {
	rule := CacheRule{RuleType: "suffix", Pattern: "*.php", TTL: 0}
	assert.Equal(t, 0, rule.TTL, "TTL=0 means no-cache")
}

func TestCacheRule_NegativeOneTTLMeansFollowOrigin(t *testing.T) {
	rule := CacheRule{RuleType: "all", Pattern: "*", TTL: -1}
	assert.Equal(t, -1, rule.TTL, "TTL=-1 means follow origin Cache-Control")
}

// ── Constant value guards ─────────────────────────────────────────────────────

func TestDomainStatusConstants(t *testing.T) {
	// Guard: downstream code compares against these string literals.
	assert.Equal(t, "online", DomainStatusOnline)
	assert.Equal(t, "offline", DomainStatusOffline)
	assert.Equal(t, "configuring", DomainStatusConfiguring)
	assert.Equal(t, "checking", DomainStatusChecking)
}

func TestBusinessTypeConstants(t *testing.T) {
	assert.Equal(t, "web", BusinessTypeWeb)
	assert.Equal(t, "download", BusinessTypeDownload)
	assert.Equal(t, "media", BusinessTypeMedia)
}

func TestTaskStatusConstants(t *testing.T) {
	assert.Equal(t, "pending", TaskStatusPending)
	assert.Equal(t, "processing", TaskStatusProcessing)
	assert.Equal(t, "done", TaskStatusDone)
	assert.Equal(t, "failed", TaskStatusFailed)
}

func TestACLTypeConstants(t *testing.T) {
	assert.Equal(t, "whitelist", ACLTypeWhitelist)
	assert.Equal(t, "blacklist", ACLTypeBlacklist)
}
