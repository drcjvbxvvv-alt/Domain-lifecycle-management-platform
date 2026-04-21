// Types for live DNS record lookup — mirrors Go's dnsquery.LookupResult.
// Powered by miekg/dns for full protocol-level access.

export type DNSRecordType =
  | 'A' | 'AAAA' | 'CNAME' | 'MX' | 'TXT'
  | 'NS' | 'SOA' | 'SRV' | 'CAA' | 'PTR'

export interface DNSRecord {
  type: DNSRecordType
  name: string
  value: string
  ttl: number
  priority?: number  // MX / SRV
}

export interface DNSLookupResult {
  fqdn: string
  nameserver: string
  records: DNSRecord[]
  queried_at: string
  elapsed_ms: number
  error?: string
}
