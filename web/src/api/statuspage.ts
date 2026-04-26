import { http } from '@/utils/http'
import type {
  StatusPageResponse,
  StatusPageGroup,
  StatusPageMonitor,
  StatusPageIncident,
  CreateStatusPageRequest,
  UpdateStatusPageRequest,
  CreateIncidentRequest,
  UpdateIncidentRequest,
  PublicStatusResponse,
} from '@/types/statuspage'

const BASE = '/api/v1/status-pages'

export const statusPageApi = {
  // ── Admin: Status Pages ────────────────────────────────────────────────────
  list: () =>
    http.get<{ items: StatusPageResponse[]; total: number }>(BASE),

  get: (id: number) =>
    http.get<{ page: StatusPageResponse; groups: StatusPageGroup[] }>(`${BASE}/${id}`),

  create: (data: CreateStatusPageRequest) =>
    http.post<StatusPageResponse>(BASE, data),

  update: (id: number, data: UpdateStatusPageRequest) =>
    http.put<StatusPageResponse>(`${BASE}/${id}`, data),

  delete: (id: number) =>
    http.delete(`${BASE}/${id}`),

  // ── Admin: Groups ──────────────────────────────────────────────────────────
  createGroup: (pageId: number, data: { name: string; sort_order?: number }) =>
    http.post<StatusPageGroup>(`${BASE}/${pageId}/groups`, data),

  updateGroup: (pageId: number, groupId: number, data: { name: string; sort_order?: number }) =>
    http.put(`${BASE}/${pageId}/groups/${groupId}`, data),

  deleteGroup: (pageId: number, groupId: number) =>
    http.delete(`${BASE}/${pageId}/groups/${groupId}`),

  // ── Admin: Monitors ────────────────────────────────────────────────────────
  addMonitor: (pageId: number, groupId: number, data: { domain_id: number; display_name?: string; sort_order?: number }) =>
    http.post<StatusPageMonitor>(`${BASE}/${pageId}/groups/${groupId}/monitors`, data),

  removeMonitor: (pageId: number, groupId: number, monitorId: number) =>
    http.delete(`${BASE}/${pageId}/groups/${groupId}/monitors/${monitorId}`),

  // ── Admin: Incidents ───────────────────────────────────────────────────────
  listIncidents: (pageId: number) =>
    http.get<{ items: StatusPageIncident[] }>(`${BASE}/${pageId}/incidents`),

  createIncident: (pageId: number, data: CreateIncidentRequest) =>
    http.post<StatusPageIncident>(`${BASE}/${pageId}/incidents`, data),

  updateIncident: (pageId: number, incidentId: number, data: UpdateIncidentRequest) =>
    http.put<StatusPageIncident>(`${BASE}/${pageId}/incidents/${incidentId}`, data),

  deleteIncident: (pageId: number, incidentId: number) =>
    http.delete(`${BASE}/${pageId}/incidents/${incidentId}`),

  // ── Public ─────────────────────────────────────────────────────────────────
  getPublicStatus: (slug: string, token?: string) =>
    http.get<PublicStatusResponse>(`/api/v1/status/${slug}`, {
      headers: token ? { 'X-Status-Token': token } : {},
    }),

  authPage: (slug: string, password: string) =>
    http.post<{ token: string }>(`/api/v1/status/${slug}/auth`, { password }),
}
