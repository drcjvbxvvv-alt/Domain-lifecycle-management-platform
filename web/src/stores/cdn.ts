import { defineStore } from 'pinia'
import { ref } from 'vue'
import { cdnApi } from '@/api/cdn'
import type {
  CDNProviderResponse,
  CDNAccountResponse,
  CreateCDNProviderRequest,
  UpdateCDNProviderRequest,
  CreateCDNAccountRequest,
  UpdateCDNAccountRequest,
} from '@/api/cdn'

type ApiResponse<T> = { data: T; code: number; message: string }
type ListResponse<T> = { items: T[]; total: number }

export const useCDNStore = defineStore('cdn', () => {
  const providers  = ref<CDNProviderResponse[]>([])
  const total      = ref(0)
  const loading    = ref(false)
  const current    = ref<CDNProviderResponse | null>(null)
  const accounts   = ref<CDNAccountResponse[]>([])
  const allAccounts = ref<CDNAccountResponse[]>([])

  // ── Providers ────────────────────────────────────────────────────────────────

  async function fetchList() {
    loading.value = true
    try {
      const res = await cdnApi.listProviders() as unknown as ApiResponse<ListResponse<CDNProviderResponse>>
      providers.value = res.data?.items ?? []
      total.value = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number) {
    loading.value = true
    try {
      const res = await cdnApi.getProvider(id) as unknown as ApiResponse<CDNProviderResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function create(data: CreateCDNProviderRequest) {
    const res = await cdnApi.createProvider(data) as unknown as ApiResponse<CDNProviderResponse>
    await fetchList()
    return res.data
  }

  async function update(id: number, data: UpdateCDNProviderRequest) {
    const res = await cdnApi.updateProvider(id, data) as unknown as ApiResponse<CDNProviderResponse>
    current.value = res.data
    await fetchList()
    return res.data
  }

  async function remove(id: number) {
    await cdnApi.deleteProvider(id)
    await fetchList()
  }

  // ── Accounts ─────────────────────────────────────────────────────────────────

  async function fetchAccounts(providerId: number) {
    loading.value = true
    try {
      const res = await cdnApi.listAccounts(providerId) as unknown as ApiResponse<ListResponse<CDNAccountResponse>>
      accounts.value = res.data?.items ?? []
    } finally {
      loading.value = false
    }
  }

  async function fetchAllAccounts() {
    const res = await cdnApi.listAllAccounts() as unknown as ApiResponse<ListResponse<CDNAccountResponse>>
    allAccounts.value = res.data?.items ?? []
  }

  async function createAccount(providerId: number, data: CreateCDNAccountRequest) {
    const res = await cdnApi.createAccount(providerId, data) as unknown as ApiResponse<CDNAccountResponse>
    await fetchAccounts(providerId)
    return res.data
  }

  async function updateAccount(id: number, providerId: number, data: UpdateCDNAccountRequest) {
    const res = await cdnApi.updateAccount(id, data) as unknown as ApiResponse<CDNAccountResponse>
    await fetchAccounts(providerId)
    return res.data
  }

  async function removeAccount(id: number, providerId: number) {
    await cdnApi.deleteAccount(id)
    await fetchAccounts(providerId)
  }

  return {
    providers,
    total,
    loading,
    current,
    accounts,
    allAccounts,
    fetchList,
    fetchOne,
    create,
    update,
    remove,
    fetchAccounts,
    fetchAllAccounts,
    createAccount,
    updateAccount,
    removeAccount,
  }
})
