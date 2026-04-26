import axios from 'axios'
import type { DNSBindingStatus } from '@/types/domain'

const BASE = '/api/v1/domains'

export const dnsBindingApi = {
  /** GET /api/v1/domains/:id/dns-binding */
  getStatus(domainId: number): Promise<{ data: { data: DNSBindingStatus } }> {
    return axios.get(`${BASE}/${domainId}/dns-binding`)
  },

  /** PUT /api/v1/domains/:id/dns-binding — pass null to unbind */
  bind(domainId: number, dnsProviderId: number | null): Promise<{ data: { data: DNSBindingStatus } }> {
    return axios.put(`${BASE}/${domainId}/dns-binding`, { dns_provider_id: dnsProviderId })
  },

  /** POST /api/v1/domains/:id/dns-binding/verify — manually trigger NS check */
  triggerVerify(domainId: number): Promise<void> {
    return axios.post(`${BASE}/${domainId}/dns-binding/verify`)
  },
}
