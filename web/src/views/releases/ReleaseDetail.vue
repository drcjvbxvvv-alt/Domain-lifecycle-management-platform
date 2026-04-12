<script setup lang="ts">
import { onMounted, computed, ref, h } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem,
  NTimeline, NTimelineItem, NButton, NSpace, NTag, NText, useMessage,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { AppTable, PageHeader, StatusTag, ConfirmModal, PageHint } from '@/components'
import { useReleaseStore } from '@/stores/release'
import type { ReleaseShardResponse, DryRunResult } from '@/types/release'

const route   = useRoute()
const store   = useReleaseStore()
const message = useMessage()
const rid     = route.params.rid as string

const actionLoading  = ref(false)
const showCancel     = ref(false)
const showRollback   = ref(false)
const dryRunResult   = ref<DryRunResult | null>(null)
const dryRunLoading  = ref(false)

const canPause    = computed(() => store.current?.status === 'executing')
const canResume   = computed(() => store.current?.status === 'paused')
const canCancel   = computed(() => {
  const s = store.current?.status
  return s === 'pending' || s === 'planning' || s === 'ready' || s === 'paused' || s === 'failed'
})
const canRollback = computed(() => {
  const s = store.current?.status
  return s === 'failed' || s === 'paused'
})
const canDryRun   = computed(() => {
  const s = store.current?.status
  return s !== 'pending' && s !== 'planning' && store.current?.artifact_id != null
})

const shardCols: DataTableColumns<ReleaseShardResponse> = [
  { title: '#', key: 'shard_index', width: 60 },
  { title: 'Canary', key: 'is_canary', width: 80,
    render: (row) => row.is_canary ? 'canary' : '-' },
  { title: '狀態', key: 'status', width: 140,
    render: (row) => h(StatusTag, { status: row.status }) },
  { title: '域名數', key: 'domain_count', width: 80 },
  { title: '成功', key: 'success_count', width: 70 },
  { title: '失敗', key: 'failure_count', width: 70 },
  { title: '開始時間', key: 'started_at', width: 180,
    render: (row) => row.started_at ? new Date(row.started_at).toLocaleString('zh-TW') : '-' },
  { title: '結束時間', key: 'ended_at', width: 180,
    render: (row) => row.ended_at ? new Date(row.ended_at).toLocaleString('zh-TW') : '-' },
]

