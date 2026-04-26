package dnsrecord

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
)

// NSCheckResult holds the outcome of a single NS delegation check.
type NSCheckResult struct {
	Status    string    // "verified" | "mismatch" | "error"
	Actual    []string  // observed nameservers (lowercased, sorted, trailing dot stripped)
	Expected  []string  // expected nameservers (normalised the same way)
	CheckedAt time.Time
}

// CheckNSDelegation uses the system resolver to look up the NS records for
// the given domain and compares them to the expectedNS list (case-insensitive,
// order-independent, trailing-dot-insensitive).
//
// It returns:
//   - "verified"  when every expected NS is present in the live NS set
//   - "mismatch"  when the sets differ
//   - "error"     when the lookup itself failed (network error, NXDOMAIN, etc.)
func CheckNSDelegation(ctx context.Context, domain string, expectedNS []string) (NSCheckResult, error) {
	result := NSCheckResult{
		Expected:  normaliseNSList(expectedNS),
		CheckedAt: time.Now().UTC(),
	}

	// net.LookupNS uses the system resolver. The context deadline applies.
	nss, err := net.DefaultResolver.LookupNS(ctx, domain)
	if err != nil {
		result.Status = "error"
		result.Actual = []string{}
		return result, err
	}

	actual := make([]string, 0, len(nss))
	for _, ns := range nss {
		actual = append(actual, normaliseNS(ns.Host))
	}
	sort.Strings(actual)
	result.Actual = actual

	if nsListsMatch(result.Expected, actual) {
		result.Status = "verified"
	} else {
		result.Status = "mismatch"
	}
	return result, nil
}

// nsListsMatch returns true when both lists contain the same entries
// (order-independent). Empty expected always returns false (we require at
// least one expected NS to be present before declaring "verified").
func nsListsMatch(expected, actual []string) bool {
	if len(expected) == 0 {
		return false
	}
	if len(expected) != len(actual) {
		return false
	}
	// Both are already sorted.
	for i := range expected {
		if expected[i] != actual[i] {
			return false
		}
	}
	return true
}

// normaliseNSList normalises a list of NS hostnames and sorts them.
func normaliseNSList(ns []string) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		if n = normaliseNS(n); n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// normaliseNS strips trailing dot and lowercases an NS hostname.
func normaliseNS(ns string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(ns), "."))
}
