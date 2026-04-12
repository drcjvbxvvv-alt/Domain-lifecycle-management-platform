<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton } from 'naive-ui'
import { AppTable, PageHeader, StatusTag, PageHint } from '@/components'
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
  <div class="list-page">
    <PageHeader title="Agent 管理" subtitle="所有 Nginx 節點">
      <template #hint>
        <PageHint storage-key="agent-list" title="Agent 管理說明">
          Agent 是部署在各 Nginx 伺服器上的 pull agent，負責拉取 artifact 並執行部署。<br>
          <strong>狀態</strong>：online 就緒 / busy 執行中 / offline 心跳逾時 / draining 排空中 / error 需處理<br>
          心跳超時自動轉為 offline；超過閾值後升級為 <strong>error</strong>，需排查主機問題後在詳情頁手動清除。
        </PageHint>
      </template>
    </PageHeader>
    <AppTable
      :columns="columns"
      :data="store.agents"
      :loading="store.loading"
      :row-key="(r) => String(r.id)"
    />
  </div>
</template>

<style scoped>
.list-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
</style>
