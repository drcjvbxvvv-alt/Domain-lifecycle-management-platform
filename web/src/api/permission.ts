// Zone-level RBAC API calls.
import { http } from '@/utils/http'
import type { DomainPermission, DomainPermissionLevel } from '@/types/permission'

interface ListResponse { items: DomainPermission[]; total: number }
interface MyPermissionResponse { permission: DomainPermissionLevel | '' }

export const permissionApi = {
  /** List all explicit permission grants for a domain. */
  list(domainId: number): Promise<{ data: ListResponse }> {
    return http.get(`/domains/${domainId}/permissions`) as Promise<{ data: ListResponse }>
  },

  /** Grant (or update) a user's permission on a domain. */
  grant(domainId: number, userId: number, permission: DomainPermissionLevel): Promise<unknown> {
    return http.post(`/domains/${domainId}/permissions`, { user_id: userId, permission })
  },

  /** Revoke a user's permission grant on a domain. */
  revoke(domainId: number, userId: number): Promise<unknown> {
    return http.delete(`/domains/${domainId}/permissions/${userId}`)
  },

  /** Get the caller's effective permission on a domain. */
  myPermission(domainId: number): Promise<{ data: MyPermissionResponse }> {
    return http.get(`/domains/${domainId}/my-permission`) as Promise<{ data: MyPermissionResponse }>
  },
}
