import { http } from '@/utils/http'
import type {
  DomainResponse,
  RegisterDomainRequest,
  DomainTransitionRequest,
  DomainLifecycleHistoryEntry,
} from '@/types/domain'
import type { PaginatedData } from '@/types/common'

export const domainApi = {
  list: (params: { project_id?: number; lifecycle_state?: string; cursor?: string; limit?: number }) =>
    http.get<PaginatedData<DomainResponse>>('/domains', { params }),

  get: (id: string) =>
    http.get<DomainResponse>(`/domains/${id}`),

  register: (data: RegisterDomainRequest) =>
    http.post<DomainResponse>('/domains', data),

  transition: (id: string, data: DomainTransitionRequest) =>
    http.post<DomainResponse>(`/domains/${id}/transition`, data),

  delete: (id: string) =>
    http.delete(`/domains/${id}`),

  history: (id: string) =>
    http.get<DomainLifecycleHistoryEntry[]>(`/domains/${id}/history`),
}
