import { defineStore } from 'pinia'
import { ref } from 'vue'
import { registrarApi } from '@/api/registrar'
import type {
  RegistrarResponse,
  RegistrarAccountResponse,
  CreateRegistrarRequest,
  UpdateRegistrarRequest,
  CreateRegistrarAccountRequest,
  UpdateRegistrarAccountRequest,
} from '@/types/registrar'
import type { ApiResponse } from '@/types/common'

type ListResponse<T> = { items: T[]; total: number }

export const useRegistrarStore = defineStore('registrar', () => {
  const registrars = ref<RegistrarResponse[]>([])
  const total = ref(0)
  const loading = ref(false)
  const current = ref<RegistrarResponse | null>(null)
  const accounts = ref<RegistrarAccountResponse[]>([])

  async function fetchList() {
    loading.value = true
    try {
      const res = await registrarApi.list() as unknown as ApiResponse<ListResponse<RegistrarResponse>>
      registrars.value = res.data?.items ?? []
      total.value = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number) {
    loading.value = true
    try {
      const res = await registrarApi.get(id) as unknown as ApiResponse<RegistrarResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function create(data: CreateRegistrarRequest) {
    const res = await registrarApi.create(data) as unknown as ApiResponse<RegistrarResponse>
    await fetchList()
    return res.data
  }

  async function update(id: number, data: UpdateRegistrarRequest) {
    const res = await registrarApi.update(id, data) as unknown as ApiResponse<RegistrarResponse>
    current.value = res.data
    await fetchList()
    return res.data
  }

  async function remove(id: number) {
    await registrarApi.delete(id)
    await fetchList()
  }

  // ── Accounts ──────────────────────────────────────────────────────────────

  async function fetchAccounts(registrarId: number) {
    loading.value = true
    try {
      const res = await registrarApi.listAccounts(registrarId) as unknown as ApiResponse<ListResponse<RegistrarAccountResponse>>
      accounts.value = res.data?.items ?? []
    } finally {
      loading.value = false
    }
  }

  async function createAccount(registrarId: number, data: CreateRegistrarAccountRequest) {
    const res = await registrarApi.createAccount(registrarId, data) as unknown as ApiResponse<RegistrarAccountResponse>
    await fetchAccounts(registrarId)
    return res.data
  }

  async function updateAccount(id: number, registrarId: number, data: UpdateRegistrarAccountRequest) {
    const res = await registrarApi.updateAccount(id, data) as unknown as ApiResponse<RegistrarAccountResponse>
    await fetchAccounts(registrarId)
    return res.data
  }

  async function removeAccount(id: number, registrarId: number) {
    await registrarApi.deleteAccount(id)
    await fetchAccounts(registrarId)
  }

  return {
    registrars,
    total,
    loading,
    current,
    accounts,
    fetchList,
    fetchOne,
    create,
    update,
    remove,
    fetchAccounts,
    createAccount,
    updateAccount,
    removeAccount,
  }
})
