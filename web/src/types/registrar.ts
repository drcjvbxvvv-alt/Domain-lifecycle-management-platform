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

// ── Sync ────────────────────────────────────────────────────────────────────

export interface SyncItemError {
  fqdn: string
  message: string
}

export interface SyncResult {
  total: number
  updated: number
  not_found: string[]
  errors?: SyncItemError[]
}

// ── Provider credentials shapes ──────────────────────────────────────────────
// Used by the credential editor in RegistrarDetail.vue.
// Field names must match the Go structs in pkg/provider/registrar/*.go exactly.

// GoDaddy: field names match GoDaddy dev portal labels (developer.godaddy.com/keys)
export interface GoDaddyCredentials {
  key: string
  secret: string
  environment: 'production' | 'ote'
}

// Namecheap: api_user + api_key + client_ip (must be whitelisted in account)
export interface NamecheapCredentials {
  api_user: string
  api_key: string
  username: string       // usually same as api_user; leave blank to auto-copy
  client_ip: string      // server IP — must be whitelisted in Namecheap Profile > Tools > API Access
  environment: 'production' | 'sandbox'
}

// Aliyun (阿里雲萬網): AccessKey pair from RAM console
export interface AliyunCredentials {
  access_key_id: string
  access_key_secret: string
}
