import type { DomainLifecycleState } from './common'

// Mirror of api/handler/domain.go domainResponse(). Keep in sync.

export interface DomainResponse {
  // Core identity
  id:              number
  uuid:            string
  project_id:      number
  fqdn:            string
  tld:             string | null
  lifecycle_state: DomainLifecycleState
  owner_user_id:   number | null

  // Provider binding
  registrar_account_id: number | null
  dns_provider_id:      number | null
  cdn_account_id:       number | null
  origin_ips:           string[]

  // Denormalised display names — present on list-endpoint items (PE.3)
  registrar_name?:    string | null
  cdn_account_name?:  string | null
  cdn_provider_type?: string | null

  // Registration & expiry
  registration_date: string | null  // ISO date
  expiry_date:        string | null
  auto_renew:         boolean
  grace_end_date:     string | null
  expiry_status:      string | null

  // Status flags
  transfer_lock: boolean
  hold:          boolean

  // Transfer tracking
  transfer_status:            string | null
  transfer_gaining_registrar: string | null
  transfer_requested_at:      string | null
  transfer_completed_at:      string | null
  last_transfer_at:           string | null
  last_renewed_at:            string | null

  // DNS
  nameservers:    Record<string, unknown> | null
  dnssec_enabled: boolean

  // WHOIS
  whois_privacy:      boolean
  registrant_contact: Record<string, unknown> | null
  admin_contact:      Record<string, unknown> | null
  tech_contact:       Record<string, unknown> | null

  // Financial
  annual_cost:    number | null
  currency:       string | null
  purchase_price: number | null
  fee_fixed:      boolean

  // Metadata
  purpose:  string | null
  notes:    string | null
  metadata: Record<string, unknown> | null

  // NS delegation tracking (PB.1)
  ns_delegation_status: NSDelegationStatus
  ns_verified_at:       string | null
  ns_last_checked_at:   string | null
  ns_actual:            string[]

  // Drift / sync tracking (PB.7)
  last_sync_at:  string | null
  last_drift_at: string | null

  created_at: string
  updated_at: string
}

// NSDelegationStatus mirrors the ns_delegation_status column.
export type NSDelegationStatus = 'unset' | 'pending' | 'verified' | 'mismatch'

// DNSBindingStatus is returned by GET /domains/:id/dns-binding.
export interface DNSBindingStatus {
  dns_provider_id:       number | null
  ns_delegation_status:  NSDelegationStatus
  expected_nameservers:  string[]
  actual_nameservers:    string[]
  ns_verified_at:        string | null
  ns_last_checked_at:    string | null
}

export interface RegisterDomainRequest {
  project_id:           number
  fqdn:                 string
  dns_provider_id?:     number | null
  registrar_account_id?: number | null
  cdn_account_id?:      number | null
  origin_ips?:          string[]
  registration_date?:   string | null
  expiry_date?:         string | null
  auto_renew?:          boolean
  annual_cost?:         number | null
  currency?:            string | null
  purpose?:             string | null
  notes?:               string | null
}

export interface UpdateDomainAssetRequest {
  registrar_account_id?: number | null
  dns_provider_id?:      number | null
  cdn_account_id?:       number | null
  origin_ips?:           string[]
  registration_date?:    string | null
  expiry_date?:          string | null
  auto_renew?:           boolean
  transfer_lock?:        boolean
  hold?:                 boolean
  dnssec_enabled?:       boolean
  whois_privacy?:        boolean
  annual_cost?:          number | null
  currency?:             string | null
  purchase_price?:       number | null
  fee_fixed?:            boolean
  purpose?:              string | null
  notes?:                string | null
}

export interface DomainTransitionRequest {
  to:     DomainLifecycleState
  reason: string
}

export interface InitiateTransferRequest {
  gaining_registrar?: string | null
  notes?:             string | null
}

export interface DomainLifecycleHistoryEntry {
  id:           number
  from_state:   DomainLifecycleState | null
  to_state:     DomainLifecycleState
  reason:       string | null
  triggered_by: string
  created_at:   string
}

export interface DomainStats {
  total:        number
  by_registrar: Array<{ registrar_name: string; count: number }>
  by_tld:       Array<{ tld: string; count: number }>
  by_lifecycle: Array<{ lifecycle_state: string; count: number }>
  expiring_30d: number
  expiring_7d:  number
}

export interface DomainVariables {
  domain_id:  number
  variables:  Record<string, unknown>
  updated_at: string
}

export type TransferStatus = 'pending' | 'completed' | 'cancelled'
export type ExpiryStatus   = 'ok' | 'expiring_soon' | 'expired' | 'grace'
