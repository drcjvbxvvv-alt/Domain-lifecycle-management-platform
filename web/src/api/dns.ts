import { http } from '@/utils/http'
import type { ApiResponse } from '@/types/common'
import type { DNSLookupResult, PropagationResult, DriftResult, ProviderRecord, CreateProviderRecordRequest, UpdateProviderRecordRequest } from '@/types/dns'

export const dnsApi = {
  /** Look up DNS records for a domain in the database by its ID. */
  lookupByDomain(domainId: number): Promise<ApiResponse<DNSLookupResult>> {
    return http.get(`/domains/${domainId}/dns-records`)
  },

  /** Look up DNS records for any arbitrary FQDN. */
  lookupByFQDN(fqdn: string): Promise<ApiResponse<DNSLookupResult>> {
    return http.get('/dns/lookup', { params: { fqdn } })
  },

  /** Check DNS propagation for a domain across multiple resolvers. */
  propagationByDomain(domainId: number, types?: string): Promise<ApiResponse<PropagationResult>> {
    return http.get(`/domains/${domainId}/dns-propagation`, { params: types ? { types } : {} })
  },

  /** Check DNS propagation for any arbitrary FQDN. */
  propagationByFQDN(fqdn: string, types?: string): Promise<ApiResponse<PropagationResult>> {
    return http.get('/dns/propagation', { params: { fqdn, ...(types ? { types } : {}) } })
  },

  /** Check drift between DNS provider (expected) and live DNS (actual). */
  driftCheck(domainId: number): Promise<ApiResponse<DriftResult>> {
    return http.get(`/domains/${domainId}/dns-drift`)
  },

  // ── Provider record CRUD ──────────────────────────────────────────────────

  /** List DNS records from the provider API. */
  listProviderRecords(domainId: number, type?: string): Promise<ApiResponse<{ items: ProviderRecord[]; total: number }>> {
    return http.get(`/domains/${domainId}/provider-records`, { params: type ? { type } : {} })
  },

  /** Create a DNS record via the provider API. */
  createProviderRecord(domainId: number, data: CreateProviderRecordRequest): Promise<ApiResponse<ProviderRecord>> {
    return http.post(`/domains/${domainId}/provider-records`, data)
  },

  /** Update a DNS record via the provider API. */
  updateProviderRecord(domainId: number, recordId: string, data: UpdateProviderRecordRequest): Promise<ApiResponse<ProviderRecord>> {
    return http.put(`/domains/${domainId}/provider-records/${recordId}`, data)
  },

  /** Delete a DNS record via the provider API. */
  deleteProviderRecord(domainId: number, recordId: string): Promise<ApiResponse<null>> {
    return http.delete(`/domains/${domainId}/provider-records/${recordId}`)
  },
}
