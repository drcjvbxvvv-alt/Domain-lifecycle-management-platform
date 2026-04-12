<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem,
  NTimeline, NTimelineItem, NButton, NSpace, useMessage,
} from 'naive-ui'
import { PageHeader, StatusTag, ConfirmModal, PageHint } from '@/components'
import { useDomainStore } from '@/stores/domain'
import { domainApi } from '@/api/domain'
import type { DomainLifecycleHistoryEntry } from '@/types/domain'
import type { DomainLifecycleState, ApiResponse } from '@/types/common'

const route   = useRoute()
const store   = useDomainStore()
const message = useMessage()
const id      = route.params.id as string
const history = ref<DomainLifecycleHistoryEntry[]>([])
const actionLoading = ref(false)
const showRetire    = ref(false)

// Valid transitions per current state
const validTransitions = {
  requested:   ['approved', 'retired'],
  approved:    ['provisioned', 'retired'],
  provisioned: ['active', 'disabled', 'retired'],
  active:      ['disabled', 'retired'],
  disabled:    ['active', 'retired'],
  retired:     [],
} as Record<string, DomainLifecycleState[]>

const nextStates = computed(() => {
  const s = store.current?.lifecycle_state
  if (!s) return []
  return (validTransitions[s] || []).filter(st => st !== 'retired')
})

const canRetire = computed(() => {
  const s = store.current?.lifecycle_state
  return s && s !== 'retired'
})

const transitionLabel: Record<string, string> = {
  approved: '核准',
  provisioned: '佈建完成',
  active: '啟用',
  disabled: '停用',
}

async function handleTransition(to: DomainLifecycleState) {
  actionLoading.value = true
  try {
    await store.transition(id, { to, reason: `operator: ${to}` })
    message.success(`已轉換至 ${to}`)
    await refreshHistory()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function handleRetire() {
  actionLoading.value = true
  try {
    await store.transition(id, { to: 'retired', reason: 'operator retired' })
    message.success('已退役')
    showRetire.value = false
    await refreshHistory()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function refreshHistory() {
  const res = await domainApi.history(id) as unknown as ApiResponse<DomainLifecycleHistoryEntry[]>
  history.value = res.data ?? []
}

onMounted(async () => {
  await store.fetchOne(id)
  await refreshHistory()
})
</script>

<template>
  <div class="detail-page">
    <PageHeader
      :title="store.current?.fqdn ?? '載入中...'"
      subtitle="域名詳情"
    >
      <template #actions>
        <NSpace v-if="store.current">
          <NButton
            v-for="state in nextStates"
            :key="state"
            type="primary"
            :loading="actionLoading"
            @click="handleTransition(state)"
          >
            {{ transitionLabel[state] || state }}
          </NButton>
          <NButton
            v-if="canRetire"
            type="error"
            :loading="actionLoading"
            @click="showRetire = true"
          >
            退役
          </NButton>
        </NSpace>
      </template>
      <template #hint>
        <PageHint storage-key="domain-detail" title="域名操作說明">
          頂部按鈕依當前狀態動態顯示可執行的操作。<br>
          <strong>核准</strong>：approved（自動觸發 DNS 佈建）→ <strong>佈建完成</strong>：provisioned → <strong>啟用</strong>：active（可加入發布）<br>
          <strong>停用</strong>：暫停此域名，可重新啟用；<strong>退役</strong>：永久終止，無法還原。
        </PageHint>
      </template>
    </PageHeader>

    <div v-if="store.current" class="detail-page__body">
      <div class="detail-page__sidebar">
        <NDescriptions bordered :column="1" label-placement="left">
          <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
          <NDescriptionsItem label="狀態">
            <StatusTag :status="store.current.lifecycle_state" />
          </NDescriptionsItem>
          <NDescriptionsItem label="專案 ID">{{ store.current.project_id }}</NDescriptionsItem>
          <NDescriptionsItem label="DNS Provider">{{ store.current.dns_provider || '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="DNS Zone">{{ store.current.dns_zone || '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="建立時間">
            {{ new Date(store.current.created_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
        </NDescriptions>
      </div>

      <div class="detail-page__main">
        <NTabs type="line" animated>
          <NTabPane name="history" :tab="`狀態歷史 (${history.length})`">
            <NTimeline class="history-timeline">
              <NTimelineItem
                v-for="entry in history"
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

    <ConfirmModal
      v-model:show="showRetire"
      title="退役域名"
      :content="`確定要退役 ${store.current?.fqdn ?? ''} 嗎？此操作無法還原。`"
      type="danger"
      :loading="actionLoading"
      confirm-text="確認退役"
      @confirm="handleRetire"
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
</style>
