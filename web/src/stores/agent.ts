import { defineStore } from 'pinia'
import { ref } from 'vue'
import { agentApi } from '@/api/agent'
import type { AgentResponse, AgentStateHistoryEntry } from '@/types/agent'
import type { ApiResponse, PaginatedData } from '@/types/common'

export const useAgentStore = defineStore('agent', () => {
  const agents   = ref<AgentResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<AgentResponse | null>(null)
  const history  = ref<AgentStateHistoryEntry[]>([])

  async function fetchList(params?: { limit?: number; offset?: number }) {
    loading.value = true
    try {
      const res = await agentApi.list(params) as unknown as ApiResponse<PaginatedData<AgentResponse>>
      agents.value = res.data?.items ?? []
      total.value  = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number | string) {
    loading.value = true
    try {
      const res = await agentApi.get(id) as unknown as ApiResponse<AgentResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function fetchHistory(id: number | string) {
    const res = await agentApi.history(id) as unknown as ApiResponse<{ items: AgentStateHistoryEntry[] }>
    history.value = res.data?.items ?? []
  }

  function currentStatus(id: number | string): string {
    return (current.value?.id === Number(id) ? current.value?.status : null)
      ?? agents.value.find(a => a.id === Number(id))?.status
      ?? 'online'
  }

  async function transition(id: number | string, to: string, reason?: string) {
    const from = currentStatus(id)
    await agentApi.transition(id, { from, to, reason })
    // Re-fetch to update both list and detail
    const res = await agentApi.get(id) as unknown as ApiResponse<AgentResponse>
    if (res.data) {
      if (current.value?.id === Number(id)) current.value = res.data
      const idx = agents.value.findIndex(a => a.id === Number(id))
      if (idx !== -1) agents.value[idx] = res.data
    }
  }

  const drain   = (id: number | string) => transition(id, 'draining', 'operator drain')
  const disable = (id: number | string) => transition(id, 'disabled',  'operator disable')
  const enable  = (id: number | string) => transition(id, 'online',   'operator enable')

  return { agents, total, loading, current, history, fetchList, fetchOne, fetchHistory, transition, drain, disable, enable }
})
