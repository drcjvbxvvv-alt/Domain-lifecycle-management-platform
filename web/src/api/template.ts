import { http } from '@/utils/http'
import type { TemplateResponse, TemplateVersionResponse } from '@/types/template'
import type { PaginatedData } from '@/types/common'

export const templateApi = {
  listByProject: (projectId: number | string) =>
    http.get<PaginatedData<TemplateResponse>>(`/projects/${projectId}/templates`),

  get: (id: number | string) =>
    http.get<TemplateResponse>(`/templates/${id}`),

  listVersions: (id: number | string) =>
    http.get<{ items: TemplateVersionResponse[] }>(`/templates/${id}/versions`),

  getVersion: (versionId: number | string) =>
    http.get<TemplateVersionResponse>(`/template-versions/${versionId}`),
}
