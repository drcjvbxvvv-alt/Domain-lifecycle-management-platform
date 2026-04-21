// Package dnsquery provides live DNS record lookups using miekg/dns.
//
// It queries DNS resolvers at the raw protocol level, returning full record
// details including TTL. Supports A, AAAA, CNAME, MX, TXT, NS, SOA, SRV,
// CAA, and PTR record types. The caller can specify a custom nameserver
// (e.g. "8.8.8.8:53") or leave it empty to use system default.
//
// This is a credential-free, read-only lookup — it does not use any DNS
// Provider API. Suitable for answering "what does this domain actually
// resolve to right now?".
package dnsquery

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"
)

// RecordType enumerates supported DNS record types.
type RecordType string

const (
	TypeA     RecordType = "A"
	TypeAAAA  RecordType = "AAAA"
	TypeCNAME RecordType = "CNAME"
	TypeMX    RecordType = "MX"
	TypeTXT   RecordType = "TXT"
	TypeNS    RecordType = "NS"
	TypeSOA   RecordType = "SOA"
	TypeSRV   RecordType = "SRV"
	TypeCAA   RecordType = "CAA"
	TypePTR   RecordType = "PTR"
)

// typeOrder defines the display order for record types.
var typeOrder = map[RecordType]int{
	TypeA: 0, TypeAAAA: 1, TypeCNAME: 2, TypeMX: 3,
	TypeNS: 4, TypeSOA: 5, TypeSRV: 6, TypeCAA: 7,
	TypeTXT: 8, TypePTR: 9,
}

// Record represents a single DNS record.
type Record struct {
	Type     RecordType `json:"type"`
	Name     string     `json:"name"`
	Value    string     `json:"value"`
	TTL      uint32     `json:"ttl"`
	Priority int        `json:"priority,omitempty"` // MX / SRV
}

// LookupResult is the full DNS lookup response for one FQDN.
type LookupResult struct {
	FQDN       string   `json:"fqdn"`
	Nameserver string   `json:"nameserver"`          // resolver used
	Records    []Record `json:"records"`
	QueriedAt  string   `json:"queried_at"`           // ISO 8601
	ElapsedMs  int64    `json:"elapsed_ms"`           // total query time
	Error      string   `json:"error,omitempty"`
}

// defaultQueryTypes lists the record types queried by Lookup().
var defaultQueryTypes = []uint16{
	dns.TypeA,
	dns.TypeAAAA,
	dns.TypeCNAME,
	dns.TypeMX,
	dns.TypeNS,
	dns.TypeSOA,
	dns.TypeSRV,
	dns.TypeCAA,
	dns.TypeTXT,
}

// Service performs DNS lookups via miekg/dns.
type Service struct {
	nameserver string // e.g. "8.8.8.8:53"; empty = system default
	logger     *zap.Logger
}

// NewService creates a DNS query service.
// nameserver is the DNS resolver address (e.g. "8.8.8.8:53").
// Pass "" to auto-detect the system resolver.
func NewService(nameserver string, logger *zap.Logger) *Service {
	if nameserver == "" {
		nameserver = detectSystemResolver()
	}
	// Ensure port is present
	if _, _, err := net.SplitHostPort(nameserver); err != nil {
		nameserver = net.JoinHostPort(nameserver, "53")
	}
	return &Service{
		nameserver: nameserver,
		logger:     logger,
	}
}

// Nameserver returns the resolver address this service uses.
func (s *Service) Nameserver() string {
	return s.nameserver
}

// Lookup queries all supported record types for the given FQDN.
// Individual query failures are logged at debug level but do not
// cause the overall lookup to fail.
func (s *Service) Lookup(ctx context.Context, fqdn string) *LookupResult {
	fqdn = strings.TrimSuffix(strings.TrimSpace(fqdn), ".")
	if fqdn == "" {
		return &LookupResult{FQDN: fqdn, Error: "empty FQDN", QueriedAt: now()}
	}

	start := time.Now()
	result := &LookupResult{
		FQDN:       fqdn,
		Nameserver: s.nameserver,
		QueriedAt:  now(),
	}

	// FQDN must end with a dot for miekg/dns
	qname := dns.Fqdn(fqdn)

	c := new(dns.Client)
	c.Timeout = 5 * time.Second

	for _, qtype := range defaultQueryTypes {
		if ctx.Err() != nil {
			break
		}

		msg := new(dns.Msg)
		msg.SetQuestion(qname, qtype)
		msg.RecursionDesired = true

		resp, _, err := c.ExchangeContext(ctx, msg, s.nameserver)
		if err != nil {
			s.logQueryErr(fqdn, dns.TypeToString[qtype], err)
			continue
		}
		if resp == nil {
			continue
		}
		// Retry with TCP if response was truncated (common for TXT records)
		if resp.Truncated {
			tcpClient := new(dns.Client)
			tcpClient.Net = "tcp"
			tcpClient.Timeout = 5 * time.Second
			tcpResp, _, tcpErr := tcpClient.ExchangeContext(ctx, msg, s.nameserver)
			if tcpErr == nil && tcpResp != nil {
				resp = tcpResp
			}
		}
		if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
			s.logger.Debug("dns non-success rcode",
				zap.String("fqdn", fqdn),
				zap.String("type", dns.TypeToString[qtype]),
				zap.String("rcode", dns.RcodeToString[resp.Rcode]),
			)
			continue
		}

		for _, rr := range resp.Answer {
			rec := parseRR(rr)
			if rec != nil {
				result.Records = append(result.Records, *rec)
			}
		}
	}

	// Deduplicate (multiple query types can return the same CNAME chain)
	result.Records = dedup(result.Records)

	// Sort by type order, then value
	sort.Slice(result.Records, func(i, j int) bool {
		oi, oj := typeOrder[result.Records[i].Type], typeOrder[result.Records[j].Type]
		if oi != oj {
			return oi < oj
		}
		return result.Records[i].Value < result.Records[j].Value
	})

	result.ElapsedMs = time.Since(start).Milliseconds()
	return result
}

