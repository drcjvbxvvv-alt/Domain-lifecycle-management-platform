// web/src/types/registrar.ts
// Mirror Go handler DTOs exactly. Keep in sync with api/handler/registrar.go.

export interface RegistrarResponse {
  id: number
  uuid: string
  name: string
  url: string | null
  api_type: string | null
  capabilities: Record<string, unknown>
  notes: string | null
  created_at: string
  updated_at: string
}

export interface RegistrarAccountResponse {
  id: number
  uuid: string
  registrar_id: number
  account_name: string
  owner_user_id: number | null
  is_default: boolean
  notes: string | null
  created_at: string
  updated_at: string
  // NOTE: credentials never returned by API (security)
}

export interface CreateRegistrarRequest {
  name: string
  url?: string | null
  api_type?: string | null
  capabilities?: Record<string, unknown>
  notes?: string | null
}

export interface UpdateRegistrarRequest {
  name: string
  url?: string | null
  api_type?: string | null
  capabilities?: Record<string, unknown>
  notes?: string | null
}

export interface CreateRegistrarAccountRequest {
  account_name: string
  owner_user_id?: number | null
  credentials?: Record<string, unknown>
  is_default?: boolean
  notes?: string | null
}

export interface UpdateRegistrarAccountRequest {
  account_name: string
  owner_user_id?: number | null
  credentials?: Record<string, unknown>
  is_default?: boolean
  notes?: string | null
}
