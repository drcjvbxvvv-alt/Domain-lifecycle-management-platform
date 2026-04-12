import { http } from '@/utils/http'
import type { ReleaseResponse, ReleaseShardResponse, ReleaseStateHistoryEntry, DryRunResult } from '@/types/release'
import type { PaginatedData } from '@/types/common'

export const releaseApi = {
  list: (params: { project_id?: number; cursor?: string }) =>
    http.get<PaginatedData<ReleaseResponse>>('/releases', { params }),

  get: (id: string) =>
    http.get<ReleaseResponse>(`/releases/${id}`),

  create: (data: {
    project_id: number
    project_slug: string
    template_version_id: number
    release_type?: string
    description?: string
    domain_ids?: number[]
  }) =>
    http.post<ReleaseResponse>('/releases', data),

  start: (id: string) =>
    http.post(`/releases/${id}/start`),

  pause: (id: string, reason?: string) =>
    http.post(`/releases/${id}/pause`, { reason }),

  resume: (id: string) =>
    http.post(`/releases/${id}/resume`),

  rollback: (id: string, reason?: string) =>
    http.post(`/releases/${id}/rollback`, { reason }),

  dryRun: (id: string) =>
    http.get<DryRunResult>(`/releases/${id}/dry-run`),

  shards: (id: string) =>
    http.get<ReleaseShardResponse[]>(`/releases/${id}/shards`),

  history: (id: string) =>
    http.get<{ items: ReleaseStateHistoryEntry[] }>(`/releases/${id}/history`),

  cancel: (id: string, reason?: string) =>
    http.post(`/releases/${id}/cancel`, { reason }),
}
