<script setup lang="ts">
import { onMounted, onUnmounted, ref, computed, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton, NSpace, NSelect, useMessage } from 'naive-ui'
import { AppTable, PageHeader, StatusTag, PageHint } from '@/components'
import { useAgentStore } from '@/stores/agent'
import type { AgentResponse } from '@/types/agent'

const router  = useRouter()
const store   = useAgentStore()
const message = useMessage()

const statusFilter = ref<string | null>(null)
const actionLoading = ref<number | null>(null)

const statusOptions = [
  { label: '全部', value: null },
  { label: 'online',    value: 'online'    },
  { label: 'busy',      value: 'busy'      },
  { label: 'idle',      value: 'idle'      },
  { label: 'offline',   value: 'offline'   },
  { label: 'draining',  value: 'draining'  },
  { label: 'disabled',  value: 'disabled'  },
  { label: 'error',     value: 'error'     },
]

const filteredAgents = computed(() =>
  statusFilter.value
    ? store.agents.filter(a => a.status === statusFilter.value)
    : store.agents
)

async function doAction(row: AgentResponse, fn: (id: number) => Promise<void>, label: string) {
  actionLoading.value = row.id
  try {
    await fn(row.id)
    message.success(`操作成功：${label}`)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = null
  }
}

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
  { title: '操作', key: 'actions', width: 220, fixed: 'right',
    render: (row) => {
      const loading = actionLoading.value === row.id
      const btns = []

      if (['online', 'busy', 'idle'].includes(row.status)) {
        btns.push(h(NButton, {
          size: 'small', type: 'warning', ghost: true, loading, disabled: loading,
          onClick: () => doAction(row, store.drain, '排空'),
        }, { default: () => '排空' }))
      }
      if (!['disabled', 'draining'].includes(row.status)) {
        btns.push(h(NButton, {
          size: 'small', type: 'error', ghost: true, loading, disabled: loading,
          onClick: () => doAction(row, store.disable, '停用'),
        }, { default: () => '停用' }))
      }
      if (['disabled', 'error', 'draining'].includes(row.status)) {
        btns.push(h(NButton, {
          size: 'small', type: 'primary', ghost: true, loading, disabled: loading,
          onClick: () => doAction(row, store.enable, '啟用'),
        }, { default: () => '啟用' }))
      }
      btns.push(h(NButton, {
        size: 'small', quaternary: true,
        onClick: () => router.push(`/agents/${row.id}`),
      }, { default: () => '查看' }))

      return h(NSpace, { size: 4 }, { default: () => btns })
    },
  },
]

let timer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  store.fetchList()
  timer = setInterval(() => store.fetchList(), 10_000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="list-page">
    <PageHeader title="Agent 管理" subtitle="所有 Nginx 節點">
      <template #actions>
        <NSelect
          v-model:value="statusFilter"
          :options="statusOptions"
          placeholder="篩選狀態"
          style="width: 130px"
          clearable
        />
      </template>
      <template #hint>
        <PageHint storage-key="agent-list" title="Agent 管理說明">
          Agent 是部署在各 Nginx 伺服器上的 pull agent，負責拉取 artifact 並執行部署。<br>
          <strong>狀態</strong>：online 就緒 / busy 執行中 / offline 心跳逾時 / draining 排空中 / error 需處理<br>
          心跳超時自動轉為 offline；超過閾值後升級為 <strong>error</strong>，需排查主機問題後在詳情頁手動清除。<br>
          <strong>排空</strong>：Agent 完成目前任務後不接新任務。<strong>停用</strong>：立即停止派發新任務。
        </PageHint>
      </template>
    </PageHeader>
    <AppTable
      :columns="columns"
      :data="filteredAgents"
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
