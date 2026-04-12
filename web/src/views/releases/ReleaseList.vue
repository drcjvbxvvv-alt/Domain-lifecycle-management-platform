<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton } from 'naive-ui'
import AppTable from '@/components/AppTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useReleaseStore } from '@/stores/release'
import type { ReleaseResponse } from '@/types/release'

const route  = useRoute()
const router = useRouter()
const store  = useReleaseStore()

// Supports both /releases (global nav) and /projects/:id/releases
const projectId = route.params.id as string | undefined

const columns: DataTableColumns<ReleaseResponse> = [
  { title: 'Release ID', key: 'release_id', ellipsis: { tooltip: true }, width: 220 },
  { title: '類型',  key: 'release_type', width: 80 },
  { title: '狀態',  key: 'status', width: 140,
    render: (row) => h(StatusTag, { status: row.status }) },
  { title: '域名數', key: 'total_domains', width: 80,
    render: (row) => row.total_domains ?? '-' },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${row.project_id}/releases/${row.uuid}`),
    }, { default: () => '查看' }) },
]

onMounted(() => {
  if (projectId) {
    store.fetchByProject(Number(projectId))
  }
})
</script>

<template>
  <div>
    <PageHeader title="發布管理" :subtitle="projectId ? `專案 #${projectId}` : ''" />
    <AppTable :columns="columns" :data="store.releases" :loading="store.loading"
      :row-key="(r) => r.uuid" style="margin-top: 16px;" />
  </div>
</template>
