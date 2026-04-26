package dnsrecord

import (
	"context"
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dnsprovider "domain-platform/pkg/provider/dns"
)

// ── toStringSlice ─────────────────────────────────────────────────────────────

func TestToStringSlice(t *testing.T) {
	assert.Equal(t, []string{}, toStringSlice(nil))
	assert.Equal(t, []string{"ns1.example.com"}, toStringSlice(pq.StringArray{"ns1.example.com"}))
	assert.Equal(t, []string{"a", "b"}, toStringSlice(pq.StringArray{"a", "b"}))
}

// ── fetchExpectedNS ───────────────────────────────────────────────────────────

// nsProvider implements the full dnsprovider.Provider interface.
type nsProvider struct {
	ns    []string
	nsErr error
}

func (p *nsProvider) Name() string { return "ns-stub" }
func (p *nsProvider) ListRecords(_ context.Context, _ string, _ dnsprovider.RecordFilter) ([]dnsprovider.Record, error) {
	return nil, nil
}
func (p *nsProvider) CreateRecord(_ context.Context, _ string, _ dnsprovider.Record) (*dnsprovider.Record, error) {
	return nil, nil
}
func (p *nsProvider) UpdateRecord(_ context.Context, _ string, _ string, _ dnsprovider.Record) (*dnsprovider.Record, error) {
	return nil, nil
}
func (p *nsProvider) DeleteRecord(_ context.Context, _ string, _ string) error { return nil }
func (p *nsProvider) BatchCreateRecords(_ context.Context, _ string, _ []dnsprovider.Record) ([]dnsprovider.Record, error) {
	return nil, nil
}
func (p *nsProvider) BatchDeleteRecords(_ context.Context, _ string, _ []string) error { return nil }
func (p *nsProvider) GetNameservers(_ context.Context, _ string) ([]string, error) {
	return p.ns, p.nsErr
}

func TestFetchExpectedNS_ReturnsNameservers(t *testing.T) {
	ctx := context.Background()
	p := &nsProvider{ns: []string{"ns1.provider.com", "ns2.provider.com"}}
	ns, err := fetchExpectedNS(ctx, p, "example.com")
	require.NoError(t, err)
	assert.Equal(t, []string{"ns1.provider.com", "ns2.provider.com"}, ns)
}

func TestFetchExpectedNS_ProviderError(t *testing.T) {
	ctx := context.Background()
	p := &nsProvider{nsErr: errors.New("provider error")}
	ns, err := fetchExpectedNS(ctx, p, "example.com")
	assert.Error(t, err)
	assert.Equal(t, []string{}, ns)
}

func TestFetchExpectedNS_NilSlice_ReturnsEmpty(t *testing.T) {
	// A provider returning nil nameservers should give back an empty slice.
	ctx := context.Background()
	p := &nsProvider{ns: nil}
	ns, err := fetchExpectedNS(ctx, p, "example.com")
	require.NoError(t, err)
	assert.Equal(t, []string{}, ns)
}

func TestFetchExpectedNS_EmptyNSList(t *testing.T) {
	ctx := context.Background()
	p := &nsProvider{ns: []string{}}
	ns, err := fetchExpectedNS(ctx, p, "example.com")
	require.NoError(t, err)
	assert.Equal(t, []string{}, ns)
}
