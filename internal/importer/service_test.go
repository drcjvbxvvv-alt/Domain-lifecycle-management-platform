package importer

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ParseCSV tests ────────────────────────────────────────────────────────────

func TestParseCSV_MinimalRequired(t *testing.T) {
	csv := "fqdn\nexample.com\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "example.com", result.Rows[0].FQDN)
	assert.True(t, result.Rows[0].AutoRenew, "auto_renew defaults to true")
	assert.Nil(t, result.Rows[0].ExpiryDate)
	assert.Empty(t, result.Errors)
}

func TestParseCSV_FullRow(t *testing.T) {
	csv := "fqdn,expiry_date,auto_renew,registrar_account_id,dns_provider_id,tags,notes\n" +
		"shop.example.com,2027-03-15,false,3,7,\"production;core\",Main shop\n"

	result, err := ParseCSV(csv)
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)

	row := result.Rows[0]
	assert.Equal(t, "shop.example.com", row.FQDN)
	require.NotNil(t, row.ExpiryDate)
	assert.Equal(t, 2027, row.ExpiryDate.Year())
	assert.Equal(t, time.March, row.ExpiryDate.Month())
	assert.Equal(t, 15, row.ExpiryDate.Day())
	assert.False(t, row.AutoRenew)
	require.NotNil(t, row.RegistrarAccountID)
	assert.Equal(t, int64(3), *row.RegistrarAccountID)
	require.NotNil(t, row.DNSProviderID)
	assert.Equal(t, int64(7), *row.DNSProviderID)
	assert.Equal(t, []string{"production", "core"}, row.Tags)
	assert.Equal(t, "Main shop", row.Notes)
	assert.Empty(t, result.Errors)
}

func TestParseCSV_FQDNLowercased(t *testing.T) {
	csv := "fqdn\nSHOP.EXAMPLE.COM\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	assert.Equal(t, "shop.example.com", result.Rows[0].FQDN)
}

func TestParseCSV_MultipleRows(t *testing.T) {
	csv := "fqdn,notes\nexample.com,a\nfoo.bar.com,b\nbaz.io,c\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	assert.Len(t, result.Rows, 3)
	assert.Empty(t, result.Errors)
}

func TestParseCSV_EmptyFQDN_RecordedAsError(t *testing.T) {
	// Use whitespace-only fqdn which trims to empty (blank lines are skipped by csv.Reader)
	csv := "fqdn,notes\n  ,some note\nexample.com,ok\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	assert.Len(t, result.Rows, 1)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, 2, result.Errors[0].Line)
	assert.Contains(t, result.Errors[0].Reason, "empty")
}

func TestParseCSV_InvalidExpiryDate_RecordedAsError(t *testing.T) {
	csv := "fqdn,expiry_date\nexample.com,not-a-date\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	assert.Empty(t, result.Rows)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Reason, "expiry_date")
}

func TestParseCSV_InvalidAutoRenew_RecordedAsError(t *testing.T) {
	csv := "fqdn,auto_renew\nexample.com,maybe\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	assert.Empty(t, result.Rows)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Reason, "auto_renew")
}

func TestParseCSV_InvalidRegistrarAccountID_RecordedAsError(t *testing.T) {
	csv := "fqdn,registrar_account_id\nexample.com,abc\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	assert.Empty(t, result.Rows)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Reason, "registrar_account_id")
}

func TestParseCSV_EmptyCSV_ReturnsError(t *testing.T) {
	_, err := ParseCSV("")
	assert.ErrorIs(t, err, ErrEmptyCSV)
}

func TestParseCSV_HeaderOnly_ReturnsError(t *testing.T) {
	_, err := ParseCSV("fqdn\n")
	assert.ErrorIs(t, err, ErrEmptyCSV)
}

func TestParseCSV_MissingRequiredHeader_ReturnsError(t *testing.T) {
	_, err := ParseCSV("notes,expiry_date\nsome note,2027-01-01\n")
	assert.ErrorIs(t, err, ErrInvalidHeader)
}

func TestParseCSV_ExceedsMaxRows_ReturnsError(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("fqdn\n")
	for i := 0; i <= MaxRows; i++ {
		sb.WriteString("a" + strings.Repeat("x", 5) + ".example.com\n")
	}
	_, err := ParseCSV(sb.String())
	assert.ErrorIs(t, err, ErrTooManyRows)
}

func TestParseCSV_TagsSemicolonSplit(t *testing.T) {
	csv := "fqdn,tags\nexample.com,alpha;beta; gamma\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, result.Rows[0].Tags)
}

func TestParseCSV_ColumnOrderIndependent(t *testing.T) {
	csv := "notes,fqdn,tags\nsome note,order.test.com,x;y\n"
	result, err := ParseCSV(csv)
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "order.test.com", result.Rows[0].FQDN)
	assert.Equal(t, "some note", result.Rows[0].Notes)
}

// ── validateFQDN tests ────────────────────────────────────────────────────────

func TestValidateFQDN_Valid(t *testing.T) {
	valid := []string{
		"example.com",
		"shop.example.com",
		"foo-bar.io",
		"a.b.c.d",
	}
	for _, fqdn := range valid {
		t.Run(fqdn, func(t *testing.T) {
			assert.NoError(t, validateFQDN(fqdn))
		})
	}
}

func TestValidateFQDN_Invalid(t *testing.T) {
	cases := []struct {
		fqdn   string
		reason string
	}{
		{"nodot", "no dot"},
		{"-starts.com", "starts with hyphen"},
		{"ends-.com", "ends with hyphen"},
		{"has space.com", "space character"},
		{strings.Repeat("a", 254), "exceeds 253 chars"},
		{"empty..label.com", "empty label"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			err := validateFQDN(tc.fqdn)
			assert.Error(t, err, "expected error for %q (%s)", tc.fqdn, tc.reason)
		})
	}
}
