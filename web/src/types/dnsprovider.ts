// web/src/types/dnsprovider.ts
// Mirror Go handler DTOs exactly. Keep in sync with api/handler/dnsprovider.go.

export type DNSProviderType =
  | 'cloudflare'
  | 'route53'
  | 'dnspod'
  | 'alidns'
  | 'godaddy'
  | 'namecheap'
  | 'manual'

export interface DNSProviderResponse {
  id: number
  uuid: string
  name: string
  provider_type: DNSProviderType
  config: Record<string, unknown>
  notes: string | null
  created_at: string
  updated_at: string
  // NOTE: credentials never returned by API (security)
}

export interface CreateDNSProviderRequest {
  name: string
  provider_type: DNSProviderType
  config?: Record<string, unknown>
  credentials?: Record<string, unknown>
  notes?: string | null
}

export interface UpdateDNSProviderRequest {
  name: string
  provider_type: DNSProviderType
  config?: Record<string, unknown>
  credentials?: Record<string, unknown>
  notes?: string | null
}
