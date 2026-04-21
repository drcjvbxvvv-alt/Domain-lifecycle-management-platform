import { http } from '@/utils/http'
import type {
  DNSProviderResponse,
  CreateDNSProviderRequest,
  UpdateDNSProviderRequest,
} from '@/types/dnsprovider'

type ListResponse<T> = { items: T[]; total: number }

export const dnsProviderApi = {
  types: () =>
    http.get<string[]>('/dns-providers/types'),

  list: () =>
    http.get<ListResponse<DNSProviderResponse>>('/dns-providers'),

  get: (id: number) =>
    http.get<DNSProviderResponse>(`/dns-providers/${id}`),

  create: (data: CreateDNSProviderRequest) =>
    http.post<DNSProviderResponse>('/dns-providers', data),

  update: (id: number, data: UpdateDNSProviderRequest) =>
    http.put<DNSProviderResponse>(`/dns-providers/${id}`, data),

  delete: (id: number) =>
    http.delete(`/dns-providers/${id}`),
}
