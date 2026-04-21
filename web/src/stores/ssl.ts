import { defineStore } from 'pinia'
import { ref } from 'vue'
import { sslApi } from '@/api/ssl'
import type { SSLCertResponse, CreateSSLCertRequest } from '@/types/ssl'
import type { ApiResponse, PaginatedData } from '@/types/common'

type ExpiringResponse = { items: SSLCertResponse[]; total: number; days: number }

export const useSSLStore = defineStore('ssl', () => {
  const certs    = ref<SSLCertResponse[]>([])
  const expiring = ref<SSLCertResponse[]>([])
  const loading  = ref(false)

  async function fetchList(domainId: number) {
    loading.value = true
    try {
      const res = await sslApi.list(domainId) as unknown as ApiResponse<PaginatedData<SSLCertResponse>>
      certs.value = res.data?.items ?? []
    } finally {
      loading.value = false
    }
  }

  async function create(domainId: number, data: CreateSSLCertRequest) {
    const res = await sslApi.create(domainId, data) as unknown as ApiResponse<SSLCertResponse>
    return res.data
  }

  async function check(domainId: number, fqdn: string) {
    const res = await sslApi.check(domainId, { fqdn }) as unknown as ApiResponse<SSLCertResponse>
    // Refresh the list so the newly upserted cert shows
    const existing = certs.value.findIndex(c => c.serial_number === res.data?.serial_number)
    if (existing >= 0) {
      certs.value[existing] = res.data
    } else {
      certs.value = [res.data, ...certs.value]
    }
    return res.data
  }

  async function fetchExpiring(days = 30) {
    const res = await sslApi.expiring(days) as unknown as ApiResponse<ExpiringResponse>
    expiring.value = res.data?.items ?? []
    return res.data
  }

  async function deleteCert(id: number) {
    await sslApi.delete(id)
    certs.value = certs.value.filter(c => c.id !== id)
  }

  return { certs, expiring, loading, fetchList, create, check, fetchExpiring, deleteCert }
})
