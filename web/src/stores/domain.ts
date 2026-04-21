import { defineStore } from 'pinia'
import { ref } from 'vue'
import { domainApi } from '@/api/domain'
import type {
  DomainResponse,
  RegisterDomainRequest,
  UpdateDomainAssetRequest,
  DomainTransitionRequest,
  DomainStats,
  InitiateTransferRequest,
} from '@/types/domain'
import type { ApiResponse, PaginatedData } from '@/types/common'
import type { DomainListParams } from '@/api/domain'

type ExpiringResponse = { items: DomainResponse[]; total: number; days: number }
type StatsResponse    = DomainStats

export const useDomainStore = defineStore('domain', () => {
  const domains  = ref<DomainResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<DomainResponse | null>(null)
  const stats    = ref<DomainStats | null>(null)
  const expiring = ref<DomainResponse[]>([])

  async function fetchList(params: DomainListParams = {}) {
    loading.value = true
    try {
      const res = await domainApi.list(params) as unknown as ApiResponse<PaginatedData<DomainResponse>>
      domains.value = res.data?.items ?? []
      total.value   = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number | string) {
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
    await fetchList(data.project_id ? { project_id: data.project_id } : {})
    return res.data
  }

  async function updateAsset(id: number, data: UpdateDomainAssetRequest) {
    const res = await domainApi.updateAsset(id, data) as unknown as ApiResponse<DomainResponse>
    current.value = res.data
    return res.data
  }

  async function transition(id: number | string, data: DomainTransitionRequest) {
    const res = await domainApi.transition(id, data) as unknown as ApiResponse<DomainResponse>
    current.value = res.data
    return res.data
  }

  async function fetchStats(projectId?: number) {
    const res = await domainApi.stats(projectId) as unknown as ApiResponse<StatsResponse>
    stats.value = res.data
    return res.data
  }

  async function fetchExpiring(days = 30) {
    const res = await domainApi.expiring(days) as unknown as ApiResponse<ExpiringResponse>
    expiring.value = res.data?.items ?? []
    return res.data
  }

  async function initiateTransfer(id: number, data: InitiateTransferRequest) {
    const res = await domainApi.initiateTransfer(id, data) as unknown as ApiResponse<DomainResponse>
    current.value = res.data
    return res.data
  }

  async function completeTransfer(id: number) {
    const res = await domainApi.completeTransfer(id) as unknown as ApiResponse<DomainResponse>
    current.value = res.data
    return res.data
  }

  async function cancelTransfer(id: number) {
    const res = await domainApi.cancelTransfer(id) as unknown as ApiResponse<DomainResponse>
    current.value = res.data
    return res.data
  }

  return {
    domains, total, loading, current, stats, expiring,
    fetchList, fetchOne, register, updateAsset, transition,
    fetchStats, fetchExpiring,
    initiateTransfer, completeTransfer, cancelTransfer,
  }
})
