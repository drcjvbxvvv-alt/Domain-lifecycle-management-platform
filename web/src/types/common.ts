// ── API response wrappers ──────────────────────────────────────────────────
export interface ApiResponse<T> {
  code: number
  data: T
  message: string
}

export interface PaginatedData<T> {
  items: T[]
  total: number
  cursor?: string
}

// ── RBAC roles (per ADR-0003 D7) ────────────────────────────────────────────
export type Role = 'viewer' | 'operator' | 'release_manager' | 'admin' | 'auditor'

// ── Domain Lifecycle states (CLAUDE.md §"Domain Lifecycle State Machine") ──
export type DomainLifecycleState =
  | 'requested'
  | 'approved'
  | 'provisioned'
  | 'active'
  | 'disabled'
  | 'retired'

// ── Release status (CLAUDE.md §"Release State Machine") ────────────────────
export type ReleaseStatus =
  | 'pending'
  | 'planning'
  | 'ready'
  | 'executing'
  | 'paused'
  | 'succeeded'
  | 'failed'
  | 'rolling_back'
  | 'rolled_back'
  | 'cancelled'

// ── Release type ────────────────────────────────────────────────────────────
export type ReleaseType = 'html' | 'nginx' | 'full'

// ── Agent status (CLAUDE.md §"Agent State Machine") ────────────────────────
export type AgentStatus =
  | 'registered'
  | 'online'
  | 'busy'
  | 'idle'
  | 'offline'
  | 'draining'
  | 'disabled'
  | 'upgrading'
  | 'error'

// ── Alert severity ──────────────────────────────────────────────────────────
export type AlertSeverity = 'P1' | 'P2' | 'P3' | 'INFO'

// ── Union of all status values that StatusTag knows how to render ──────────
export type AnyStatus = DomainLifecycleState | ReleaseStatus | AgentStatus

// ── Semantic categories used by StatusTag and color tokens ─────────────────
// Each AnyStatus value maps to one of these semantic buckets, which in turn
// maps to a color token in styles/tokens.ts. See FRONTEND_GUIDE.md.
export type StatusSemantic =
  | 'success'
  | 'progress'
  | 'warning'
  | 'danger'
  | 'neutral'
  | 'upgrading'
