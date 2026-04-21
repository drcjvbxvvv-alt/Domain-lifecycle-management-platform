<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput,
  NPopconfirm, useMessage,
} from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { useRegistrarStore } from '@/stores/registrar'
import type { RegistrarResponse, CreateRegistrarRequest } from '@/types/registrar'

const router  = useRouter()
const store   = useRegistrarStore()
const message = useMessage()

// ── Create modal ─────────────────────────────────────────────────────────────
const showCreate = ref(false)
const creating   = ref(false)
const form = ref<CreateRegistrarRequest>({
  name:     '',
  url:      null,
  api_type: null,
  notes:    null,
})

function openCreate() {
  form.value = { name: '', url: null, api_type: null, notes: null }
  showCreate.value = true
}

async function submitCreate() {
  if (!form.value.name.trim()) {
    message.warning('請輸入 Registrar 名稱')
    return
  }
  creating.value = true
  try {
    await store.create(form.value)
    message.success('Registrar 建立成功')
    showCreate.value = false
  } catch (e: any) {
    const msg = e?.response?.data?.message ?? '建立失敗'
    message.error(msg)
  } finally {
    creating.value = false
  }
}

// ── Delete ────────────────────────────────────────────────────────────────────
async function deleteRegistrar(id: number) {
  try {
    await store.remove(id)
    message.success('已刪除')
  } catch (e: any) {
    const msg = e?.response?.data?.message ?? '刪除失敗'
    message.error(msg)
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const columns: DataTableColumns<RegistrarResponse> = [
  { title: '名稱', key: 'name', ellipsis: { tooltip: true } },
  { title: 'API 類型', key: 'api_type', width: 140,
    render: (row) => row.api_type ?? '-' },
  { title: '網址', key: 'url', ellipsis: { tooltip: true },
    render: (row) => row.url ?? '-' },
  { title: '備注', key: 'notes', ellipsis: { tooltip: true },
    render: (row) => row.notes ?? '-' },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  {
    title: '操作', key: 'actions', width: 200, fixed: 'right',
    render: (row): VNodeChild => h(NSpace, { size: 'small' }, {
      default: () => [
        h(NButton, {
          size: 'small', type: 'primary', ghost: true,
          onClick: () => router.push({ name: 'RegistrarDetail', params: { id: row.id } }),
        }, { default: () => '詳情' }),
        h(NPopconfirm, {
          onPositiveClick: () => deleteRegistrar(row.id),
        }, {
          trigger: () => h(NButton, {
            size: 'small', type: 'error', ghost: true,
          }, { default: () => '刪除' }),
          default: () => '確定刪除此 Registrar？若有帳號或域名依附將無法刪除。',
        }),
      ],
    }),
  },
]

onMounted(() => store.fetchList())
</script>

<template>
  <div>
    <PageHeader title="Registrar 管理">
      <template #extra>
        <NButton type="primary" @click="openCreate">新增 Registrar</NButton>
      </template>
    </PageHeader>

    <AppTable
      :columns="columns"
      :data="store.registrars"
      :loading="store.loading"
      :row-key="(row) => row.id"
    />

    <!-- Create modal -->
    <NModal
      v-model:show="showCreate"
      preset="card"
      title="新增 Registrar"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="form">
        <NFormItem label="名稱" required>
          <NInput v-model:value="form.name" placeholder="e.g. Namecheap" />
        </NFormItem>
        <NFormItem label="API 類型">
          <NInput v-model:value="(form as any).api_type" placeholder="e.g. namecheap" clearable />
        </NFormItem>
        <NFormItem label="網址">
          <NInput v-model:value="(form as any).url" placeholder="https://registrar.example.com" clearable />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="(form as any).notes" type="textarea" :rows="2" clearable />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="creating" @click="submitCreate">建立</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>
