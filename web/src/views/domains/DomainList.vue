<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton } from 'naive-ui'
import AppTable from '@/components/AppTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useDomainStore } from '@/stores/domain'
import type { DomainResponse } from '@/types/domain'

const route  = useRoute()
const router = useRouter()
const store  = useDomainStore()

// Supports both /domains (global) and /projects/:id/domains (project-scoped)
const projectId = route.params.id as string | undefined

const columns: DataTableColumns<DomainResponse> = [
  { title: 'FQDN', key: 'fqdn', ellipsis: { tooltip: true } },
  { title: '狀態', key: 'lifecycle_state', width: 140,
    render: (row) => h(StatusTag, { status: row.lifecycle_state }) },
  { title: 'DNS Provider', key: 'dns_provider', width: 140,
    render: (row) => row.dns_provider ?? '-' },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/domains/${row.uuid}`),
    }, { default: () => '查看' }) },
]

onMounted(() => store.fetchList(projectId ? { project_id: Number(projectId) } : undefined))
</script>

<template>
  <div>
    <PageHeader title="域名管理" :subtitle="projectId ? `專案 #${projectId}` : '所有域名'" />
    <AppTable :columns="columns" :data="store.domains" :loading="store.loading"
      :row-key="(r) => r.uuid" style="margin-top: 16px;" />
  </div>
</template>
