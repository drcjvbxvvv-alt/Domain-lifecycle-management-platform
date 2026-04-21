import { http } from '@/utils/http'
import type {
  RegistrarResponse,
  RegistrarAccountResponse,
  CreateRegistrarRequest,
  UpdateRegistrarRequest,
  CreateRegistrarAccountRequest,
  UpdateRegistrarAccountRequest,
} from '@/types/registrar'
import type { PaginatedData } from '@/types/common'

type ListResponse<T> = { items: T[]; total: number }

export const registrarApi = {
  // ── Registrars ──────────────────────────────────────────────────────────
  list: () =>
    http.get<ListResponse<RegistrarResponse>>('/registrars'),

  get: (id: number) =>
    http.get<RegistrarResponse>(`/registrars/${id}`),

  create: (data: CreateRegistrarRequest) =>
    http.post<RegistrarResponse>('/registrars', data),

  update: (id: number, data: UpdateRegistrarRequest) =>
    http.put<RegistrarResponse>(`/registrars/${id}`, data),

  delete: (id: number) =>
    http.delete(`/registrars/${id}`),

  // ── Accounts (nested under registrar) ───────────────────────────────────
  listAccounts: (registrarId: number) =>
    http.get<ListResponse<RegistrarAccountResponse>>(`/registrars/${registrarId}/accounts`),

  createAccount: (registrarId: number, data: CreateRegistrarAccountRequest) =>
    http.post<RegistrarAccountResponse>(`/registrars/${registrarId}/accounts`, data),

  // ── Accounts (individual) ────────────────────────────────────────────────
  getAccount: (id: number) =>
    http.get<RegistrarAccountResponse>(`/registrar-accounts/${id}`),

  updateAccount: (id: number, data: UpdateRegistrarAccountRequest) =>
    http.put<RegistrarAccountResponse>(`/registrar-accounts/${id}`, data),

  deleteAccount: (id: number) =>
    http.delete(`/registrar-accounts/${id}`),
}

// Re-export PaginatedData for convenience
export type { PaginatedData }
