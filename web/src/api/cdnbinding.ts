import { http } from '@/utils/http'

// ── Types ─────────────────────────────────────────────────────────────────────

export interface CDNBindingResponse {
  id: number
  uuid: string
  domain_id: number
  cdn_account_id: number
  cdn_cname: string | null
  business_type: CDNBusinessType
  status: CDNDomainStatus
  created_at: string
  updated_at: string
}

export type CDNDomainStatus = 'online' | 'offline' | 'configuring' | 'checking'
export type CDNBusinessType = 'web' | 'download' | 'media'

export interface BindCDNRequest {
  cdn_account_id: number
  business_type?: CDNBusinessType
}

// ── API client ────────────────────────────────────────────────────────────────

export const cdnBindingApi = {
  /** Bind a domain to a CDN account. Calls provider.AddDomain internally. */
  bind(domainId: number, req: BindCDNRequest): Promise<{ code: number; data: CDNBindingResponse; message: string }> {
    return http.post(`/api/v1/domains/${domainId}/cdn-bindings`, req)
  },

  /** List all active CDN bindings for a domain. */
  list(domainId: number): Promise<{ code: number; data: { items: CDNBindingResponse[]; total: number }; message: string }> {
    return http.get(`/api/v1/domains/${domainId}/cdn-bindings`)
  },

  /** Get a single CDN binding by id (local snapshot). */
  get(domainId: number, bindingId: number): Promise<{ code: number; data: CDNBindingResponse; message: string }> {
    return http.get(`/api/v1/domains/${domainId}/cdn-bindings/${bindingId}`)
  },

  /** Unbind: remove from CDN provider and soft-delete locally. */
  unbind(domainId: number, bindingId: number): Promise<void> {
    return http.delete(`/api/v1/domains/${domainId}/cdn-bindings/${bindingId}`)
  },

  /** Poll CDN provider for the latest status + CNAME and update local record. */
  refreshStatus(domainId: number, bindingId: number): Promise<{ code: number; data: CDNBindingResponse; message: string }> {
    return http.post(`/api/v1/domains/${domainId}/cdn-bindings/${bindingId}/refresh`, {})
  },
}
