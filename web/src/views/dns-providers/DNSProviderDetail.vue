<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { SelectOption, DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput, NSelect,
  NDescriptions, NDescriptionsItem, NSpin, NDataTable, NTag,
  NDivider, NAlert, NText, useMessage,
} from 'naive-ui'
import { PageHeader, StatusTag } from '@/components'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import { domainApi } from '@/api/domain'
import type { UpdateDNSProviderRequest } from '@/types/dnsprovider'
import type { DomainResponse } from '@/types/domain'

const route   = useRoute()
const router  = useRouter()
const store   = useDNSProviderStore()
const message = useMessage()

const providerId = Number(route.params.id)

// ── Edit modal ────────────────────────────────────────────────────────────────
const showEdit  = ref(false)
const saving    = ref(false)
const typeOptions = ref<SelectOption[]>([])
const editForm  = ref<UpdateDNSProviderRequest>({
  name:          '',
  provider_type: 'cloudflare',
  notes:         null,
})

function openEdit() {
  if (!store.current) return
  editForm.value = {
    name:          store.current.name,
    provider_type: store.current.provider_type,
    notes:         store.current.notes,
  }
  showEdit.value = true
}

async function submitEdit() {
  if (!editForm.value.name.trim()) {
    message.warning('名稱必填')
    return
  }
  saving.value = true
  try {
    await store.update(providerId, editForm.value)
    message.success('已更新')
    showEdit.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '更新失敗')
  } finally {
    saving.value = false
  }
}

// ── Domains using this provider ───────────────────────────────────────────────
const domainsLoading = ref(false)
const domains        = ref<DomainResponse[]>([])
const domainsTotal   = ref(0)

const lifecycleTagType = (state: string): 'success' | 'warning' | 'error' | 'default' | 'info' => {
  switch (state) {
    case 'active':      return 'success'
    case 'provisioned': return 'info'
    case 'approved':    return 'warning'
    case 'disabled':    return 'warning'
    case 'retired':     return 'error'
    default:            return 'default'
  }
}

const domainColumns: DataTableColumns<DomainResponse> = [
  {
    title: '域名', key: 'fqdn', sorter: 'default',
    render: (row): VNodeChild =>
      h('a', {
        style: 'cursor:pointer;color:var(--primary-color);font-family:var(--font-mono);font-size:13px',
        onClick: () => router.push(`/domains/${row.id}`),
      }, row.fqdn),
  },
  {
    title: '生命週期', key: 'lifecycle_state', width: 110,
    render: (row): VNodeChild =>
      h(NTag, {
        type: lifecycleTagType(row.lifecycle_state),
        size: 'small',
        bordered: false,
      }, { default: () => row.lifecycle_state }),
  },
  {
    title: '專案', key: 'project_id', width: 80,
    render: (row): VNodeChild => h('span', `#${row.project_id}`),
  },
  {
    title: '操作', key: '_ops', width: 140, fixed: 'right',
    render: (row): VNodeChild =>
      h('div', { style: 'display:flex;gap:6px' }, [
        h(NButton, {
          size: 'small',
          quaternary: true,
          onClick: () => router.push({ path: `/domains/${row.id}`, query: { tab: 'provider-records' } }),
        }, { default: () => '管理 DNS 記錄' }),
      ]),
  },
]

async function fetchDomains() {
  domainsLoading.value = true
  try {
    const res = await domainApi.list({ dns_provider_id: providerId, limit: 200 }) as any
    domains.value      = res.data?.items ?? []
    domainsTotal.value = res.data?.total ?? domains.value.length
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '載入域名失敗')
  } finally {
    domainsLoading.value = false
  }
}

onMounted(async () => {
  await Promise.all([store.fetchOne(providerId), store.fetchTypes()])
  typeOptions.value = store.supportedTypes.map(t => ({ label: t, value: t }))
  await fetchDomains()
})
</script>

<template>
  <div>
    <PageHeader :title="store.current?.name ?? '載入中…'" @back="router.back()">
      <template #actions>
        <NButton @click="openEdit">編輯</NButton>
      </template>
    </PageHeader>

    <NSpin :show="store.loading">
      <NDescriptions v-if="store.current" bordered :column="2" style="margin-bottom:24px">
        <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
        <NDescriptionsItem label="Provider 類型">{{ store.current.provider_type }}</NDescriptionsItem>
        <NDescriptionsItem label="建立時間">{{ new Date(store.current.created_at).toLocaleString('zh-TW') }}</NDescriptionsItem>
        <NDescriptionsItem label="更新時間">{{ new Date(store.current.updated_at).toLocaleString('zh-TW') }}</NDescriptionsItem>
        <NDescriptionsItem label="備注" :span="2">{{ store.current.notes ?? '-' }}</NDescriptionsItem>
      </NDescriptions>

      <!-- Domains using this provider -->
      <NDivider title-placement="left">
        <span style="font-size:13px;font-weight:500">
          使用此 Provider 的域名（{{ domainsTotal }}）
        </span>
      </NDivider>

      <NAlert
        v-if="!domainsLoading && domains.length === 0"
        type="info"
        style="margin-bottom:12px"
      >
        目前沒有域名使用此 DNS Provider。
      </NAlert>

      <NSpin v-else :show="domainsLoading">
        <NDataTable
          v-if="domains.length > 0"
          :columns="domainColumns"
          :data="domains"
          :row-key="(r: DomainResponse) => r.id"
          size="small"
          :max-height="420"
          striped
          style="margin-bottom:12px"
        />
      </NSpin>

      <div v-if="domains.length > 0" style="display:flex;justify-content:flex-end;margin-top:4px">
        <NText depth="3" style="font-size:12px">
          共 {{ domainsTotal }} 個域名使用此 DNS Provider
        </NText>
      </div>
    </NSpin>

    <!-- Edit modal -->
    <NModal
      v-model:show="showEdit"
      preset="card"
      title="編輯 DNS Provider"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="editForm">
        <NFormItem label="名稱" required>
          <NInput v-model:value="editForm.name" />
        </NFormItem>
        <NFormItem label="Provider 類型" required>
          <NSelect v-model:value="(editForm as any).provider_type" :options="typeOptions" />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="(editForm as any).notes" type="textarea" :rows="2" clearable />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">取消</NButton>
          <NButton type="primary" :loading="saving" @click="submitEdit">儲存</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>
