import { defineStore } from 'pinia'
import { ref } from 'vue'
import { projectApi } from '@/api/project'
import type { ProjectResponse } from '@/types/project'

export const useProjectStore = defineStore('project', () => {
  const projects = ref<ProjectResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<ProjectResponse | null>(null)

  async function fetchList() {
    loading.value = true
    try {
      const res = await projectApi.list() as any
      projects.value = res.data?.items ?? res.items ?? []
      total.value    = res.data?.total ?? res.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      const res = await projectApi.get(id) as any
      current.value = res.data ?? res
    } finally {
      loading.value = false
    }
  }

  return { projects, total, loading, current, fetchList, fetchOne }
})
