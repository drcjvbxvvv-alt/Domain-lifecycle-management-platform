// web/src/types/statuspage.ts — mirrors Go status page DTOs.

export type IncidentSeverity = 'info' | 'warning' | 'danger'
export type OverallStatus = 'operational' | 'degraded' | 'outage' | 'maintenance'
export type MonitorStatus = 'up' | 'down' | 'maintenance' | 'unknown'

export interface StatusPageResponse {
  id: number
  uuid: string
  slug: string
  title: string
  description?: string
  published: boolean
  has_password: boolean
  custom_domain?: string
  theme: string
  logo_url?: string
  footer_text?: string
  custom_css?: string
  auto_refresh_seconds: number
  created_at: string
  updated_at: string
}

export interface StatusPageGroup {
  id: number
  status_page_id: number
  name: string
  sort_order: number
}

export interface StatusPageMonitor {
  id: number
  group_id: number
  domain_id: number
  display_name?: string
  sort_order: number
}

export interface StatusPageIncident {
  id: number
  status_page_id: number
  title: string
  content?: string
  severity: IncidentSeverity
  pinned: boolean
  active: boolean
  created_at: string
  updated_at: string
}

export interface CreateStatusPageRequest {
  slug: string
  title: string
  description?: string
  published?: boolean
  password?: string
  custom_domain?: string
  theme?: string
  logo_url?: string
  footer_text?: string
  auto_refresh_seconds?: number
}

export interface UpdateStatusPageRequest extends CreateStatusPageRequest {
  clear_password?: boolean
}

export interface CreateIncidentRequest {
  title: string
  content?: string
  severity: IncidentSeverity
  pinned?: boolean
}

export interface UpdateIncidentRequest extends CreateIncidentRequest {
  active?: boolean
}

// Public status page response (from GET /api/v1/status/:slug)
export interface PublicMonitor {
  monitor_id: number
  domain_id: number
  display_name: string
  status: MonitorStatus
  uptime_pct: number
  response_ms?: number
}

export interface PublicGroup {
  group_id: number
  group_name: string
  status: MonitorStatus
  monitors: PublicMonitor[]
}

export interface PublicStatusResponse {
  page: {
    slug: string
    title: string
    description?: string
    theme: string
    logo_url?: string
    footer_text?: string
    custom_css?: string
    auto_refresh_seconds: number
  }
  overall: OverallStatus
  groups: PublicGroup[]
  incidents: StatusPageIncident[]
}
