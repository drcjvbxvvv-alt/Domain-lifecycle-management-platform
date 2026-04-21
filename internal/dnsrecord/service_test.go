package dnsrecord

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── CreateRecordInput.Validate ───────────────────────────────────────────────

func TestCreateRecordInput_Validate_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input CreateRecordInput
	}{
		{"A record", CreateRecordInput{Type: "A", Name: "example.com", Content: "1.2.3.4", TTL: 300}},
		{"AAAA record", CreateRecordInput{Type: "AAAA", Name: "example.com", Content: "::1"}},
		{"CNAME record", CreateRecordInput{Type: "CNAME", Name: "www.example.com", Content: "example.com"}},
		{"MX record", CreateRecordInput{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: 10}},
		{"TXT record", CreateRecordInput{Type: "TXT", Name: "example.com", Content: "v=spf1 ~all"}},
		{"lowercase type", CreateRecordInput{Type: "a", Name: "example.com", Content: "1.2.3.4"}},
		{"TTL zero", CreateRecordInput{Type: "A", Name: "example.com", Content: "1.2.3.4", TTL: 0}},
		{"proxied", CreateRecordInput{Type: "A", Name: "example.com", Content: "1.2.3.4", Proxied: true}},
		{"CAA record", CreateRecordInput{Type: "CAA", Name: "example.com", Content: `0 issue "letsencrypt.org"`}},
		{"SRV record", CreateRecordInput{Type: "SRV", Name: "_sip._tcp.example.com", Content: "sip.example.com", Priority: 10}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, tc.input.Validate())
		})
	}
}

func TestCreateRecordInput_Validate_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input CreateRecordInput
		err   string
	}{
		{"empty type", CreateRecordInput{Type: "", Name: "example.com", Content: "1.2.3.4"}, "unsupported record type"},
		{"unknown type", CreateRecordInput{Type: "FAKE", Name: "example.com", Content: "val"}, "unsupported record type"},
		{"empty name", CreateRecordInput{Type: "A", Name: "", Content: "1.2.3.4"}, "name is required"},
		{"empty content", CreateRecordInput{Type: "A", Name: "example.com", Content: ""}, "content is required"},
		{"negative TTL", CreateRecordInput{Type: "A", Name: "example.com", Content: "1.2.3.4", TTL: -1}, "TTL must be >= 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
		})
	}
}

// ── UpdateRecordInput.Validate ───────────────────────────────────────────────

func TestUpdateRecordInput_Validate_Valid(t *testing.T) {
	in := UpdateRecordInput{Type: "A", Name: "example.com", Content: "5.6.7.8", TTL: 600}
	assert.NoError(t, in.Validate())
}

func TestUpdateRecordInput_Validate_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input UpdateRecordInput
		err   string
	}{
		{"empty type", UpdateRecordInput{Type: "", Name: "example.com", Content: "1.2.3.4"}, "unsupported record type"},
		{"empty name", UpdateRecordInput{Type: "A", Name: "", Content: "1.2.3.4"}, "name is required"},
		{"empty content", UpdateRecordInput{Type: "A", Name: "example.com", Content: ""}, "content is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
		})
	}
}

// ── Type normalisation ───────────────────────────────────────────────────────

func TestValidate_NormalisesType(t *testing.T) {
	in := CreateRecordInput{Type: "  cname ", Name: "www.example.com", Content: "cdn.example.com"}
	assert.NoError(t, in.Validate())
	assert.Equal(t, "CNAME", in.Type, "type should be uppercased and trimmed")
}
