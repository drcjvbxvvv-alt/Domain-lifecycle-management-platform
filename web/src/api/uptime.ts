import { http } from '@/utils/http'

export interface UptimeResult {
  domain_id: number
  probe_type: string
  days: number
  up_count: number
  down_count: number
  uptime_pct: number
}

export interface DataPoint {
  bucket: string
  avg_response_ms: number | null
  min_response_ms: number | null
  max_response_ms: number | null
  p95_response_ms: number | null
}

export interface DomainUptimeRow {
  domain_id: number
  fqdn: string
  uptime_pct: number
  up_count: number
  down_count: number
}

export interface CalendarDay {
  date: string
  uptime_pct: number  // -1 = no data
}

export const uptimeApi = {
  getUptime: (domainId: number, params?: { probe_type?: string; days?: number }) =>
    http.get<UptimeResult>(`/api/v1/uptime/${domainId}`, { params }),

  getResponseTimeSeries: (domainId: number, params?: {
    probe_type?: string
    granularity?: 'hourly' | 'daily'
    from?: string
    to?: string
  }) =>
    http.get<{ items: DataPoint[] }>(`/api/v1/uptime/${domainId}/response-time`, { params }),

  getWorstPerformers: (params?: { probe_type?: string; days?: number; limit?: number }) =>
    http.get<{ items: DomainUptimeRow[] }>('/api/v1/uptime/worst', { params }),

  getUptimeCalendar: (domainId: number, year: number, month: number) =>
    http.get<{ domain_id: number; year: number; month: number; days: CalendarDay[] }>(
      `/api/v1/uptime/${domainId}/calendar`,
      { params: { year, month } }
    ),
}
