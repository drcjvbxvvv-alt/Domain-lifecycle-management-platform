<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRoute } from 'vue-router'
import { NTabs, NTabPane, NDescriptions, NDescriptionsItem, NSpin, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import AppTable from '@/components/AppTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useTemplateStore } from '@/stores/template'
import type { TemplateVersionResponse } from '@/types/template'

const route = useRoute()
const store = useTemplateStore()
const tid   = route.params.tid as string

const versionCols: DataTableColumns<TemplateVersionResponse> = [
  { title: '版本',      key: 'version_label', width: 120 },
  { title: '校驗碼',    key: 'checksum',       ellipsis: { tooltip: true }, width: 200 },
  { title: '已發布',    key: 'published_at',   width: 180,
    render: (row) => row.published_at
      ? new Date(row.published_at).toLocaleString('zh-TW')
      : h(NTag, { type: 'warning', size: 'small' }, { default: () => '草稿' }) },
  { title: '建立時間',  key: 'created_at',     width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
]

onMounted(async () => {
  await store.fetchOne(tid)
  if (store.current) await store.fetchVersions(store.current.id)
})
</script>

<template>
  <NSpin :show="store.loading">
    <PageHeader :title="store.current?.name ?? ''" subtitle="範本詳情" />

    <NDescriptions v-if="store.current" bordered :column="2" style="margin-top: 16px;">
      <NDescriptionsItem label="ID">{{ store.current.id }}</NDescriptionsItem>
      <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
      <NDescriptionsItem label="說明" :span="2">{{ store.current.description || '-' }}</NDescriptionsItem>
    </NDescriptions>

    <NTabs style="margin-top: 24px;" type="line" animated>
      <NTabPane name="versions" :tab="`版本列表 (${store.versions.length})`">
        <AppTable :columns="versionCols" :data="store.versions" :row-key="(r) => String(r.id)" />
      </NTabPane>
    </NTabs>
  </NSpin>
</template>
