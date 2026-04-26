<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NModal, NCard, NForm, NFormItem, NInput, NSelect,
  NSpace, NTag, useMessage,
} from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { useCDNStore } from '@/stores/cdn'
import { CDN_PROVIDER_TYPES } from '@/api/cdn'
import type { CDNProviderResponse, CreateCDNProviderRequest } from '@/api/cdn'

const router  = useRouter()
const store   = useCDNStore()
const message = useMessage()

// ── Create modal ──────────────────────────────────────────────────────────────
const showCreate = ref(false)
const creating   = ref(false)
const form = ref<CreateCDNProviderRequest & { description: string }>({
  name:          '',
  provider_type: '',
  description:   '',
})

function openCreate() {
  form.value = { name: '', provider_type: '', description: '' }
  showCreate.value = true
}

async function handleCreate() {
  if (!form.value.name || !form.value.provider_type) {
    message.warning('請填寫名稱並選擇供應商類型')
    return
  }
  creating.value = true
  try {
    await store.create({
      name:         form.value.name,
      provider_type: form.value.provider_type,
      description:  form.value.description || null,
    })
    message.success('CDN 供應商建立成功')
    showCreate.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '建立失敗')
  } finally {
    creating.value = false
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const providerTypeLabel = computed(() => {
  const map: Record<string, string> = {}
  CDN_PROVIDER_TYPES.forEach(t => { map[t.value] = t.label })
  return map
})

const columns: DataTableColumns<CDNProviderResponse> = [
  { title: '名稱',        key: 'name',          minWidth: 150 },
  {
    title: '類型',        key: 'provider_type',  width: 150,
    render: (row): VNodeChild => {
      const label = providerTypeLabel.value[row.provider_type] ?? row.provider_type
      return h(NTag, { size: 'small', type: 'info' }, { default: () => label })
    },
  },
  {
    title: '說明',        key: 'description',    ellipsis: { tooltip: true }, minWidth: 200,
    render: (row) => row.description ?? '-',
  },
  {
    title: '建立時間',    key: 'created_at',     width: 160,
    render: (row) => new Date(row.created_at).toLocaleDateString('zh-TW'),
  },
  {
    title: '操作',        key: 'actions',        width: 80,  fixed: 'right',
    render: (row): VNodeChild => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/cdn-providers/${row.id}`),
    }, { default: () => '管理' }),
  },
]

onMounted(() => store.fetchList())
</script>

<template>
  <div class="list-page">
    <PageHeader title="CDN 供應商管理" subtitle="管理 CDN/加速商帳號，與 Registrar、DNS Provider 並列">
      <template #actions>
        <NButton type="primary" @click="openCreate">新增供應商</NButton>
      </template>
    </PageHeader>

    <AppTable
      :columns="columns"
      :data="store.providers"
      :loading="store.loading"
      :row-key="(r: CDNProviderResponse) => r.id"
    />

    <!-- Create modal -->
    <NModal v-model:show="showCreate" :mask-closable="!creating">
      <NCard title="新增 CDN 供應商" :bordered="false" style="width: 480px">
        <NForm label-placement="left" label-width="100px">
          <NFormItem label="名稱" required>
            <NInput v-model:value="form.name" placeholder="例：自定義 CDN" />
          </NFormItem>
          <NFormItem label="供應商類型" required>
            <NSelect
              v-model:value="form.provider_type"
              :options="CDN_PROVIDER_TYPES"
              placeholder="選擇類型"
            />
          </NFormItem>
          <NFormItem label="說明">
            <NInput
              v-model:value="form.description"
              type="textarea"
              :rows="2"
              placeholder="選填"
            />
          </NFormItem>
        </NForm>
        <template #action>
          <NSpace justify="end">
            <NButton :disabled="creating" @click="showCreate = false">取消</NButton>
            <NButton type="primary" :loading="creating" @click="handleCreate">建立</NButton>
          </NSpace>
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
