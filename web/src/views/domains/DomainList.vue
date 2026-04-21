<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns, SelectOption } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NModal, NCard, NForm, NFormItem, NInput, NSelect,
  NDatePicker, NSwitch, NInputNumber, NSpace, useMessage,
} from 'naive-ui'
import { AppTable, PageHeader, StatusTag, PageHint } from '@/components'
import { useDomainStore } from '@/stores/domain'
import { useProjectStore } from '@/stores/project'
import { useRegistrarStore } from '@/stores/registrar'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import { useTagStore } from '@/stores/tag'
import { tagApi } from '@/api/tag'
import type { DomainResponse, RegisterDomainRequest } from '@/types/domain'

const route    = useRoute()
const router   = useRouter()
const store    = useDomainStore()
const projects = useProjectStore()
const registrars   = useRegistrarStore()
const dnsProviders = useDNSProviderStore()
const tagStore     = useTagStore()
const message      = useMessage()

// Route context: accessed via /projects/:id/domains or standalone /domains
const routeProjectId = route.params.id as string | undefined
const selectedProjectId = ref<number | null>(routeProjectId ? Number(routeProjectId) : null)

// ── Filters ───────────────────────────────────────────────────────────────────
const filterRegistrar    = ref<number | null>(null)
const filterDNSProvider  = ref<number | null>(null)
const filterTLD          = ref<string>('')
const filterState        = ref<string | null>(null)
const filterExpiryStatus = ref<string | null>(null)
const filterTagId        = ref<number | null>(null)
const checkedRowKeys     = ref<number[]>([])

const projectOptions = computed(() =>
  projects.projects.map(p => ({ label: `${p.name} (${p.slug})`, value: p.id }))
)
const registrarOptions = computed(() =>
  registrars.registrars.map(r => ({ label: r.name, value: r.id }))
)
const dnsProviderOptions = computed(() =>
  dnsProviders.providers.map(p => ({ label: p.name, value: p.id }))
)
const tagOptions = computed(() =>
  tagStore.tags.map(t => ({ label: t.name, value: t.id }))
)

const stateOptions: SelectOption[] = [
  { label: 'requested',   value: 'requested'   },
  { label: 'approved',    value: 'approved'    },
  { label: 'provisioned', value: 'provisioned' },
  { label: 'active',      value: 'active'      },
  { label: 'disabled',    value: 'disabled'    },
  { label: 'retired',     value: 'retired'     },
]

const expiryStatusOptions: SelectOption[] = [
  { label: '90 天內到期', value: 'expiring_90d' },
  { label: '30 天內到期', value: 'expiring_30d' },
  { label: '7 天內到期',  value: 'expiring_7d'  },
  { label: '已過期',      value: 'expired'       },
  { label: 'Grace',       value: 'grace'         },
]

const selectedProjectName = computed(() => {
  if (routeProjectId) return projects.current?.name ?? `專案 #${routeProjectId}`
  if (selectedProjectId.value)
    return projects.projects.find(p => p.id === selectedProjectId.value)?.name ?? ''
  return ''
})

function loadDomains() {
  store.fetchList({
    project_id:      selectedProjectId.value ?? undefined,
    registrar_id:    filterRegistrar.value ?? undefined,
    dns_provider_id: filterDNSProvider.value ?? undefined,
    tld:             filterTLD.value || undefined,
    lifecycle_state: filterState.value ?? undefined,
    expiry_status:   filterExpiryStatus.value ?? undefined,
    tag_id:          filterTagId.value ?? undefined,
    limit: 50,
  })
}

// ── Bulk actions ──────────────────────────────────────────────────────────────
async function handleBulkAddTags(tagIds: number[]) {
  if (checkedRowKeys.value.length === 0 || tagIds.length === 0) return
  try {
    await tagStore.bulk({ domain_ids: checkedRowKeys.value, action: 'add_tags', tag_ids: tagIds })
    message.success(`已為 ${checkedRowKeys.value.length} 筆域名新增標籤`)
    checkedRowKeys.value = []
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  }
}

function handleExport() {
  const url = tagApi.exportUrl({
    ...(selectedProjectId.value ? { project_id: String(selectedProjectId.value) } : {}),
    ...(filterTagId.value ? { tag_id: String(filterTagId.value) } : {}),
    ...(filterState.value ? { lifecycle_state: filterState.value } : {}),
  })
  window.open(url, '_blank')
}

// ── Create modal ──────────────────────────────────────────────────────────────
const showCreate = ref(false)
const creating   = ref(false)
const form = ref<RegisterDomainRequest & { expiry_ts: number | null }>({
  project_id:           0,
  fqdn:                 '',
  registrar_account_id: null,
  dns_provider_id:      null,
  auto_renew:           true,
  annual_cost:          null,
  currency:             'USD',
  notes:                null,
  expiry_ts:            null,
})

