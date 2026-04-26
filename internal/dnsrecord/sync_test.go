package dnsrecord

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dnsprovider "domain-platform/pkg/provider/dns"
)

// ── ptrInt ────────────────────────────────────────────────────────────────────

func TestPtrInt_Zero(t *testing.T) {
	assert.Nil(t, ptrInt(0))
}

func TestPtrInt_NonZero(t *testing.T) {
	p := ptrInt(10)
	assert.NotNil(t, p)
	assert.Equal(t, 10, *p)
}

// ── providerRecordToRow ───────────────────────────────────────────────────────

func TestProviderRecordToRow_BasicA(t *testing.T) {
	rec := dnsprovider.Record{
		ID:      "prov-1",
		Type:    "A",
		Name:    "www.example.com",
		Content: "1.2.3.4",
		TTL:     300,
	}
	row := providerRecordToRow(10, 5, rec)

	assert.Equal(t, int64(10), row.DomainID)
	assert.Equal(t, int64(5), *row.DNSProviderID)
	assert.Equal(t, "prov-1", *row.ProviderRecordID)
	assert.Equal(t, "A", row.RecordType)
	assert.Equal(t, "www.example.com", row.Name)
	assert.Equal(t, "1.2.3.4", row.Content)
	assert.Equal(t, 300, row.TTL)
	assert.False(t, row.Proxied)
	assert.Nil(t, row.Priority)
}

func TestProviderRecordToRow_MXWithPriority(t *testing.T) {
	rec := dnsprovider.Record{
		ID:       "prov-2",
		Type:     "MX",
		Name:     "example.com",
		Content:  "mail.example.com",
		TTL:      600,
		Priority: 10,
	}
	row := providerRecordToRow(10, 5, rec)

	assert.Equal(t, "MX", row.RecordType)
	assert.NotNil(t, row.Priority)
	assert.Equal(t, 10, *row.Priority)
}

func TestProviderRecordToRow_EmptyID(t *testing.T) {
	rec := dnsprovider.Record{
		ID:      "",
		Type:    "TXT",
		Name:    "example.com",
		Content: "v=spf1 ~all",
		TTL:     300,
	}
	row := providerRecordToRow(10, 5, rec)
	assert.Nil(t, row.ProviderRecordID)
}

func TestProviderRecordToRow_Proxied(t *testing.T) {
	rec := dnsprovider.Record{
		ID:      "cf-1",
		Type:    "A",
		Name:    "www.example.com",
		Content: "1.2.3.4",
		TTL:     1,
		Proxied: true,
	}
	row := providerRecordToRow(10, 5, rec)
	assert.True(t, row.Proxied)
}

func TestProviderRecordToRow_ExtraInitialised(t *testing.T) {
	rec := dnsprovider.Record{ID: "x", Type: "A", Name: "a", Content: "1.1.1.1", TTL: 60}
	row := providerRecordToRow(1, 1, rec)
	assert.Equal(t, `{}`, string(row.Extra))
}
