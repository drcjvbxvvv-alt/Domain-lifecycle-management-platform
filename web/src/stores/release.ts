import { defineStore } from 'pinia'
import { ref } from 'vue'
import { releaseApi } from '@/api/release'
import type { ReleaseResponse, ReleaseShardResponse, ReleaseStateHistoryEntry } from '@/types/release'

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
      const res = await releaseApi.list({ project_id: projectId, ...params }) as any
      releases.value = res.data?.items ?? res.items ?? []
      total.value    = res.data?.total ?? res.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      const res = await releaseApi.get(id) as any
      current.value = res.data ?? res
    } finally {
      loading.value = false
    }
  }

  async function fetchShards(id: string) {
    const res = await releaseApi.shards(id) as any
    shards.value = res.data ?? res ?? []
  }

  async function fetchHistory(id: string) {
    const res = await releaseApi.history(id) as any
    history.value = res.data?.items ?? res.items ?? []
  }

  return { releases, total, loading, current, shards, history, fetchByProject, fetchOne, fetchShards, fetchHistory }
})
