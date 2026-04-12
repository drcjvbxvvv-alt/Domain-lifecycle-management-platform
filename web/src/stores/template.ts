import { defineStore } from 'pinia'
import { ref } from 'vue'
import { templateApi } from '@/api/template'
import type { TemplateResponse, TemplateVersionResponse } from '@/types/template'

export const useTemplateStore = defineStore('template', () => {
  const templates = ref<TemplateResponse[]>([])
  const total     = ref(0)
  const loading   = ref(false)
  const current   = ref<TemplateResponse | null>(null)
  const versions  = ref<TemplateVersionResponse[]>([])

  async function fetchByProject(projectId: number | string) {
    loading.value = true
    try {
      const res = await templateApi.listByProject(projectId) as any
      templates.value = res.data?.items ?? res.items ?? []
      total.value     = res.data?.total ?? res.total ?? 0
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: number | string) {
    loading.value = true
    try {
      const res = await templateApi.get(id) as any
      current.value = res.data ?? res
    } finally {
      loading.value = false
    }
  }

  async function fetchVersions(id: number | string) {
    const res = await templateApi.listVersions(id) as any
    versions.value = res.data?.items ?? res.items ?? []
  }

  return { templates, total, loading, current, versions, fetchByProject, fetchOne, fetchVersions }
})
