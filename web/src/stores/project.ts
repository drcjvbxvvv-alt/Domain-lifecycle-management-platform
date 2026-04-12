import { defineStore } from 'pinia'
import { ref } from 'vue'
import { projectApi } from '@/api/project'
import type { ProjectResponse, CreateProjectRequest } from '@/types/project'
import type { ApiResponse, PaginatedData } from '@/types/common'

export const useProjectStore = defineStore('project', () => {
  const projects = ref<ProjectResponse[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const current  = ref<ProjectResponse | null>(null)

  async function fetchList() {
    loading.value = true
    try {
      const res = await projectApi.list() as unknown as ApiResponse<PaginatedData<ProjectResponse>>
      projects.value = res.data?.items ?? []
      total.value    = res.data?.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      const res = await projectApi.get(id) as unknown as ApiResponse<ProjectResponse>
      current.value = res.data
    } finally {
      loading.value = false
    }
  }

  async function create(data: CreateProjectRequest) {
    const res = await projectApi.create(data) as unknown as ApiResponse<ProjectResponse>
    await fetchList()
    return res.data
  }

  return { projects, total, loading, current, fetchList, fetchOne, create }
})
