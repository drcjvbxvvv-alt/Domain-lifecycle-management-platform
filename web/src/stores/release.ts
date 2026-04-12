import { defineStore } from 'pinia'
import { ref } from 'vue'
import { releaseApi } from '@/api/release'
import type { ReleaseResponse, ReleaseShardResponse, ReleaseStateHistoryEntry, DryRunResult } from '@/types/release'
import type { ApiResponse, PaginatedData } from '@/types/common'

export const useReleaseStore = defineStore('release', () => {
  const releases = ref<ReleaseResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<ReleaseResponse | null>(null)
  const shards   = ref<ReleaseShardResponse[]>([])
  const history  = ref<ReleaseStateHistoryEntry[]>([])

  async function fetchByProject(projectId: number, params?: { cursor?: string }) {
    loading.value = true
    try {
      const res = await releaseApi.list({ project_id: projectId, ...params }) as unknown as ApiResponse<PaginatedData<ReleaseResponse>>
      releases.value = res.data?.items ?? []
      total.value    = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      const res = await releaseApi.get(id) as unknown as ApiResponse<ReleaseResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function fetchShards(id: string) {
    const res = await releaseApi.shards(id) as unknown as ApiResponse<ReleaseShardResponse[]>
    shards.value = res.data ?? []
  }

  async function fetchHistory(id: string) {
    const res = await releaseApi.history(id) as unknown as ApiResponse<{ items: ReleaseStateHistoryEntry[] }>
    history.value = res.data?.items ?? []
  }

  async function create(data: { project_id: number; project_slug: string; template_version_id: number; release_type?: string; description?: string; domain_ids?: number[] }) {
    const res = await releaseApi.create(data) as unknown as ApiResponse<ReleaseResponse>
    return res.data
  }

  async function pause(id: string, reason?: string) {
    await releaseApi.pause(id, reason)
    await fetchOne(id)
  }

  async function resume(id: string) {
    await releaseApi.resume(id)
    await fetchOne(id)
  }

  async function cancel(id: string, reason?: string) {
    await releaseApi.cancel(id, reason)
    await fetchOne(id)
  }

  async function rollback(id: string, reason?: string) {
    await releaseApi.rollback(id, reason)
    await fetchOne(id)
  }

  async function dryRun(id: string): Promise<DryRunResult | null> {
    const res = await releaseApi.dryRun(id) as unknown as ApiResponse<DryRunResult>
    return res.data ?? null
  }

  return { releases, total, loading, current, shards, history, fetchByProject, fetchOne, fetchShards, fetchHistory, create, pause, resume, cancel, rollback, dryRun }
})
