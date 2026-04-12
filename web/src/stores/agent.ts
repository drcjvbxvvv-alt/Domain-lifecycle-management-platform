import { defineStore } from 'pinia'
import { ref } from 'vue'
import { agentApi } from '@/api/agent'
import type { AgentResponse, AgentStateHistoryEntry } from '@/types/agent'

export const useAgentStore = defineStore('agent', () => {
  const agents   = ref<AgentResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<AgentResponse | null>(null)
  const history  = ref<AgentStateHistoryEntry[]>([])

  async function fetchList(params?: { limit?: number; offset?: number }) {
    loading.value = true
    try {
      const res = await agentApi.list(params) as any
      agents.value = res.data?.items ?? res.items ?? []
      total.value  = res.data?.total ?? res.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number | string) {
    loading.value = true
    try {
      const res = await agentApi.get(id) as any
      current.value = res.data ?? res
    } finally {
      loading.value = false
    }
  }

  async function fetchHistory(id: number | string) {
    const res = await agentApi.history(id) as any
    history.value = res.data?.items ?? res.items ?? []
  }

  return { agents, total, loading, current, history, fetchList, fetchOne, fetchHistory }
})
