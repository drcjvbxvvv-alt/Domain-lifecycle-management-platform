<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane, NButton, NDescriptions, NDescriptionsItem } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { AppTable, PageHeader, StatusTag, PageHint } from '@/components'
import { useProjectStore } from '@/stores/project'
import { useDomainStore } from '@/stores/domain'
import { useReleaseStore } from '@/stores/release'
import { useTemplateStore } from '@/stores/template'
import type { DomainResponse } from '@/types/domain'
import type { ReleaseResponse } from '@/types/release'
import type { TemplateResponse } from '@/types/template'

const route   = useRoute()
const router  = useRouter()
const id      = route.params.id as string

const projectStore  = useProjectStore()
const domainStore   = useDomainStore()
const releaseStore  = useReleaseStore()
const templateStore = useTemplateStore()

const domainCols: DataTableColumns<DomainResponse> = [
  { title: '域名',  key: 'fqdn', ellipsis: { tooltip: true } },
  { title: '狀態',   key: 'lifecycle_state', width: 120,
    render: (row) => h(StatusTag, { status: row.lifecycle_state }) },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/domains/${row.uuid}`),
    }, { default: () => '查看' }) },
]

const releaseCols: DataTableColumns<ReleaseResponse> = [
  { title: 'Release ID', key: 'release_id', ellipsis: { tooltip: true }, width: 200 },
  { title: '類型',  key: 'release_type', width: 80 },
  { title: '狀態',  key: 'status', width: 120,
    render: (row) => h(StatusTag, { status: row.status }) },
  { title: '域名數', key: 'total_domains', width: 80 },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${id}/releases/${row.uuid}`),
    }, { default: () => '查看' }) },
]

const templateCols: DataTableColumns<TemplateResponse> = [
  { title: '名稱', key: 'name', ellipsis: { tooltip: true } },
  { title: '說明', key: 'description', ellipsis: { tooltip: true } },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${id}/templates/${row.id}`),
    }, { default: () => '查看' }) },
]

onMounted(async () => {
  await projectStore.fetchOne(id)
  await Promise.all([
    domainStore.fetchList({ project_id: Number(id) }),
    releaseStore.fetchByProject(Number(id)),
    templateStore.fetchByProject(id),
  ])
})
</script>

<template>
  <div class="detail-page">
    <PageHeader
      :title="projectStore.current?.name ?? '載入中...'"
      :subtitle="`slug: ${projectStore.current?.slug ?? ''}`"
    >
      <template #hint>
        <PageHint storage-key="project-detail" title="專案詳情說明">
          <strong>域名 tab</strong>：此專案下所有域名，點擊「查看」可進行生命週期操作。<br>
          <strong>範本 tab</strong>：可用於發布的 HTML / Nginx conf 範本，點擊查看各版本及 Version ID。<br>
          <strong>發布 tab</strong>：歷史發布記錄，點擊「查看」進入發布操作頁（暫停 / 回滾 / Dry Run）。
        </PageHint>
      </template>
    </PageHeader>

    <div v-if="projectStore.current" class="detail-page__body">
      <div class="detail-page__sidebar">
        <NDescriptions bordered :column="1" label-placement="left">
          <NDescriptionsItem label="UUID">{{ projectStore.current.uuid }}</NDescriptionsItem>
          <NDescriptionsItem label="Slug">{{ projectStore.current.slug }}</NDescriptionsItem>
          <NDescriptionsItem label="建立時間">
            {{ new Date(projectStore.current.created_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
          <NDescriptionsItem label="說明">{{ projectStore.current.description || '-' }}</NDescriptionsItem>
        </NDescriptions>
      </div>

      <div class="detail-page__main">
        <NTabs type="line" animated>
          <NTabPane name="domains" :tab="`域名 (${domainStore.total})`">
            <AppTable :columns="domainCols" :data="domainStore.domains" :loading="domainStore.loading"
              :row-key="(r) => r.uuid" />
          </NTabPane>
          <NTabPane name="templates" :tab="`範本 (${templateStore.total})`">
            <AppTable :columns="templateCols" :data="templateStore.templates" :loading="templateStore.loading"
              :row-key="(r) => String(r.id)" />
          </NTabPane>
          <NTabPane name="releases" :tab="`發布 (${releaseStore.total})`">
            <AppTable :columns="releaseCols" :data="releaseStore.releases" :loading="releaseStore.loading"
              :row-key="(r) => r.uuid" />
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
</style>
