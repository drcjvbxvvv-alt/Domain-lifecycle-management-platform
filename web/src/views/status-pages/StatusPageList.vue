<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NTag, NModal, NForm, NFormItem,
  NInput, NSwitch, NPopconfirm, useMessage,
} from 'naive-ui'
import { useRouter } from 'vue-router'
import { AppTable, PageHeader } from '@/components'
import { statusPageApi } from '@/api/statuspage'
import type { StatusPageResponse, CreateStatusPageRequest } from '@/types/statuspage'

const message = useMessage()
const router  = useRouter()

// ── State ──────────────────────────────────────────────────────────────────────

const loading   = ref(false)
const pages     = ref<StatusPageResponse[]>([])
const showModal = ref(false)
const saving    = ref(false)

const form = ref<{
  slug: string
  title: string
  description: string
  published: boolean
  password: string
  auto_refresh_seconds: number
}>({
  slug: '', title: '', description: '', published: true,
  password: '', auto_refresh_seconds: 60,
})

// ── Data ───────────────────────────────────────────────────────────────────────

async function fetchList() {
  loading.value = true
  try {
    const res = await statusPageApi.list() as any
    pages.value = res?.data?.items ?? []
  } catch {
    message.error('載入狀態頁失敗')
  } finally {
    loading.value = false
  }
}

// ── Actions ────────────────────────────────────────────────────────────────────

function openCreate() {
  form.value = { slug: '', title: '', description: '', published: true, password: '', auto_refresh_seconds: 60 }
  showModal.value = true
}

async function save() {
  if (!form.value.slug || !form.value.title) {
    message.warning('請輸入 Slug 和標題')
    return
  }
  saving.value = true
  try {
    const payload: CreateStatusPageRequest = {
      slug:               form.value.slug,
      title:              form.value.title,
      description:        form.value.description || undefined,
      published:          form.value.published,
      password:           form.value.password || undefined,
      auto_refresh_seconds: form.value.auto_refresh_seconds,
    }
    await statusPageApi.create(payload)
    message.success('建立成功')
    showModal.value = false
    await fetchList()
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '儲存失敗')
  } finally {
    saving.value = false
  }
}

async function remove(id: number) {
  try {
    await statusPageApi.delete(id)
    message.success('已刪除')
    pages.value = pages.value.filter(p => p.id !== id)
  } catch {
    message.error('刪除失敗')
  }
}

// ── Columns ────────────────────────────────────────────────────────────────────

const columns: DataTableColumns<StatusPageResponse> = [
  {
    title: 'Slug', key: 'slug',
    render: (row) => row.slug,
  },
  {
    title: '標題', key: 'title', ellipsis: { tooltip: true },
  },
  {
    title: '狀態', key: 'published', width: 90,
    render: (row): VNodeChild =>
      h(NTag, { type: row.published ? 'success' : 'default', size: 'small' },
        { default: () => row.published ? '公開' : '草稿' }),
  },
  {
    title: '密碼保護', key: 'has_password', width: 90,
    render: (row): VNodeChild =>
      h(NTag, { type: row.has_password ? 'warning' : 'default', size: 'small' },
        { default: () => row.has_password ? '有' : '無' }),
  },
  {
    title: '刷新間隔', key: 'auto_refresh_seconds', width: 100,
    render: (row) => `${row.auto_refresh_seconds}s`,
  },
  {
    title: '操作', key: 'actions', width: 180,
    render: (row): VNodeChild =>
      h(NSpace, { size: 'small' }, {
        default: () => [
          h(NButton, { size: 'small', onClick: () => router.push(`/status-pages/${row.id}`) }, { default: () => '管理' }),
          h(NButton, { size: 'small', type: 'info', onClick: () => router.push(`/status/${row.slug}`) }, { default: () => '預覽' }),
          h(NPopconfirm, { onPositiveClick: () => remove(row.id) }, {
            trigger: () => h(NButton, { size: 'small', type: 'error' }, { default: () => '刪除' }),
            default: () => '確定刪除此狀態頁？',
          }),
        ],
      }),
  },
]

onMounted(fetchList)
</script>

<template>
  <div>
    <PageHeader title="狀態頁管理">
      <template #actions>
        <NButton type="primary" @click="openCreate">新增狀態頁</NButton>
      </template>
    </PageHeader>

    <AppTable
      :columns="columns"
      :data="pages"
      :loading="loading"
      :row-key="(row) => row.id"
    />

    <NModal
      v-model:show="showModal"
      title="新增狀態頁"
      preset="card"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px">
        <NFormItem label="Slug" required>
          <NInput v-model:value="form.slug" placeholder="例: main" />
        </NFormItem>
        <NFormItem label="標題" required>
          <NInput v-model:value="form.title" placeholder="狀態頁名稱" />
        </NFormItem>
        <NFormItem label="說明">
          <NInput v-model:value="form.description" type="textarea" :rows="2" />
        </NFormItem>
        <NFormItem label="公開發布">
          <NSwitch v-model:value="form.published" />
        </NFormItem>
        <NFormItem label="密碼保護">
          <NInput v-model:value="form.password" type="password" placeholder="留空表示無密碼" show-password-on="click" />
        </NFormItem>
        <NFormItem label="刷新秒數">
          <NInput v-model:value="(form.auto_refresh_seconds as any)" style="width:100px" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showModal = false">取消</NButton>
          <NButton type="primary" :loading="saving" @click="save">建立</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>
