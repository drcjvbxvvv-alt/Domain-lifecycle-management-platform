import { http } from '@/utils/http'
import type { ReleaseResponse, ReleaseShardResponse, ReleaseStateHistoryEntry } from '@/types/release'
import type { PaginatedData } from '@/types/common'

export const releaseApi = {
  list: (params: { project_id?: number; cursor?: string }) =>
    http.get<PaginatedData<ReleaseResponse>>('/releases', { params }),

  get: (id: string) =>
    http.get<ReleaseResponse>(`/releases/${id}`),

  create: (data: { project_id: number; title?: string; shard_size?: number }) =>
    http.post<ReleaseResponse>('/releases', data),

  start: (id: string) =>
    http.post(`/releases/${id}/start`),

  pause: (id: string) =>
    http.post(`/releases/${id}/pause`),

  resume: (id: string) =>
    http.post(`/releases/${id}/resume`),

  rollback: (id: string) =>
    http.post(`/releases/${id}/rollback`),

  shards: (id: string) =>
    http.get<ReleaseShardResponse[]>(`/releases/${id}/shards`),

  history: (id: string) =>
    http.get<{ items: ReleaseStateHistoryEntry[] }>(`/releases/${id}/history`),

  cancel: (id: string) =>
    http.post(`/releases/${id}/cancel`),
}