async function handlePause() {
  actionLoading.value = true
  try {
    await store.pause(rid, 'operator paused')
    message.success('已暫停')
    await store.fetchHistory(rid)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function handleResume() {
  actionLoading.value = true
  try {
    await store.resume(rid)
    message.success('已恢復')
    await store.fetchHistory(rid)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function handleCancel() {
  actionLoading.value = true
  try {
    await store.cancel(rid, 'operator cancelled')
    message.success('已取消')
    showCancel.value = false
    await store.fetchHistory(rid)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function handleRollback() {
  actionLoading.value = true
  try {
    await store.rollback(rid, 'operator rollback')
    message.success('回滾已發起')
    showRollback.value = false
    await store.fetchHistory(rid)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function loadDryRun() {
  dryRunLoading.value = true
  try {
    dryRunResult.value = await store.dryRun(rid)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '無法載入 Dry Run')
  } finally {
    dryRunLoading.value = false
  }
}

onMounted(async () => {
  await store.fetchOne(rid)
  await Promise.all([
    store.fetchShards(rid),
    store.fetchHistory(rid),
  ])
})
</script>

<template>
  <div class="detail-page">
    <PageHeader
      :title="store.current?.release_id ?? '載入中...'"
      subtitle="發布詳情"
    >
      <template #actions>
        <NSpace>
          <NButton v-if="canDryRun" :loading="dryRunLoading" @click="loadDryRun">
            Dry Run
          </NButton>
          <NButton v-if="canPause" type="warning" :loading="actionLoading" @click="handlePause">
            暫停
          </NButton>
          <NButton v-if="canResume" type="primary" :loading="actionLoading" @click="handleResume">
            恢復
          </NButton>
          <NButton v-if="canRollback" type="warning" :loading="actionLoading" @click="showRollback = true">
            回滾
          </NButton>
          <NButton v-if="canCancel" type="error" :loading="actionLoading" @click="showCancel = true">
            取消發布
          </NButton>
        </NSpace>
      </template>
      <template #hint>
        <PageHint storage-key="release-detail" title="發布操作說明">
          <strong>Dry Run</strong>：預覽此次部署會變更哪些檔案（added / modified / removed），不實際執行。<br>
          <strong>暫停 / 恢復</strong>：中斷或繼續正在執行（executing）的發布。<br>
          <strong>回滾</strong>：在 failed 或 paused 狀態下，將所有節點恢復至上一個成功版本的 artifact。<br>
          <strong>取消發布</strong>：終止尚未完成的發布，操作不可還原。
        </PageHint>
      </template>
    </PageHeader>

    <div v-if="store.current" class="detail-page__body">
      <div class="detail-page__sidebar">
        <NDescriptions bordered :column="1" label-placement="left">
          <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
          <NDescriptionsItem label="狀態">
            <StatusTag :status="store.current.status" />
          </NDescriptionsItem>
          <NDescriptionsItem label="類型">{{ store.current.release_type }}</NDescriptionsItem>
          <NDescriptionsItem label="觸發來源">{{ store.current.trigger_source }}</NDescriptionsItem>
          <NDescriptionsItem label="域名數">{{ store.current.total_domains ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="Shard 數">{{ store.current.total_shards ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="成功 / 失敗">
            {{ store.current.success_count }} / {{ store.current.failure_count }}
          </NDescriptionsItem>
          <NDescriptionsItem label="建立時間">
            {{ new Date(store.current.created_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
          <NDescriptionsItem v-if="store.current.started_at" label="開始時間">
            {{ new Date(store.current.started_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
          <NDescriptionsItem v-if="store.current.ended_at" label="結束時間">
            {{ new Date(store.current.ended_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
          <NDescriptionsItem label="說明">
            {{ store.current.description || '-' }}
          </NDescriptionsItem>
        </NDescriptions>
      </div>

      <div class="detail-page__main">
        <NTabs type="line" animated>
          <NTabPane name="shards" :tab="`Shards (${store.shards.length})`">
            <AppTable :columns="shardCols" :data="store.shards"
              :row-key="(r) => String(r.id)" />
          </NTabPane>

          <NTabPane name="history" :tab="`狀態歷史 (${store.history.length})`">
            <NTimeline class="history-timeline">
              <NTimelineItem
                v-for="entry in store.history"
                :key="entry.id"
                :title="`${entry.from_state ?? '—'} → ${entry.to_state}`"
                :time="new Date(entry.created_at).toLocaleString('zh-TW')"
                :content="entry.reason || undefined"
              />
            </NTimeline>
          </NTabPane>

          <NTabPane v-if="dryRunResult" name="dryrun" tab="Dry Run 預覽">
            <div class="dryrun-summary">
              <NSpace>
                <NTag type="success">+{{ dryRunResult.summary.added }} 新增</NTag>
                <NTag type="error">-{{ dryRunResult.summary.removed }} 移除</NTag>
                <NTag type="warning">{{ dryRunResult.summary.modified }} 修改</NTag>
                <NTag>{{ dryRunResult.summary.unchanged }} 不變</NTag>
              </NSpace>
            </div>
            <div v-for="file in dryRunResult.files" :key="file.path" class="dryrun-file">
              <div class="dryrun-file__header">
                <NTag
                  :type="file.change === 'added' ? 'success' : file.change === 'removed' ? 'error' : file.change === 'modified' ? 'warning' : 'default'"
                  size="small"
                >{{ file.change }}</NTag>
                <NText class="dryrun-file__path">{{ file.path }}</NText>
              </div>
              <pre v-if="file.diff" class="dryrun-diff">{{ file.diff }}</pre>
            </div>
          </NTabPane>
        </NTabs>
      </div>
    </div>

    <ConfirmModal
      v-model:show="showCancel"
      title="取消發布"
      content="確定要取消此發布嗎？此操作無法還原。"
      type="danger"
      :loading="actionLoading"
      confirm-text="確認取消"
      @confirm="handleCancel"
    />

    <ConfirmModal
      v-model:show="showRollback"
      title="回滾發布"
      :content="`確定要將 ${store.current?.release_id ?? ''} 回滾至上一個成功版本嗎？`"
      type="warning"
      :loading="actionLoading"
      confirm-text="確認回滾"
      @confirm="handleRollback"
    />
  </div>
</template>

<style scoped>
.detail-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.detail-page__body {
  display: flex;
  flex: 1;
  overflow: hidden;
  gap: 0;
}
.detail-page__sidebar {
  width: 320px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  padding: var(--space-6);
  overflow-y: auto;
}
.detail-page__main {
  flex: 1;
  padding: var(--space-6);
  overflow-y: auto;
}
.history-timeline {
  padding-left: var(--space-4);
}
.dryrun-summary {
  padding: var(--space-4) 0 var(--space-6);
}
.dryrun-file {
  border: 1px solid var(--border);
  border-radius: 4px;
  margin-bottom: var(--space-4);
  overflow: hidden;
}
.dryrun-file__header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-4);
  background: var(--color-bg-subtle, #f5f5f5);
  border-bottom: 1px solid var(--border);
}
.dryrun-file__path {
  font-family: monospace;
  font-size: 13px;
}
.dryrun-diff {
  margin: 0;
  padding: var(--space-4);
  font-family: monospace;
  font-size: 12px;
  line-height: 1.5;
  overflow-x: auto;
  white-space: pre;
  background: var(--color-bg-code, #fafafa);
}
</style>
