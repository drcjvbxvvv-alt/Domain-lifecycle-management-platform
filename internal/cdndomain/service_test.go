package cdndomain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	cdnprovider "domain-platform/pkg/provider/cdn"
	"domain-platform/store/postgres"
)

// ── bindingToResponse ─────────────────────────────────────────────────────────

func TestBindingToResponse_PopulatesAllFields(t *testing.T) {
	cname := "abc123.cdnprovider.net"
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	b := &postgres.DomainCDNBinding{
		ID:           42,
		UUID:         "test-uuid",
		DomainID:     10,
		CDNAccountID: 5,
		CDNCNAME:     &cname,
		BusinessType: "web",
		Status:       "online",
		CreatedAt:    now,
		UpdatedAt:    now.Add(time.Hour),
	}

	resp := bindingToResponse(b)

	assert.Equal(t, int64(42), resp.ID)
	assert.Equal(t, "test-uuid", resp.UUID)
	assert.Equal(t, int64(10), resp.DomainID)
	assert.Equal(t, int64(5), resp.CDNAccountID)
	assert.Equal(t, &cname, resp.CDNCNAME)
	assert.Equal(t, "web", resp.BusinessType)
	assert.Equal(t, "online", resp.Status)
	assert.Equal(t, "2026-04-26T12:00:00Z", resp.CreatedAt)
	assert.Equal(t, "2026-04-26T13:00:00Z", resp.UpdatedAt)
}

func TestBindingToResponse_NilCNAME(t *testing.T) {
	b := &postgres.DomainCDNBinding{
		ID:           1,
		UUID:         "u1",
		BusinessType: "download",
		Status:       "offline",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	resp := bindingToResponse(b)
	assert.Nil(t, resp.CDNCNAME)
	assert.Equal(t, "offline", resp.Status)
	assert.Equal(t, "download", resp.BusinessType)
}

func TestBindingToResponse_MediaType(t *testing.T) {
	b := &postgres.DomainCDNBinding{
		ID:           2,
		UUID:         "u2",
		BusinessType: "media",
		Status:       "configuring",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	resp := bindingToResponse(b)
	assert.Equal(t, "media", resp.BusinessType)
	assert.Equal(t, "configuring", resp.Status)
}

// ── Error sentinel identity ────────────────────────────────────────────────────

func TestErrorSentinels_AreDistinct(t *testing.T) {
	assert.NotEqual(t, ErrBindingNotFound, ErrBindingAlreadyExists)
	assert.NotEqual(t, ErrBindingNotFound, ErrNoCDNProvider)
	assert.NotEqual(t, ErrBindingAlreadyExists, ErrNoCDNProvider)
	assert.NotEqual(t, ErrAccountNotFound, ErrNoCDNProvider)
}

func TestErrorSentinels_MatchPostgresSentinels(t *testing.T) {
	// These must remain aliased so callers can errors.Is against postgres sentinels.
	assert.Equal(t, postgres.ErrCDNBindingNotFound, ErrBindingNotFound,
		"ErrBindingNotFound must alias postgres.ErrCDNBindingNotFound")
	assert.Equal(t, postgres.ErrCDNBindingAlreadyExists, ErrBindingAlreadyExists,
		"ErrBindingAlreadyExists must alias postgres.ErrCDNBindingAlreadyExists")
	assert.Equal(t, postgres.ErrCDNAccountNotFound, ErrAccountNotFound,
		"ErrAccountNotFound must alias postgres.ErrCDNAccountNotFound")
}

// ── BindingResponse zero-value guards ─────────────────────────────────────────

func TestBindingResponse_CNAMEDefaultsNil(t *testing.T) {
	var resp BindingResponse
	assert.Nil(t, resp.CDNCNAME)
}

func TestBindingResponse_StatusDefaultsEmpty(t *testing.T) {
	var resp BindingResponse
	assert.Empty(t, resp.Status)
}

// ── Business type constant guards ─────────────────────────────────────────────

// Guard against accidental changes to cdn provider constant values that would
// silently break the default in BindDomain.
func TestBusinessTypeConstants_Values(t *testing.T) {
	assert.Equal(t, "web", cdnprovider.BusinessTypeWeb)
	assert.Equal(t, "download", cdnprovider.BusinessTypeDownload)
	assert.Equal(t, "media", cdnprovider.BusinessTypeMedia)
}

// ── DomainStatus constant guards ──────────────────────────────────────────────

func TestDomainStatusConstants_UsedByRefreshStatus(t *testing.T) {
	// RefreshStatus sets status to DomainStatusOffline when the domain is
	// missing on the CDN side. Guard this value.
	assert.Equal(t, "offline", cdnprovider.DomainStatusOffline)
	assert.Equal(t, "online", cdnprovider.DomainStatusOnline)
}
