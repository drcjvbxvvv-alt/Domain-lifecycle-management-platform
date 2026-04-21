import { defineStore } from 'pinia'
import { ref } from 'vue'
import { dnsProviderApi } from '@/api/dnsprovider'
import type {
  DNSProviderResponse,
  CreateDNSProviderRequest,
  UpdateDNSProviderRequest,
} from '@/types/dnsprovider'
import type { ApiResponse } from '@/types/common'

type ListResponse<T> = { items: T[]; total: number }

export const useDNSProviderStore = defineStore('dnsProvider', () => {
  const providers = ref<DNSProviderResponse[]>([])
  const total = ref(0)
  const loading = ref(false)
  const current = ref<DNSProviderResponse | null>(null)
  const supportedTypes = ref<string[]>([])

  async function fetchTypes() {
    const res = await dnsProviderApi.types() as unknown as ApiResponse<string[]>
    supportedTypes.value = res.data ?? []
  }

  async function fetchList() {
    loading.value = true
    try {
      const res = await dnsProviderApi.list() as unknown as ApiResponse<ListResponse<DNSProviderResponse>>
      providers.value = res.data?.items ?? []
      total.value = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number) {
    loading.value = true
    try {
      const res = await dnsProviderApi.get(id) as unknown as ApiResponse<DNSProviderResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function create(data: CreateDNSProviderRequest) {
    const res = await dnsProviderApi.create(data) as unknown as ApiResponse<DNSProviderResponse>
    await fetchList()
    return res.data
  }

  async function update(id: number, data: UpdateDNSProviderRequest) {
    const res = await dnsProviderApi.update(id, data) as unknown as ApiResponse<DNSProviderResponse>
    current.value = res.data
    await fetchList()
    return res.data
  }

  async function remove(id: number) {
    await dnsProviderApi.delete(id)
    await fetchList()
  }

  return {
    providers,
    total,
    loading,
    current,
    supportedTypes,
    fetchTypes,
    fetchList,
    fetchOne,
    create,
    update,
    remove,
  }
})
