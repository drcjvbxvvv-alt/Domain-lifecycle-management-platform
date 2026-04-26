<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import {
  NGrid, NGi, NCard, NStatistic, NSpin, NButton, NTag,
  useMessage,
} from 'naive-ui'
import { PageHeader } from '@/components'
import { AppTable } from '@/components'
import { gfwApi } from '@/api/gfw'
import type { BlockedDomainRow, GFWStats } from '@/api/gfw'

const router  = useRouter()
const message = useMessage()

// ── State ─────────────────────────────────────────────────────────────────────
const loadingStats   = ref(false)
const loadingDomains = ref(false)
const stats          = ref<GFWStats | null>(null)
const blockedDomains = ref<BlockedDomainRow[]>([])

// ── Data fetch ─────────────────────────────────────────────────────────────────

async function fetchStats() {
  loadingStats.value = true
  try {
    const res = await gfwApi.getStats() as any
    stats.value = res?.data ?? null
  } catch {
    message.error('載入 GFW 統計失敗')
  } finally {
    loadingStats.value = false
  }
}

async function fetchBlockedDomains() {
  loadingDomains.value = true
  try {
    const res = await gfwApi.listBlockedDomains() as any
    blockedDomains.value = res?.data?.items ?? []
  } catch {
    message.error('載入被封鎖域名列表失敗')
  } finally {
    loadingDomains.value = false
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function statusLabel(status: string): string {
  if (status === 'blocked')          return '已封鎖'
  if (status === 'possibly_blocked') return '可能封鎖'
  return status
}

function statusType(status: string): 'error' | 'warning' | 'default' {
  if (status === 'blocked')          return 'error'
  if (status === 'possibly_blocked') return 'warning'
  return 'default'
}

function blockingTypeLabel(t: string | null): string {
  if (!t) return '-'
  const map: Record<string, string> = {
    dns:          'DNS 注入',
    tcp_ip:       'TCP/IP 封鎖',
    tls_sni:      'TLS SNI',
    'http-failure': 'HTTP 失敗',
    'http-diff':  'HTTP 差異',
    indeterminate: '不確定',
  }
  return map[t] ?? t
}

function formatDate(d: string | null): string {
  if (!d) return '-'
  return new Date(d).toLocaleDateString('zh-TW')
}

// ── Table columns ─────────────────────────────────────────────────────────────
const columns: DataTableColumns<BlockedDomainRow> = [
  { title: '域名', key: 'fqdn', ellipsis: { tooltip: true }, minWidth: 200 },
  {
    title: '封鎖狀態', key: 'blocking_status', width: 120,
    render: (row) => h(NTag, { type: statusType(row.blocking_status), size: 'small' },
      { default: () => statusLabel(row.blocking_status) }),
  },
  {
    title: '封鎖類型', key: 'blocking_type', width: 130,
    render: (row) => blockingTypeLabel(row.blocking_type),
  },
  {
    title: '信心值', key: 'blocking_confidence', width: 90,
    render: (row) => `${Math.round(row.blocking_confidence * 100)}%`,
  },
  {
    title: '封鎖起始', key: 'blocking_since', width: 120,
    render: (row) => formatDate(row.blocking_since),
  },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/domains/${row.id}`),
    }, { default: () => '查看' }),
  },
]

// ── Lifecycle ─────────────────────────────────────────────────────────────────
onMounted(() => {
  fetchStats()
  fetchBlockedDomains()
})
</script>

<template>
  <div class="gfw-dashboard">
    <PageHeader title="GFW 封鎖監控" subtitle="Great Firewall 偵測儀表板">
      <template #actions>
        <NButton @click="fetchStats(); fetchBlockedDomains()">重新整理</NButton>
      </template>
    </PageHeader>

    <!-- Summary cards -->
    <NSpin :show="loadingStats">
      <NGrid :cols="4" :x-gap="16" :y-gap="16" class="stats-grid">
        <NGi>
          <NCard>
            <NStatistic
              label="監控域名總數"
              :value="stats?.total_monitored ?? 0"
            />
          </NCard>
        </NGi>
        <NGi>
          <NCard>
            <NStatistic
              label="已確認封鎖"
              :value="stats?.total_blocked ?? 0"
              :value-style="{ color: 'var(--error-color, #d03050)' }"
            />
          </NCard>
        </NGi>
        <NGi>
          <NCard>
            <NStatistic
              label="可能封鎖"
              :value="stats?.total_possibly_blocked ?? 0"
              :value-style="{ color: 'var(--warning-color, #f0a020)' }"
            />
          </NCard>
        </NGi>
        <NGi>
          <NCard>
            <NStatistic
              label="DNS 封鎖"
              :value="stats?.blocked_dns ?? 0"
            />
          </NCard>
        </NGi>
        <NGi>
          <NCard>
            <NStatistic
              label="TCP/IP 封鎖"
              :value="stats?.blocked_tcp_ip ?? 0"
            />
          </NCard>
        </NGi>
        <NGi>
          <NCard>
            <NStatistic
              label="TLS SNI 封鎖"
              :value="stats?.blocked_tls_sni ?? 0"
            />
          </NCard>
        </NGi>
        <NGi>
          <NCard>
            <NStatistic
              label="HTTP 封鎖"
              :value="stats?.blocked_http ?? 0"
            />
          </NCard>
        </NGi>
      </NGrid>
    </NSpin>

    <!-- Blocked domain list -->
    <NCard title="目前封鎖域名" class="table-card">
      <AppTable
        :columns="columns"
        :data="blockedDomains"
        :loading="loadingDomains"
        :row-key="(r: BlockedDomainRow) => r.id"
      />
    </NCard>
  </div>
</template>

<style scoped>
.gfw-dashboard {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: auto;
}
.stats-grid {
  padding: 16px var(--content-padding, 16px);
}
.table-card {
  margin: 0 var(--content-padding, 16px) 16px;
}
</style>
