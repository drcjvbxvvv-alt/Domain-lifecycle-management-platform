package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTLD(t *testing.T) {
	tests := []struct {
		fqdn string
		want string
	}{
		// Simple TLDs
		{"example.com", ".com"},
		{"www.example.com", ".com"},
		{"shop.example.com", ".com"},
		{"example.net", ".net"},
		{"example.org", ".org"},
		{"example.io", ".io"},
		{"example.ai", ".ai"},
		{"example.app", ".app"},
		{"example.dev", ".dev"},

		// Country-code + generic TLD (ccSLD)
		{"test.co.uk", ".co.uk"},
		{"www.example.co.uk", ".co.uk"},
		{"api.shop.co.uk", ".co.uk"},
		{"example.com.au", ".com.au"},
		{"shop.example.com.au", ".com.au"},
		{"example.co.nz", ".co.nz"},
		{"example.co.jp", ".co.jp"},
		{"example.com.cn", ".com.cn"},
		{"example.org.uk", ".org.uk"},
		{"example.me.uk", ".me.uk"},
		{"example.net.au", ".net.au"},
		{"example.org.au", ".org.au"},

		// ccTLD only (no SLD pattern)
		{"example.de", ".de"},
		{"example.fr", ".fr"},
		{"example.cn", ".cn"},

		// Long SLD (> 4 chars) — not treated as ccSLD
		{"example.github.io", ".io"},
		{"www.example.london", ".london"},

		// Case insensitive
		{"Example.COM", ".com"},
		{"TEST.CO.UK", ".co.uk"},

		// Trailing dot
		{"example.com.", ".com"},
	}

	for _, tt := range tests {
		t.Run(tt.fqdn, func(t *testing.T) {
			got := ExtractTLD(tt.fqdn)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTransferSentinelErrors(t *testing.T) {
	assert.NotEqual(t, ErrTransferAlreadyPending, ErrNoActiveTransfer)
	assert.NotEqual(t, ErrTransferAlreadyPending, ErrDomainNotFound)
	assert.NotEqual(t, ErrNoActiveTransfer, ErrDuplicateFQDN)
}
