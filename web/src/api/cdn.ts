import { http } from '@/utils/http'

type ListResponse<T> = { items: T[]; total: number }

export interface CDNProviderResponse {
  id: number
  uuid: string
  name: string
  provider_type: string
  description: string | null
  created_at: string
  updated_at: string
}

export interface CDNAccountResponse {
  id: number
  uuid: string
  cdn_provider_id: number
  account_name: string
  credentials: Record<string, unknown>
  notes: string | null
  enabled: boolean
  created_by: number | null
  created_at: string
  updated_at: string
}

export interface CreateCDNProviderRequest {
  name: string
  provider_type: string
  description?: string | null
}

export interface UpdateCDNProviderRequest {
  name: string
  provider_type: string
  description?: string | null
}

export interface CreateCDNAccountRequest {
  account_name: string
  credentials?: Record<string, unknown>
  notes?: string | null
  enabled?: boolean
}

export interface UpdateCDNAccountRequest {
  account_name: string
  credentials?: Record<string, unknown>
  notes?: string | null
  enabled?: boolean
}

// Known provider_type values — must match allowedProviderTypes in service.go
export const CDN_PROVIDER_TYPES = [
  { label: 'Cloudflare',   value: 'cloudflare'  },
  { label: '聚合',          value: 'juhe'        },
  { label: '網宿',          value: 'wangsu'      },
  { label: '白山雲',        value: 'baishan'     },
  { label: '騰訊雲 CDN',    value: 'tencent_cdn' },
  { label: '華為雲 CDN',    value: 'huawei_cdn'  },
  { label: '阿里雲 CDN',    value: 'aliyun_cdn'  },
  { label: 'Fastly',       value: 'fastly'      },
  { label: '其他',          value: 'other'       },
]

export const cdnApi = {
  // ── Providers ───────────────────────────────────────────────────────────────
  listProviders: () =>
    http.get<ListResponse<CDNProviderResponse>>('/cdn-providers'),

  getProvider: (id: number) =>
    http.get<CDNProviderResponse>(`/cdn-providers/${id}`),

  createProvider: (data: CreateCDNProviderRequest) =>
    http.post<CDNProviderResponse>('/cdn-providers', data),

  updateProvider: (id: number, data: UpdateCDNProviderRequest) =>
    http.put<CDNProviderResponse>(`/cdn-providers/${id}`, data),

  deleteProvider: (id: number) =>
    http.delete(`/cdn-providers/${id}`),

  // ── Accounts (nested under provider) ────────────────────────────────────────
  listAccounts: (providerId: number) =>
    http.get<ListResponse<CDNAccountResponse>>(`/cdn-providers/${providerId}/accounts`),

  createAccount: (providerId: number, data: CreateCDNAccountRequest) =>
    http.post<CDNAccountResponse>(`/cdn-providers/${providerId}/accounts`, data),

  // ── Accounts (individual) ────────────────────────────────────────────────────
  listAllAccounts: () =>
    http.get<ListResponse<CDNAccountResponse>>('/cdn-accounts'),

  getAccount: (id: number) =>
    http.get<CDNAccountResponse>(`/cdn-accounts/${id}`),

  updateAccount: (id: number, data: UpdateCDNAccountRequest) =>
    http.put<CDNAccountResponse>(`/cdn-accounts/${id}`, data),

  deleteAccount: (id: number) =>
    http.delete(`/cdn-accounts/${id}`),
}
