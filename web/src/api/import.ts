import { apiClient } from './client'
import type { ApiResponse } from '@/types/common'
import type { ImportJob, PreviewResult } from '@/types/import'

export const importApi = {
  /** Upload CSV and create an import job. Returns 202 Accepted with the job. */
  upload(
    csvFile: File,
    projectId: number,
    registrarAccountId?: number,
  ): Promise<ApiResponse<ImportJob>> {
    const form = new FormData()
    form.append('csv_file', csvFile)
    form.append('project_id', String(projectId))
    if (registrarAccountId) {
      form.append('registrar_account_id', String(registrarAccountId))
    }
    return apiClient.post('/domains/import', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  /** Parse CSV and return preview without creating a job. */
  preview(csvFile: File): Promise<ApiResponse<PreviewResult>> {
    const form = new FormData()
    form.append('csv_file', csvFile)
    return apiClient.post('/domains/import/preview', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  /** Get a single import job by ID. */
  getJob(id: number): Promise<ApiResponse<ImportJob>> {
    return apiClient.get(`/domains/import/jobs/${id}`)
  },

  /** List import jobs, optionally filtered by project. */
  listJobs(params?: {
    project_id?: number
    limit?: number
    offset?: number
  }): Promise<ApiResponse<{ items: ImportJob[]; total: number }>> {
    return apiClient.get('/domains/import/jobs', { params })
  },
}