// parseRR converts a miekg/dns RR into our Record type.
// Returns nil for unsupported RR types.
func parseRR(rr dns.RR) *Record {
	hdr := rr.Header()
	name := strings.TrimSuffix(hdr.Name, ".")

	switch v := rr.(type) {
	case *dns.A:
		return &Record{Type: TypeA, Name: name, Value: v.A.String(), TTL: hdr.Ttl}
	case *dns.AAAA:
		return &Record{Type: TypeAAAA, Name: name, Value: v.AAAA.String(), TTL: hdr.Ttl}
	case *dns.CNAME:
		return &Record{Type: TypeCNAME, Name: name, Value: strings.TrimSuffix(v.Target, "."), TTL: hdr.Ttl}
	case *dns.MX:
		return &Record{Type: TypeMX, Name: name, Value: strings.TrimSuffix(v.Mx, "."), TTL: hdr.Ttl, Priority: int(v.Preference)}
	case *dns.NS:
		return &Record{Type: TypeNS, Name: name, Value: strings.TrimSuffix(v.Ns, "."), TTL: hdr.Ttl}
	case *dns.SOA:
		val := fmt.Sprintf("%s %s %d %d %d %d %d",
			strings.TrimSuffix(v.Ns, "."),
			strings.TrimSuffix(v.Mbox, "."),
			v.Serial, v.Refresh, v.Retry, v.Expire, v.Minttl,
		)
		return &Record{Type: TypeSOA, Name: name, Value: val, TTL: hdr.Ttl}
	case *dns.SRV:
		val := fmt.Sprintf("%s:%d (weight=%d)", strings.TrimSuffix(v.Target, "."), v.Port, v.Weight)
		return &Record{Type: TypeSRV, Name: name, Value: val, TTL: hdr.Ttl, Priority: int(v.Priority)}
	case *dns.CAA:
		val := fmt.Sprintf("%d %s \"%s\"", v.Flag, v.Tag, v.Value)
		return &Record{Type: TypeCAA, Name: name, Value: val, TTL: hdr.Ttl}
	case *dns.TXT:
		return &Record{Type: TypeTXT, Name: name, Value: strings.Join(v.Txt, ""), TTL: hdr.Ttl}
	case *dns.PTR:
		return &Record{Type: TypePTR, Name: name, Value: strings.TrimSuffix(v.Ptr, "."), TTL: hdr.Ttl}
	default:
		return nil
	}
}

// dedup removes duplicate records (same type + value).
func dedup(records []Record) []Record {
	seen := make(map[string]struct{}, len(records))
	out := make([]Record, 0, len(records))
	for _, r := range records {
		key := string(r.Type) + "|" + r.Value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

func (s *Service) logQueryErr(fqdn string, qtype string, err error) {
	// Timeouts and "no such host" are normal for certain types — debug only
	if isExpectedDNSErr(err) {
		s.logger.Debug("dns query not resolved",
			zap.String("fqdn", fqdn),
			zap.String("type", qtype),
		)
		return
	}
	s.logger.Warn("dns lookup error",
		zap.String("fqdn", fqdn),
		zap.String("type", qtype),
		zap.Error(err),
	)
}

func isExpectedDNSErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "NXDOMAIN")
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// detectSystemResolver tries to find the system's DNS resolver.
// Falls back to 8.8.8.8:53 if detection fails.
func detectSystemResolver() string {
	config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err == nil && len(config.Servers) > 0 {
		return net.JoinHostPort(config.Servers[0], config.Port)
	}
	return "8.8.8.8:53"
}

// LookupMultiple queries DNS for multiple FQDNs concurrently (up to 20).
func (s *Service) LookupMultiple(ctx context.Context, fqdns []string) []LookupResult {
	const maxConcurrency = 20
	if len(fqdns) > maxConcurrency {
		fqdns = fqdns[:maxConcurrency]
	}

	results := make([]LookupResult, len(fqdns))
	done := make(chan struct{}, len(fqdns))

	for i, fqdn := range fqdns {
		go func(idx int, name string) {
			r := s.Lookup(ctx, name)
			results[idx] = *r
			done <- struct{}{}
		}(i, fqdn)
	}

	for range fqdns {
		<-done
	}

	return results
}
