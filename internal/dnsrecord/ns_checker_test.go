package dnsrecord

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── normaliseNS ───────────────────────────────────────────────────────────────

func TestNormaliseNS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ns1.example.com.", "ns1.example.com"},
		{"NS1.EXAMPLE.COM", "ns1.example.com"},
		{"ns1.example.com", "ns1.example.com"},
		{"  NS2.EXAMPLE.COM.  ", "ns2.example.com"},
		{"", ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, normaliseNS(tc.input), "input: %q", tc.input)
	}
}

// ── normaliseNSList ───────────────────────────────────────────────────────────

func TestNormaliseNSList(t *testing.T) {
	in := []string{"NS2.EXAMPLE.COM.", "ns1.example.com"}
	out := normaliseNSList(in)
	// Must be sorted and normalised.
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, out)
}

func TestNormaliseNSList_Empty(t *testing.T) {
	assert.Equal(t, []string{}, normaliseNSList(nil))
	assert.Equal(t, []string{}, normaliseNSList([]string{}))
}

func TestNormaliseNSList_FiltersEmptyStrings(t *testing.T) {
	out := normaliseNSList([]string{"", "ns1.example.com.", ""})
	assert.Equal(t, []string{"ns1.example.com"}, out)
}

// ── nsListsMatch ──────────────────────────────────────────────────────────────

func TestNSListsMatch_Equal(t *testing.T) {
	a := []string{"ns1.example.com", "ns2.example.com"}
	b := []string{"ns1.example.com", "ns2.example.com"}
	assert.True(t, nsListsMatch(a, b))
}

func TestNSListsMatch_Mismatch(t *testing.T) {
	a := []string{"ns1.example.com", "ns2.example.com"}
	b := []string{"ns1.example.com", "ns3.example.com"}
	assert.False(t, nsListsMatch(a, b))
}

func TestNSListsMatch_DifferentLength(t *testing.T) {
	a := []string{"ns1.example.com"}
	b := []string{"ns1.example.com", "ns2.example.com"}
	assert.False(t, nsListsMatch(a, b))
}

func TestNSListsMatch_EmptyExpected(t *testing.T) {
	// Empty expected list is never "verified" — we require at least one NS.
	assert.False(t, nsListsMatch([]string{}, []string{}))
	assert.False(t, nsListsMatch([]string{}, []string{"ns1.example.com"}))
}

func TestNSListsMatch_BothEmpty(t *testing.T) {
	assert.False(t, nsListsMatch(nil, nil))
}

// ── CheckNSDelegation integration-style test ──────────────────────────────────
// We skip live DNS queries in unit tests; the tests above cover the pure logic.
// The worker handler integration tests should cover the full flow with a mocked resolver.
