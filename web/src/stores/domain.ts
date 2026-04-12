import { defineStore } from 'pinia'
import { ref } from 'vue'
import { domainApi } from '@/api/domain'
import type { DomainResponse, RegisterDomainRequest, DomainTransitionRequest } from '@/types/domain'
import type { ApiResponse, PaginatedData } from '@/types/common'

export const useDomainStore = defineStore('domain', () => {
  const domains  = ref<DomainResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<DomainResponse | null>(null)

  async function fetchList(params?: { project_id?: number; lifecycle_state?: string; limit?: number; offset?: number }) {
    loading.value = true
    try {
      const res = await domainApi.list(params ?? {}) as unknown as ApiResponse<PaginatedData<DomainResponse>>
      domains.value = res.data?.items ?? []
      total.value   = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      const res = await domainApi.get(id) as unknown as ApiResponse<DomainResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function register(data: RegisterDomainRequest) {
    const res = await domainApi.register(data) as unknown as ApiResponse<DomainResponse>
    await fetchList()
    return res.data
  }

  async function transition(id: string, data: DomainTransitionRequest) {
    const res = await domainApi.transition(id, data) as unknown as ApiResponse<DomainResponse>
    current.value = res.data
    return res.data
  }

  return { domains, total, loading, current, fetchList, fetchOne, register, transition }
})
