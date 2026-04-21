import { defineStore } from 'pinia'
import { ref } from 'vue'
import { importApi } from '@/api/import'
import type { ImportJob, PreviewResult } from '@/types/import'

export const useImportStore = defineStore('import', () => {
  const jobs   = ref<ImportJob[]>([])
  const loading = ref(false)

  async function fetchJobs(projectId?: number) {
    loading.value = true
    try {
      const res = await importApi.listJobs({ project_id: projectId, limit: 50 })
      jobs.value = (res as any).data?.items ?? []
    } finally {
      loading.value = false
    }
  }

  async function upload(
    file: File,
    projectId: number,
    registrarAccountId?: number,
  ): Promise<ImportJob> {
    const res = await importApi.upload(file, projectId, registrarAccountId)
    const job = (res as any).data as ImportJob
    // Prepend so it shows up first in the list
    jobs.value = [job, ...jobs.value]
    return job
  }

  async function preview(file: File): Promise<PreviewResult> {
    const res = await importApi.preview(file)
    return (res as any).data as PreviewResult
  }

  async function pollJob(id: number): Promise<ImportJob> {
    const res = await importApi.getJob(id)
    const updated = (res as any).data as ImportJob
    const idx = jobs.value.findIndex(j => j.id === id)
    if (idx !== -1) jobs.value[idx] = updated
    return updated
  }

  return { jobs, loading, fetchJobs, upload, preview, pollJob }
})
