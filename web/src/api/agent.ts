import { http } from '@/utils/http'
import type { AgentResponse, AgentStateHistoryEntry } from '@/types/agent'
import type { PaginatedData } from '@/types/common'

export const agentApi = {
  list: (params?: { limit?: number; offset?: number }) =>
    http.get<PaginatedData<AgentResponse>>('/agents', { params }),

  get: (id: number | string) =>
    http.get<AgentResponse>(`/agents/${id}`),

  history: (id: number | string, limit = 50) =>
    http.get<{ items: AgentStateHistoryEntry[] }>(`/agents/${id}/history`, { params: { limit } }),
}
