package gfw

import (
	"net"
)

// ASNDatabase maps IP addresses to their owning CDN provider name.
// A non-empty return value means "this IP belongs to a known CDN", which
// is used by the Analyzer to avoid false-positive blocking verdicts when a
// CDN serves different IP ranges from different PoPs.
type ASNDatabase interface {
	// Lookup returns the CDN name for the given IP, or an empty string if the
	// IP does not belong to any known CDN.
	Lookup(ip string) string
}

// cdnCIDR is a single CIDR entry associated with a CDN name.
type cdnCIDR struct {
	net  *net.IPNet
	name string
}

// StaticASNDatabase is a compile-time CDN CIDR list.  It is intentionally
// small and conservative — only widely-used CDN ranges that are likely to
// produce false-positive GFW blocking signals are included.
//
// Operators can extend the list at startup via NewStaticASNDatabase.
type StaticASNDatabase struct {
	entries []cdnCIDR
}

// NewStaticASNDatabase builds a database from a list of (cidr, name) pairs.
// Invalid CIDR strings are silently skipped.
func NewStaticASNDatabase(cidrs [][2]string) *StaticASNDatabase {
	db := &StaticASNDatabase{}
	for _, pair := range cidrs {
		_, ipNet, err := net.ParseCIDR(pair[0])
		if err != nil {
			continue
		}
		db.entries = append(db.entries, cdnCIDR{net: ipNet, name: pair[1]})
	}
	return db
}

// Lookup implements ASNDatabase.
func (db *StaticASNDatabase) Lookup(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	for _, e := range db.entries {
		if e.net.Contains(parsed) {
			return e.name
		}
	}
	return ""
}

// DefaultCDNCIDRs is the built-in list of CDN IP ranges used to suppress
// false-positive GFW verdicts.  Sources: official published IP lists from
// each CDN vendor (as of early 2026).  Update periodically.
var DefaultCDNCIDRs = [][2]string{
	// Cloudflare
	{"103.21.244.0/22", "cloudflare"},
	{"103.22.200.0/22", "cloudflare"},
	{"103.31.4.0/22", "cloudflare"},
	{"104.16.0.0/13", "cloudflare"},
	{"104.24.0.0/14", "cloudflare"},
	{"108.162.192.0/18", "cloudflare"},
	{"131.0.72.0/22", "cloudflare"},
	{"141.101.64.0/18", "cloudflare"},
	{"162.158.0.0/15", "cloudflare"},
	{"172.64.0.0/13", "cloudflare"},
	{"173.245.48.0/20", "cloudflare"},
	{"188.114.96.0/20", "cloudflare"},
	{"190.93.240.0/20", "cloudflare"},
	{"197.234.240.0/22", "cloudflare"},
	{"198.41.128.0/17", "cloudflare"},
	// Fastly
	{"23.235.32.0/20", "fastly"},
	{"43.249.72.0/22", "fastly"},
	{"103.244.50.0/24", "fastly"},
	{"103.245.222.0/23", "fastly"},
	{"103.245.224.0/24", "fastly"},
	{"104.156.80.0/20", "fastly"},
	{"151.101.0.0/16", "fastly"},
	{"157.52.64.0/18", "fastly"},
	{"167.82.0.0/17", "fastly"},
	{"172.111.64.0/18", "fastly"},
	{"185.31.16.0/22", "fastly"},
	{"199.27.72.0/21", "fastly"},
	{"199.232.0.0/16", "fastly"},
	// Akamai (sample — full list is dynamic; include common edge ranges)
	{"23.32.0.0/11", "akamai"},
	{"23.192.0.0/11", "akamai"},
	{"2.16.0.0/13", "akamai"},
	{"92.122.0.0/15", "akamai"},
	{"184.24.0.0/13", "akamai"},
	// AWS CloudFront
	{"13.32.0.0/15", "cloudfront"},
	{"13.35.0.0/16", "cloudfront"},
	{"52.84.0.0/15", "cloudfront"},
	{"54.182.0.0/16", "cloudfront"},
	{"54.192.0.0/16", "cloudfront"},
	{"54.230.0.0/16", "cloudfront"},
	{"54.239.128.0/18", "cloudfront"},
	{"99.86.0.0/16", "cloudfront"},
	{"205.251.192.0/19", "cloudfront"},
	// Google Cloud / GFE (used by many SaaS products)
	{"34.64.0.0/10", "google"},
	{"34.128.0.0/10", "google"},
	{"35.184.0.0/13", "google"},
	{"35.192.0.0/14", "google"},
}

// DefaultASNDatabase returns a StaticASNDatabase populated with DefaultCDNCIDRs.
func DefaultASNDatabase() *StaticASNDatabase {
	return NewStaticASNDatabase(DefaultCDNCIDRs)
}
