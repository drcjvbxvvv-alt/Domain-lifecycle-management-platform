import { http } from '@/utils/http'

export interface BlockedDomainRow {
  id: number
  fqdn: string
  project_id: number
  blocking_status: string
  blocking_type: string | null
  blocking_since: string | null
  blocking_confidence: number
}

export interface GFWStats {
  total_monitored: number
  total_blocked: number
  total_possibly_blocked: number
  blocked_dns: number
  blocked_tcp_ip: number
  blocked_tls_sni: number
  blocked_http: number
}

export interface GFWVerdict {
  id: number
  domain_id: number
  blocking: string
  accessible: boolean
  confidence: number
  probe_node_id: string
  control_node_id: string | null
  dns_consistency: string | null
  detail: Record<string, unknown>
  measured_at: string
  created_at: string
}

export interface DomainBlockingState {
  blocking_status: string | null
  blocking_type: string | null
  blocking_since: string | null
  blocking_confidence: number | null
}

export interface DomainBlockingTimeline {
  blocking_state: DomainBlockingState | null
  verdicts: GFWVerdict[]
  total: number
}

export const gfwApi = {
  // Dashboard summary
  getStats: () =>
    http.get<GFWStats>('/gfw/stats'),

  listBlockedDomains: () =>
    http.get<{ items: BlockedDomainRow[]; total: number }>('/gfw/blocked-domains'),

  // Verdict history + current state for a domain
  getDomainTimeline: (domainId: number, limit = 100) =>
    http.get<DomainBlockingTimeline>(`/gfw/timeline/${domainId}`, { params: { limit } }),

  listVerdicts: (domainId: number, limit = 100) =>
    http.get<{ items: GFWVerdict[]; total: number }>(`/gfw/verdicts/${domainId}`, { params: { limit } }),

  latestVerdict: (domainId: number) =>
    http.get<GFWVerdict>(`/gfw/verdicts/${domainId}/latest`),
}
