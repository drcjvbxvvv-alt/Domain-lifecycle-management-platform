import { defineStore } from 'pinia'
import { ref } from 'vue'
import { domainApi } from '@/api/domain'
import type { DomainResponse } from '@/types/domain'

export const useDomainStore = defineStore('domain', () => {
  const domains  = ref<DomainResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<DomainResponse | null>(null)

  async function fetchList(params?: { project_id?: number; lifecycle_state?: string; limit?: number; offset?: number }) {
    loading.value = true
    try {
      const res = await domainApi.list(params ?? {}) as any
      domains.value = res.data?.items ?? res.items ?? []
      total.value   = res.data?.total ?? res.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      const res = await domainApi.get(id) as any
      current.value = res.data ?? res
    } finally {
      loading.value = false
    }
  }

  return { domains, total, loading, current, fetchList, fetchOne }
})
