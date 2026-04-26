import { http } from '@/utils/http'
import type {
  MaintenanceWindowResponse,
  MaintenanceWindowWithTargets,
  MaintenanceTarget,
  CreateMaintenanceRequest,
  UpdateMaintenanceRequest,
  AddTargetRequest,
} from '@/types/maintenance'

const BASE = '/api/v1/maintenance'

export const maintenanceApi = {
  list: () =>
    http.get<{ items: MaintenanceWindowResponse[]; total: number }>(BASE),

  get: (id: number) =>
    http.get<MaintenanceWindowWithTargets>(`${BASE}/${id}`),

  create: (data: CreateMaintenanceRequest) =>
    http.post<MaintenanceWindowResponse>(BASE, data),

  update: (id: number, data: UpdateMaintenanceRequest) =>
    http.put<MaintenanceWindowResponse>(`${BASE}/${id}`, data),

  delete: (id: number) =>
    http.delete(`${BASE}/${id}`),

  addTarget: (id: number, data: AddTargetRequest) =>
    http.post<MaintenanceTarget>(`${BASE}/${id}/targets`, data),

  removeTarget: (id: number, targetId: number) =>
    http.delete(`${BASE}/${id}/targets/${targetId}`),
}
