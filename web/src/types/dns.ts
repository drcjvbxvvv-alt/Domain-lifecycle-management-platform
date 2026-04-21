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

// ── Propagation check types ──────────────────────────────────────────────────

export interface ResolverResult {
  address: string
  label: string
  records: DNSRecord[]
  authoritative: boolean
  elapsed_ms: number
  error?: string
}

export interface PropagationResult {
  fqdn: string
  query_types: string[]
  resolvers: ResolverResult[]
  consistent: boolean
  queried_at: string
  total_ms: number
}

// ── Drift detection types ────────────────────────────────────────────────────

export type DriftStatus = 'ok' | 'drift' | 'no_expected' | 'error'

export interface DriftRecord {
  type: string
  name: string
  expected?: string
  actual?: string
  match: boolean
}

// ── Provider record types (for CRUD) ─────────────────────────────────────────

export interface ProviderRecord {
  id: string
  type: string
  name: string
  content: string
  ttl: number
  priority?: number
  proxied?: boolean
}

export interface CreateProviderRecordRequest {
  type: string
  name: string
  content: string
  ttl: number
  priority?: number
  proxied?: boolean
}

export interface UpdateProviderRecordRequest {
  type: string
  name: string
  content: string
  ttl: number
  priority?: number
  proxied?: boolean
}

export interface DriftResult {
  fqdn: string
  provider_name: string
  provider_label: string
  status: DriftStatus
  records: DriftRecord[]
  expected_count: number
  actual_count: number
  match_count: number
  drift_count: number
  missing_count: number
  extra_count: number
  queried_at: string
  elapsed_ms: number
  error?: string
}
