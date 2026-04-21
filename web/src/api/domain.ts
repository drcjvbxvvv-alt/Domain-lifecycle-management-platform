import { http } from '@/utils/http'
import type {
  DomainResponse,
  RegisterDomainRequest,
  UpdateDomainAssetRequest,
  DomainTransitionRequest,
  DomainLifecycleHistoryEntry,
  DomainStats,
  InitiateTransferRequest,
} from '@/types/domain'
import type { PaginatedData } from '@/types/common'

export type DomainListParams = {
  project_id?:      number
  registrar_id?:    number
  dns_provider_id?: number
  tld?:             string
  expiry_status?:   string
  lifecycle_state?: string
  cursor?:          number
  limit?:           number
}

type ExpiringResponse = { items: DomainResponse[]; total: number; days: number }

export const domainApi = {
  list: (params: DomainListParams) =>
    http.get<PaginatedData<DomainResponse>>('/domains', { params }),

  get: (id: number | string) =>
    http.get<DomainResponse>(`/domains/${id}`),

  register: (data: RegisterDomainRequest) =>
    http.post<DomainResponse>('/domains', data),

  updateAsset: (id: number, data: UpdateDomainAssetRequest) =>
    http.put<DomainResponse>(`/domains/${id}`, data),

  transition: (id: number | string, data: DomainTransitionRequest) =>
    http.post<DomainResponse>(`/domains/${id}/transition`, data),

  history: (id: number | string) =>
    http.get<DomainLifecycleHistoryEntry[]>(`/domains/${id}/history`),

  expiring: (days = 30) =>
    http.get<ExpiringResponse>('/domains/expiring', { params: { days } }),

  stats: (projectId?: number) =>
    http.get<DomainStats>('/domains/stats', { params: projectId ? { project_id: projectId } : {} }),

  // Transfer
  initiateTransfer: (id: number, data: InitiateTransferRequest) =>
    http.post<DomainResponse>(`/domains/${id}/transfer`, data),

  completeTransfer: (id: number) =>
    http.post<DomainResponse>(`/domains/${id}/transfer/complete`, {}),

  cancelTransfer: (id: number) =>
    http.post<DomainResponse>(`/domains/${id}/transfer/cancel`, {}),
}
