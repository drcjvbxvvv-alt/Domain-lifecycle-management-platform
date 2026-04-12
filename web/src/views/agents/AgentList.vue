<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton } from 'naive-ui'
import AppTable from '@/components/AppTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useAgentStore } from '@/stores/agent'
import type { AgentResponse } from '@/types/agent'

const router = useRouter()
const store  = useAgentStore()

const columns: DataTableColumns<AgentResponse> = [
  { title: 'Agent ID', key: 'agent_id', ellipsis: { tooltip: true }, width: 220 },
  { title: 'Hostname', key: 'hostname', ellipsis: { tooltip: true } },
  { title: 'IP',       key: 'ip', width: 140,
    render: (row) => row.ip ?? '-' },
  { title: 'Region',   key: 'region', width: 100,
    render: (row) => row.region ?? '-' },
  { title: '版本',     key: 'agent_version', width: 100,
    render: (row) => row.agent_version ?? '-' },
  { title: '狀態',    key: 'status', width: 120,
    render: (row) => h(StatusTag, { status: row.status }) },
  { title: '最後心跳', key: 'last_seen_at', width: 180,
    render: (row) => row.last_seen_at
      ? new Date(row.last_seen_at).toLocaleString('zh-TW')
      : '-' },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/agents/${row.id}`),
    }, { default: () => '查看' }) },
]

onMounted(() => store.fetchList())
</script>

<template>
  <div>
    <PageHeader title="Agent 管理" subtitle="所有 Nginx 節點" />
    <AppTable :columns="columns" :data="store.agents" :loading="store.loading"
      :row-key="(r) => String(r.id)" style="margin-top: 16px;" />
  </div>
</template>
