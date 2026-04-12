<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton } from 'naive-ui'
import AppTable from '@/components/AppTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useProjectStore } from '@/stores/project'
import type { ProjectResponse } from '@/types/project'

const router  = useRouter()
const store   = useProjectStore()

const columns: DataTableColumns<ProjectResponse> = [
  { title: '名稱',    key: 'name',       ellipsis: { tooltip: true } },
  { title: 'Slug',   key: 'slug',       width: 160 },
  { title: '說明',    key: 'description', ellipsis: { tooltip: true } },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${row.uuid}`),
    }, { default: () => '查看' }),
  },
]

onMounted(() => store.fetchList())
</script>

<template>
  <div>
    <PageHeader title="專案管理" subtitle="管理所有專案" />
    <AppTable
      :columns="columns"
      :data="store.projects"
      :loading="store.loading"
      :row-key="(r) => r.uuid"
      style="margin-top: 16px;"
    />
  </div>
</template>
