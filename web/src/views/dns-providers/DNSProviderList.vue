<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns, SelectOption } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput, NSelect,
  NPopconfirm, useMessage,
} from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import type { DNSProviderResponse, CreateDNSProviderRequest } from '@/types/dnsprovider'

const router  = useRouter()
const store   = useDNSProviderStore()
const message = useMessage()

// ── Create modal ─────────────────────────────────────────────────────────────
const showCreate = ref(false)
const creating   = ref(false)
const form = ref<CreateDNSProviderRequest>({
  name:          '',
  provider_type: 'cloudflare',
  notes:         null,
})

const typeOptions = ref<SelectOption[]>([])

function openCreate() {
  form.value = { name: '', provider_type: 'cloudflare', notes: null }
  showCreate.value = true
}

async function submitCreate() {
  if (!form.value.name.trim()) {
    message.warning('請輸入 Provider 名稱')
    return
  }
  creating.value = true
  try {
    await store.create(form.value)
    message.success('DNS Provider 建立成功')
    showCreate.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '建立失敗')
  } finally {
    creating.value = false
  }
}

// ── Delete ────────────────────────────────────────────────────────────────────
async function deleteProvider(id: number) {
  try {
    await store.remove(id)
    message.success('已刪除')
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '刪除失敗')
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const columns: DataTableColumns<DNSProviderResponse> = [
  { title: '名稱', key: 'name', ellipsis: { tooltip: true } },
  { title: '類型', key: 'provider_type', width: 140 },
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
          onClick: () => router.push({ name: 'DNSProviderDetail', params: { id: row.id } }),
        }, { default: () => '詳情' }),
        h(NPopconfirm, {
          onPositiveClick: () => deleteProvider(row.id),
        }, {
          trigger: () => h(NButton, {
            size: 'small', type: 'error', ghost: true,
          }, { default: () => '刪除' }),
          default: () => '確定刪除此 DNS Provider？若有域名依附將無法刪除。',
        }),
      ],
    }),
  },
]

onMounted(async () => {
  await Promise.all([store.fetchList(), store.fetchTypes()])
  typeOptions.value = store.supportedTypes.map(t => ({ label: t, value: t }))
})
</script>

<template>
  <div>
    <PageHeader title="DNS Provider 管理">
      <template #extra>
        <NButton type="primary" @click="openCreate">新增 Provider</NButton>
      </template>
    </PageHeader>

    <AppTable
      :columns="columns"
      :data="store.providers"
      :loading="store.loading"
      :row-key="(row) => row.id"
    />

    <!-- Create modal -->
    <NModal
      v-model:show="showCreate"
      preset="card"
      title="新增 DNS Provider"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="form">
        <NFormItem label="名稱" required>
          <NInput v-model:value="form.name" placeholder="e.g. Cloudflare Production" />
        </NFormItem>
        <NFormItem label="Provider 類型" required>
          <NSelect
            v-model:value="(form as any).provider_type"
            :options="typeOptions"
            placeholder="選擇類型"
          />
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
