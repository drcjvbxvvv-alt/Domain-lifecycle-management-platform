import { http } from '@/utils/http'
import type { SSLCertResponse, CreateSSLCertRequest, SSLCheckRequest } from '@/types/ssl'
import type { PaginatedData } from '@/types/common'

type ExpiringResponse = { items: SSLCertResponse[]; total: number; days: number }

export const sslApi = {
  list: (domainId: number) =>
    http.get<PaginatedData<SSLCertResponse>>(`/domains/${domainId}/ssl-certs`),

  create: (domainId: number, data: CreateSSLCertRequest) =>
    http.post<SSLCertResponse>(`/domains/${domainId}/ssl-certs`, data),

  check: (domainId: number, data: SSLCheckRequest) =>
    http.post<SSLCertResponse>(`/domains/${domainId}/ssl-certs/check`, data),

  expiring: (days = 30) =>
    http.get<ExpiringResponse>('/ssl-certs/expiring', { params: { days } }),

  delete: (id: number) =>
    http.delete(`/ssl-certs/${id}`),
}
