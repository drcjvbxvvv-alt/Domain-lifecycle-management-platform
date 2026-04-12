<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem, NSpin,
  NAlert, NTimeline, NTimelineItem,
} from 'naive-ui'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useAgentStore } from '@/stores/agent'

const route = useRoute()
const store = useAgentStore()
const id    = route.params.id as string

onMounted(async () => {
  await store.fetchOne(id)
  await store.fetchHistory(id)
})
</script>

<template>
  <NSpin :show="store.loading">
    <PageHeader
      :title="store.current?.hostname ?? ''"
      :subtitle="store.current?.agent_id ?? 'Agent 詳情'"
    />

    <NAlert
      v-if="store.current?.last_error"
      type="error"
      :title="store.current.last_error"
      style="margin-top: 16px;"
    />

    <NDescriptions v-if="store.current" bordered :column="3" style="margin-top: 16px;">
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

    <NTabs style="margin-top: 24px;" type="line" animated>
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
