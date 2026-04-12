<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem, NSpin,
  NTag, NTimeline, NTimelineItem,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import AppTable from '@/components/AppTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useReleaseStore } from '@/stores/release'
import type { ReleaseShardResponse } from '@/types/release'

const route = useRoute()
const store = useReleaseStore()
const rid   = route.params.rid as string

const shardCols: DataTableColumns<ReleaseShardResponse> = [
  { title: '#', key: 'shard_index', width: 60 },
  { title: 'Canary', key: 'is_canary', width: 80,
    render: (row) => row.is_canary
      ? h(NTag, { type: 'warning', size: 'small' }, { default: () => 'canary' })
      : '-' },
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

onMounted(async () => {
  await store.fetchOne(rid)
  await Promise.all([
    store.fetchShards(rid),
    store.fetchHistory(rid),
  ])
})
</script>

<template>
  <NSpin :show="store.loading">
    <PageHeader
      :title="store.current?.release_id ?? ''"
      subtitle="發布詳情"
    />

    <NDescriptions v-if="store.current" bordered :column="3" style="margin-top: 16px;">
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
      <NDescriptionsItem label="說明" :span="3">
        {{ store.current.description || '-' }}
      </NDescriptionsItem>
    </NDescriptions>

    <NTabs style="margin-top: 24px;" type="line" animated>
      <NTabPane name="shards" :tab="`Shards (${store.shards.length})`">
        <AppTable :columns="shardCols" :data="store.shards"
          :row-key="(r) => String(r.id)" />
      </NTabPane>

      <NTabPane name="history" :tab="`狀態歷史 (${store.history.length})`">
        <NTimeline style="margin-top: 16px; padding-left: 16px;">
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
  </NSpin>
</template>
