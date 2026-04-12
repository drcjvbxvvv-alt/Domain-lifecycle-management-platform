<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import {
  NButton, NModal, NCard, NForm, NFormItem, NInput, NInputNumber,
  NSelect, useMessage,
} from 'naive-ui'
import { AppTable, PageHeader, StatusTag, PageHint } from '@/components'
import { useReleaseStore } from '@/stores/release'
import { useTemplateStore } from '@/stores/template'
import type { ReleaseResponse } from '@/types/release'

const route   = useRoute()
const router  = useRouter()
const store   = useReleaseStore()
const tmplStore = useTemplateStore()
const message = useMessage()

const projectId = route.params.id as string | undefined

const showCreate = ref(false)
const creating   = ref(false)
const form = ref({
  project_id: projectId ? Number(projectId) : (null as number | null),
  project_slug: '',
  template_version_id: null as number | null,
  release_type: 'html',
  description: '',
})

const releaseTypeOptions = [
  { label: 'HTML', value: 'html' },
  { label: 'Nginx', value: 'nginx' },
  { label: 'Full', value: 'full' },
]

const columns: DataTableColumns<ReleaseResponse> = [
  { title: 'Release ID', key: 'release_id', ellipsis: { tooltip: true }, width: 220 },
  { title: '類型',  key: 'release_type', width: 80 },
  { title: '狀態',  key: 'status', width: 140,
    render: (row) => h(StatusTag, { status: row.status }) },
  { title: '域名數', key: 'total_domains', width: 80,
    render: (row) => row.total_domains ?? '-' },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${row.project_id}/releases/${row.uuid}`),
    }, { default: () => '查看' }) },
]

function openCreate() {
  form.value = {
    project_id: projectId ? Number(projectId) : null,
    project_slug: '',
    template_version_id: null,
    release_type: 'html',
    description: '',
  }
  showCreate.value = true
  if (projectId) {
    tmplStore.fetchByProject(projectId)
  }
}

async function handleCreate() {
  if (!form.value.project_id || !form.value.template_version_id || !form.value.project_slug) {
    message.warning('請填寫必填欄位')
    return
  }
  creating.value = true
  try {
    await store.create({
      project_id: form.value.project_id,
      project_slug: form.value.project_slug,
      template_version_id: form.value.template_version_id,
      release_type: form.value.release_type,
      description: form.value.description || undefined,
    })
    message.success('發布建立成功')
    showCreate.value = false
    if (projectId) {
      await store.fetchByProject(Number(projectId))
    }
  } catch (e: any) {
    message.error(e?.response?.data?.message || '建立失敗')
  } finally {
    creating.value = false
  }
}

onMounted(() => {
  if (projectId) {
    store.fetchByProject(Number(projectId))
  }
})
</script>

<template>
  <div class="list-page">
    <PageHeader title="發布管理" :subtitle="projectId ? `專案 #${projectId}` : ''">
      <template #actions>
        <NButton type="primary" @click="openCreate" :disabled="!projectId">新增發布</NButton>
      </template>
      <template #hint>
        <PageHint storage-key="release-list" title="發布管理說明">
          建立發布前請確認：此專案有 <strong>active</strong> 域名；目標範本版本已發布。<br>
          <strong>Template Version</strong>：填入範本詳情頁版本列表的 ID 數字。<strong>Project Slug</strong>：與專案 slug 保持一致。<br>
          建立後系統自動排程 planning → artifact 建置 → ready，可在詳情頁執行 Dry Run / 暫停 / 回滾等操作。
        </PageHint>
      </template>
    </PageHeader>
    <AppTable
      :columns="columns"
      :data="store.releases"
      :loading="store.loading"
      :row-key="(r) => r.uuid"
    />

    <NModal v-model:show="showCreate" :mask-closable="!creating">
      <NCard title="新增發布" :bordered="false" style="width: 520px">
        <NForm label-placement="left" label-width="120">
          <NFormItem label="專案 ID" required>
            <NInputNumber v-model:value="form.project_id" :min="1" style="width: 100%" :disabled="!!projectId" />
          </NFormItem>
          <NFormItem label="Project Slug" required>
            <NInput v-model:value="form.project_slug" placeholder="my-project" />
          </NFormItem>
          <NFormItem label="Template Version" required>
            <NInputNumber v-model:value="form.template_version_id" :min="1" style="width: 100%" placeholder="Template Version ID" />
          </NFormItem>
          <NFormItem label="發布類型">
            <NSelect v-model:value="form.release_type" :options="releaseTypeOptions" />
          </NFormItem>
          <NFormItem label="說明">
            <NInput v-model:value="form.description" type="textarea" placeholder="發布說明（選填）" :rows="3" />
          </NFormItem>
        </NForm>
        <template #action>
          <div style="display: flex; justify-content: flex-end; gap: 8px">
            <NButton @click="showCreate = false" :disabled="creating">取消</NButton>
            <NButton type="primary" :loading="creating" @click="handleCreate">建立發布</NButton>
          </div>
        </template>
      </NCard>
    </NModal>
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
