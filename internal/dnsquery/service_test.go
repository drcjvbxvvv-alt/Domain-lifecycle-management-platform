package dnsquery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestService() *Service {
	logger, _ := zap.NewDevelopment()
	// Use Google DNS to avoid local resolver variance
	return NewService("8.8.8.8:53", logger)
}

// ── Integration tests (require network) ──────────────────────────────────────

func TestLookup_Google_HasARecordsWithTTL(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	require.NotNil(t, result)
	assert.Equal(t, "google.com", result.FQDN)
	assert.Empty(t, result.Error)
	assert.NotEmpty(t, result.QueriedAt)
	assert.Greater(t, result.ElapsedMs, int64(0))
	assert.Contains(t, result.Nameserver, "8.8.8.8")

	hasA := false
	for _, r := range result.Records {
		if r.Type == TypeA {
			hasA = true
			assert.NotEmpty(t, r.Value)
			assert.Greater(t, r.TTL, uint32(0), "A record should have non-zero TTL")
		}
	}
	assert.True(t, hasA, "expected at least one A record for google.com")
}

func TestLookup_Google_HasNSRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	hasNS := false
	for _, r := range result.Records {
		if r.Type == TypeNS {
			hasNS = true
			assert.NotEmpty(t, r.Value)
			assert.Greater(t, r.TTL, uint32(0))
		}
	}
	assert.True(t, hasNS, "expected NS records for google.com")
}

func TestLookup_Google_HasMXRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	hasMX := false
	for _, r := range result.Records {
		if r.Type == TypeMX {
			hasMX = true
			assert.Greater(t, r.Priority, 0)
			assert.NotEmpty(t, r.Value)
			assert.Greater(t, r.TTL, uint32(0))
		}
	}
	assert.True(t, hasMX, "expected MX records for google.com")
}

func TestLookup_Google_HasSOARecord(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	hasSOA := false
	for _, r := range result.Records {
		if r.Type == TypeSOA {
			hasSOA = true
			assert.Contains(t, r.Value, "google.com", "SOA should reference google's domain")
			assert.Greater(t, r.TTL, uint32(0))
		}
	}
	assert.True(t, hasSOA, "expected SOA record for google.com")
}

func TestLookup_Google_HasTXTRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	hasTXT := false
	for _, r := range result.Records {
		if r.Type == TypeTXT {
			hasTXT = true
			assert.NotEmpty(t, r.Value)
		}
	}
	assert.True(t, hasTXT, "expected TXT records for google.com (SPF etc.)")
}

func TestLookup_CAA_Letsencrypt(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// letsencrypt.org has CAA records
	result := svc.Lookup(ctx, "letsencrypt.org")
	hasCAA := false
	for _, r := range result.Records {
		if r.Type == TypeCAA {
			hasCAA = true
			assert.Contains(t, r.Value, "issue")
		}
	}
	assert.True(t, hasCAA, "expected CAA records for letsencrypt.org")
}

// ── Unit tests (no network) ─────────────────────────────────────────────────

func TestLookup_EmptyFQDN(t *testing.T) {
	svc := newTestService()
	result := svc.Lookup(context.Background(), "")
	assert.Equal(t, "empty FQDN", result.Error)
	assert.Empty(t, result.Records)
}

func TestLookup_TrailingDotStripped(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com.")
	assert.Equal(t, "google.com", result.FQDN)
}

func TestLookup_NonexistentDomain(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "this-domain-definitely-does-not-exist-abc123xyz.com")
	assert.NotNil(t, result)
	assert.Empty(t, result.Error)
	// Records may be empty (NXDOMAIN) — the point is no crash
}

func TestLookup_RecordsSortedByType(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	if len(result.Records) < 2 {
		t.Skip("not enough records to verify sorting")
	}

	for i := 1; i < len(result.Records); i++ {
		prev := typeOrder[result.Records[i-1].Type]
		curr := typeOrder[result.Records[i].Type]
		assert.LessOrEqual(t, prev, curr,
			"records should be sorted by type: %s(%d) <= %s(%d)",
			result.Records[i-1].Type, prev, result.Records[i].Type, curr,
		)
	}
}

func TestLookup_NoDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := svc.Lookup(ctx, "google.com")
	seen := make(map[string]struct{})
	for _, r := range result.Records {
		key := string(r.Type) + "|" + r.Value
		_, dup := seen[key]
		assert.False(t, dup, "duplicate record: %s %s", r.Type, r.Value)
		seen[key] = struct{}{}
	}
}

func TestLookupMultiple_Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	svc := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	fqdns := []string{"google.com", "cloudflare.com", "github.com"}
	results := svc.LookupMultiple(ctx, fqdns)
	require.Len(t, results, 3)

	for i, r := range results {
		assert.Equal(t, fqdns[i], r.FQDN)
		assert.NotEmpty(t, r.QueriedAt)
		assert.Greater(t, r.ElapsedMs, int64(0))
	}
}

func TestNewService_AutoDetectResolver(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	svc := NewService("", logger)
	assert.NotEmpty(t, svc.Nameserver())
	assert.Contains(t, svc.Nameserver(), ":")
}

func TestNewService_CustomResolver(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	svc := NewService("1.1.1.1", logger)
	assert.Equal(t, "1.1.1.1:53", svc.Nameserver())
}

func TestNewService_CustomResolverWithPort(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	svc := NewService("1.1.1.1:5353", logger)
	assert.Equal(t, "1.1.1.1:5353", svc.Nameserver())
}

// ── parseRR unit test ────────────────────────────────────────────────────────

func TestDedup(t *testing.T) {
	records := []Record{
		{Type: TypeA, Value: "1.2.3.4"},
		{Type: TypeA, Value: "1.2.3.4"},     // duplicate
		{Type: TypeA, Value: "5.6.7.8"},
		{Type: TypeNS, Value: "ns1.example"},
		{Type: TypeNS, Value: "ns1.example"}, // duplicate
	}
	result := dedup(records)
	assert.Len(t, result, 3)
}
