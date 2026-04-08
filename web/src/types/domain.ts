import type { DomainLifecycleState } from './common'

// Mirror of internal/lifecycle DTOs (Go side). Keep in sync.
// Per ADR-0003, domains are flat — no main/subdomain hierarchy, no prefix rules.

export interface DomainResponse {
  uuid:            string
  fqdn:            string
  project_id:      number
  lifecycle_state: DomainLifecycleState
  owner_user_id:   number | null
  dns_provider:    string | null
  dns_zone:        string | null
  tags?:           string[]
  created_at:      string
  updated_at:      string
}

export interface RegisterDomainRequest {
  project_id:    number
  fqdn:          string
  owner_user_id?: number
  dns_provider?: string
  dns_zone?:     string
  tags?:         string[]
}

export interface DomainTransitionRequest {
  to:     DomainLifecycleState
  reason: string
}

export interface DomainLifecycleHistoryEntry {
  id:           number
  from_state:   DomainLifecycleState | null
  to_state:     DomainLifecycleState
  reason:       string | null
  triggered_by: string
  created_at:   string
}

export interface DomainVariables {
  domain_id:  number
  variables:  Record<string, unknown>
  updated_at: string
}
