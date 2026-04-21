// Zone-level RBAC types — mirrors api/handler/dnspermission.go

export type DomainPermissionLevel = 'viewer' | 'editor' | 'admin'

export interface DomainPermission {
  id: number
  domain_id: number
  user_id: number
  username: string
  display_name?: string
  permission: DomainPermissionLevel
  granted_by?: number
  granted_at: string
}

/** Ordered permission levels — higher index = more access. */
export const PERMISSION_LEVELS: DomainPermissionLevel[] = ['viewer', 'editor', 'admin']

/** Returns true if `level` satisfies `minLevel`. */
export function hasPermission(level: DomainPermissionLevel | '', minLevel: DomainPermissionLevel): boolean {
  const idx = PERMISSION_LEVELS.indexOf(level as DomainPermissionLevel)
  const min = PERMISSION_LEVELS.indexOf(minLevel)
  return idx >= min
}
