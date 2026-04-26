package gfw

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStaticASNDatabase_KnownCDNIPs(t *testing.T) {
	db := DefaultASNDatabase()

	tests := []struct {
		ip      string
		wantCDN string
	}{
		// Cloudflare ranges
		{"104.16.0.1", "cloudflare"},
		{"172.64.0.1", "cloudflare"},
		{"173.245.48.1", "cloudflare"},
		// Fastly ranges
		{"151.101.0.1", "fastly"},
		{"199.232.0.1", "fastly"},
		// Akamai
		{"23.32.0.1", "akamai"},
		// CloudFront
		{"52.84.0.1", "cloudfront"},
		// Google
		{"34.64.0.1", "google"},
		// Non-CDN IPs
		{"8.8.8.8", ""},
		{"1.2.3.4", ""},
		{"93.184.216.34", ""},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			got := db.Lookup(tt.ip)
			assert.Equal(t, tt.wantCDN, got, "unexpected CDN for %s", tt.ip)
		})
	}
}

func TestStaticASNDatabase_InvalidInputs(t *testing.T) {
	db := DefaultASNDatabase()
	assert.Equal(t, "", db.Lookup(""))
	assert.Equal(t, "", db.Lookup("not-an-ip"))
	assert.Equal(t, "", db.Lookup("256.0.0.1"))
}

func TestNewStaticASNDatabase_SkipsInvalidCIDR(t *testing.T) {
	db := NewStaticASNDatabase([][2]string{
		{"invalid-cidr", "test"},
		{"192.0.2.0/24", "test"},
	})
	// Invalid CIDR skipped; valid one works.
	assert.Equal(t, "test", db.Lookup("192.0.2.1"))
	assert.Equal(t, "", db.Lookup("10.0.0.1"))
}

func TestNewStaticASNDatabase_Empty(t *testing.T) {
	db := NewStaticASNDatabase(nil)
	assert.Equal(t, "", db.Lookup("8.8.8.8"))
}
