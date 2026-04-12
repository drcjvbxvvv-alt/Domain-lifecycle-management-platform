<script setup lang="ts">
import { onMounted, onUnmounted, computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem,
  NAlert, NTimeline, NTimelineItem, NButton, NSpace, useMessage,
} from 'naive-ui'
import { PageHeader, StatusTag, PageHint } from '@/components'
import { useAgentStore } from '@/stores/agent'

const route   = useRoute()
const store   = useAgentStore()
const message = useMessage()
const id      = route.params.id as string

const actionLoading = ref(false)

const canDrain   = computed(() => ['online', 'busy', 'idle'].includes(store.current?.status ?? ''))
const canDisable = computed(() => !['disabled', 'draining'].includes(store.current?.status ?? ''))
const canEnable  = computed(() => ['disabled', 'error', 'draining'].includes(store.current?.status ?? ''))

async function doAction(fn: (id: string) => Promise<void>, label: string) {
  actionLoading.value = true
  try {
    await fn(id)
    message.success(`操作成功：${label}`)
    await store.fetchHistory(id)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

let timer: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  await store.fetchOne(id)
  await store.fetchHistory(id)
  timer = setInterval(() => store.fetchOne(id), 10_000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="detail-page">
    <PageHeader
      :title="store.current?.hostname ?? '載入中...'"
      :subtitle="store.current?.agent_id ?? 'Agent 詳情'"
    >
      <template #actions>
        <NSpace v-if="store.current">
          <NButton
            v-if="canDrain"
            type="warning" ghost
            :loading="actionLoading"
            @click="doAction(store.drain, '排空')"
          >排空</NButton>
          <NButton
            v-if="canDisable"
            type="error" ghost
            :loading="actionLoading"
            @click="doAction(store.disable, '停用')"
          >停用</NButton>
          <NButton
            v-if="canEnable"
            type="primary" ghost
            :loading="actionLoading"
            @click="doAction(store.enable, '啟用')"
          >啟用</NButton>
        </NSpace>
      </template>
      <template #hint>
        <PageHint storage-key="agent-detail" title="Agent 詳情說明">
          最後心跳時間超過 30 秒表示連線可能異常；頂部若有紅色提示框代表 Agent 回報了錯誤。<br>
          <strong>error 狀態</strong>：需排查 Agent 主機問題，確認修復後點擊「啟用」清除狀態。<br>
          狀態歷史記錄所有轉換及原因，可用於問題追蹤。頁面每 10 秒自動更新狀態。
        </PageHint>
      </template>
    </PageHeader>

    <div v-if="store.current" class="detail-page__body">
      <div class="detail-page__sidebar">
        <NAlert
          v-if="store.current.last_error"
          type="error"
          :title="store.current.last_error"
          class="error-alert"
        />

        <NDescriptions bordered :column="1" label-placement="left">
          <NDescriptionsItem label="Agent ID">{{ store.current.agent_id }}</NDescriptionsItem>
          <NDescriptionsItem label="狀態">
            <StatusTag :status="store.current.status" />
          </NDescriptionsItem>
          <NDescriptionsItem label="版本">{{ store.current.agent_version ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="Hostname">{{ store.current.hostname }}</NDescriptionsItem>
          <NDescriptionsItem label="IP">{{ store.current.ip ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="Region">{{ store.current.region ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="Datacenter">{{ store.current.datacenter ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="最後心跳">
            {{ store.current.last_seen_at
              ? new Date(store.current.last_seen_at).toLocaleString('zh-TW')
              : '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="建立時間">
            {{ new Date(store.current.created_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
        </NDescriptions>
      </div>

      <div class="detail-page__main">
        <NTabs type="line" animated>
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
        </NTabs>
      </div>
    </div>
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
.error-alert {
  margin-bottom: var(--space-4);
}
.history-timeline {
  padding-left: var(--space-4);
}
</style>