// Registrar account options (load on demand when registrar selected)
const accountOptions = ref<SelectOption[]>([])
async function onRegistrarChange(rid: number | null) {
  accountOptions.value = []
  if (!rid) return
  await registrars.fetchAccounts(rid)
  accountOptions.value = registrars.accounts.map(a => ({ label: a.account_name, value: a.id }))
}

function openCreate() {
  form.value = {
    project_id:           selectedProjectId.value ?? 0,
    fqdn:                 '',
    registrar_account_id: null,
    dns_provider_id:      null,
    auto_renew:           true,
    annual_cost:          null,
    currency:             'USD',
    notes:                null,
    expiry_ts:            null,
  }
  showCreate.value = true
}

async function handleCreate() {
  if (!form.value.project_id || !form.value.fqdn) {
    message.warning('請選擇專案並填寫域名')
    return
  }
  creating.value = true
  try {
    const payload: RegisterDomainRequest = {
      project_id:           form.value.project_id,
      fqdn:                 form.value.fqdn,
      registrar_account_id: form.value.registrar_account_id,
      dns_provider_id:      form.value.dns_provider_id,
      auto_renew:           form.value.auto_renew,
      annual_cost:          form.value.annual_cost,
      currency:             form.value.currency,
      notes:                form.value.notes,
      expiry_date:          form.value.expiry_ts
        ? new Date(form.value.expiry_ts).toISOString().split('T')[0]
        : null,
    }
    await store.register(payload)
    message.success('域名註冊成功')
    showCreate.value = false
    loadDomains()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '註冊失敗')
  } finally {
    creating.value = false
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const columns: DataTableColumns<DomainResponse> = [
  { type: 'selection' },
  { title: '域名', key: 'fqdn', ellipsis: { tooltip: true }, minWidth: 200 },
  { title: 'TLD', key: 'tld', width: 100,
    render: (row) => row.tld ?? '-' },
  { title: '狀態', key: 'lifecycle_state', width: 130,
    render: (row) => h(StatusTag, { status: row.lifecycle_state }) },
  { title: 'DNS Provider', key: 'dns_provider_id', width: 140,
    render: (row) => row.dns_provider_id ? `#${row.dns_provider_id}` : '-' },
  { title: '到期日', key: 'expiry_date', width: 120,
    render: (row): VNodeChild => {
      if (!row.expiry_date) return '-'
      const d = new Date(row.expiry_date)
      const now = new Date()
      const diffDays = Math.ceil((d.getTime() - now.getTime()) / 86400000)
      const text = d.toLocaleDateString('zh-TW')
      if (diffDays <= 7)  return h('span', { style: 'color: var(--error)' }, text)
      if (diffDays <= 30) return h('span', { style: 'color: var(--warning)' }, text)
      return text
    },
  },
  { title: '自動續約', key: 'auto_renew', width: 90,
    render: (row) => row.auto_renew ? '✓' : '✗' },
  { title: '年費', key: 'annual_cost', width: 100,
    render: (row) => row.annual_cost != null
      ? `${row.annual_cost} ${row.currency ?? ''}`.trim()
      : '-',
  },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row): VNodeChild => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/domains/${row.id}`),
    }, { default: () => '查看' }),
  },
]

onMounted(async () => {
  await Promise.all([
    registrars.fetchList(),
    dnsProviders.fetchList(),
    tagStore.fetchList(),
    routeProjectId ? null : projects.fetchList(),
  ])
  if (selectedProjectId.value) loadDomains()
})
</script>

<template>
  <div class="list-page">
    <PageHeader title="域名管理" :subtitle="selectedProjectName || '請先選擇專案'">
      <template #actions>
        <NSelect
          v-if="!routeProjectId"
          v-model:value="selectedProjectId"
          :options="projectOptions"
          :loading="projects.loading"
          placeholder="選擇專案"
          clearable
          style="width: 200px"
          @update:value="loadDomains"
        />
        <NButton type="primary" :disabled="!selectedProjectId" @click="openCreate">
          註冊域名
        </NButton>
        <NButton @click="router.push('/domains/import')">批次匯入</NButton>
        <NButton :disabled="!selectedProjectId" @click="handleExport">匯出 CSV</NButton>
      </template>
      <template #hint>
        <PageHint storage-key="domain-list" title="域名管理說明">
          生命週期：<strong>requested → approved → provisioned → active → disabled → retired</strong><br>
          只有 <strong>active</strong> 狀態的域名才能被包含在新的發布中。
        </PageHint>
      </template>
    </PageHeader>

    <!-- Filters -->
    <div class="filter-bar">
      <NSelect
        v-model:value="filterState"
        :options="stateOptions"
        placeholder="狀態篩選"
        clearable
        style="width: 140px"
        @update:value="loadDomains"
      />
      <NSelect
        v-model:value="filterRegistrar"
        :options="registrarOptions"
        placeholder="Registrar"
        clearable
        style="width: 160px"
        @update:value="loadDomains"
      />
      <NSelect
        v-model:value="filterDNSProvider"
        :options="dnsProviderOptions"
        placeholder="DNS Provider"
        clearable
        style="width: 160px"
        @update:value="loadDomains"
      />
      <NInput
        v-model:value="filterTLD"
        placeholder="TLD (e.g. .com)"
        clearable
        style="width: 130px"
        @change="loadDomains"
      />
      <NSelect
        v-model:value="filterExpiryStatus"
        :options="expiryStatusOptions"
        placeholder="到期狀態"
        clearable
        style="width: 140px"
        @update:value="loadDomains"
      />
      <NSelect
        v-model:value="filterTagId"
        :options="tagOptions"
        placeholder="標籤"
        clearable
        style="width: 140px"
        @update:value="loadDomains"
      />
    </div>

    <!-- Bulk action bar -->
    <div v-if="checkedRowKeys.length > 0" class="bulk-bar">
      <span>已選 {{ checkedRowKeys.length }} 筆</span>
      <NSelect
        placeholder="批次加標籤"
        :options="tagOptions"
        style="width: 160px"
        @update:value="(v: number) => handleBulkAddTags([v])"
      />
      <NButton size="small" @click="checkedRowKeys = []">取消選取</NButton>
    </div>

    <AppTable
      :columns="columns"
      :data="store.domains"
      :loading="store.loading"
      :row-key="(r: DomainResponse) => r.id"
      :checked-row-keys="checkedRowKeys"
      @update:checked-row-keys="(keys: number[]) => checkedRowKeys = keys"
    />

    <!-- Create modal -->
    <NModal v-model:show="showCreate" :mask-closable="!creating">
      <NCard title="註冊域名" :bordered="false" style="width: 560px">
        <NForm label-placement="left" label-width="110px">
          <NFormItem label="域名" required>
            <NInput v-model:value="form.fqdn" placeholder="例：shop.example.com" />
          </NFormItem>
          <NFormItem label="Registrar 帳號">
            <NSpace vertical style="width:100%">
              <NSelect
                :options="registrarOptions"
                placeholder="選擇 Registrar（選填）"
                clearable
                @update:value="onRegistrarChange"
              />
              <NSelect
                v-model:value="(form as any).registrar_account_id"
                :options="accountOptions"
                placeholder="選擇帳號（選填）"
                clearable
                :disabled="accountOptions.length === 0"
              />
            </NSpace>
          </NFormItem>
          <NFormItem label="DNS Provider">
            <NSelect
              v-model:value="(form as any).dns_provider_id"
              :options="dnsProviderOptions"
              placeholder="選擇 DNS Provider（選填）"
              clearable
            />
          </NFormItem>
          <NFormItem label="到期日">
            <NDatePicker
              v-model:value="(form as any).expiry_ts"
              type="date"
              placeholder="選擇到期日"
              clearable
              style="width:100%"
            />
          </NFormItem>
          <NFormItem label="自動續約">
            <NSwitch v-model:value="form.auto_renew" />
          </NFormItem>
          <NFormItem label="年費">
            <NSpace>
              <NInputNumber
                v-model:value="(form as any).annual_cost"
                placeholder="0.00"
                :min="0"
                :precision="2"
                style="width:120px"
              />
              <NSelect
                v-model:value="(form as any).currency"
                :options="[{label:'USD',value:'USD'},{label:'EUR',value:'EUR'},{label:'CNY',value:'CNY'},{label:'TWD',value:'TWD'}]"
                style="width:90px"
              />
            </NSpace>
          </NFormItem>
          <NFormItem label="備注">
            <NInput v-model:value="(form as any).notes" type="textarea" :rows="2" clearable />
          </NFormItem>
        </NForm>
        <template #action>
          <div style="display: flex; justify-content: flex-end; gap: 8px">
            <NButton :disabled="creating" @click="showCreate = false">取消</NButton>
            <NButton type="primary" :loading="creating" @click="handleCreate">註冊</NButton>
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
.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px var(--content-padding);
  border-bottom: 1px solid var(--border);
}
.bulk-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px var(--content-padding);
  background: var(--info-bg, #e8f4fd);
  font-size: 13px;
}
</style>
