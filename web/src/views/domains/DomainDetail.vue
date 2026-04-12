<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { NTabs, NTabPane, NDescriptions, NDescriptionsItem, NSpin, NTimeline, NTimelineItem } from 'naive-ui'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useDomainStore } from '@/stores/domain'
import { domainApi } from '@/api/domain'
import { ref } from 'vue'
import type { DomainLifecycleHistoryEntry } from '@/types/domain'

const route   = useRoute()
const store   = useDomainStore()
const id      = route.params.id as string
const history = ref<DomainLifecycleHistoryEntry[]>([])

onMounted(async () => {
  await store.fetchOne(id)
  const res = await domainApi.history(id) as any
  history.value = res.data?.items ?? res.items ?? []
})
</script>

<template>
  <NSpin :show="store.loading">
    <PageHeader
      :title="store.current?.fqdn ?? ''"
      subtitle="域名詳情"
    />

    <NDescriptions v-if="store.current" bordered :column="3" style="margin-top: 16px;">
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

    <NTabs style="margin-top: 24px;" type="line" animated>
      <NTabPane name="history" :tab="`狀態歷史 (${history.length})`">
        <NTimeline style="margin-top: 16px; padding-left: 16px;">
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
  </NSpin>
</template>
