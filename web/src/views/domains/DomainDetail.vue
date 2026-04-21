<script setup lang="ts">
import { onMounted, ref, computed, h } from 'vue'
import { useRoute } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem,
  NTimeline, NTimelineItem, NButton, NSpace, NModal, NForm,
  NFormItem, NInput, NInputNumber, NSelect, NSwitch, NDatePicker,
  NDataTable, NTag, NPopconfirm, NAlert,
  useMessage,
} from 'naive-ui'
import { PageHeader, StatusTag, ConfirmModal, PageHint } from '@/components'
import { useDomainStore } from '@/stores/domain'
import { useRegistrarStore } from '@/stores/registrar'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import { useSSLStore } from '@/stores/ssl'
import { useCostStore } from '@/stores/cost'
import { useTagStore } from '@/stores/tag'
import { domainApi } from '@/api/domain'
import { dnsApi } from '@/api/dns'
import type { DomainLifecycleHistoryEntry, UpdateDomainAssetRequest } from '@/types/domain'
import type { DNSRecord, DNSLookupResult, PropagationResult, ResolverResult, DriftResult, ProviderRecord, CreateProviderRecordRequest } from '@/types/dns'
import type { SSLCertResponse } from '@/types/ssl'
import type { DomainCostResponse, CostType } from '@/types/cost'
import type { DomainLifecycleState, ApiResponse } from '@/types/common'
import type { SelectOption } from 'naive-ui'

const route    = useRoute()
const store    = useDomainStore()
const regStore = useRegistrarStore()
const dnsStore = useDNSProviderStore()
const sslStore  = useSSLStore()
const costStore = useCostStore()
const tagStore  = useTagStore()
const message   = useMessage()

// domain ID from route (numeric)
const idParam = route.params.id as string
const domainId = Number(idParam)

const history       = ref<DomainLifecycleHistoryEntry[]>([])
const actionLoading = ref(false)
const showRetire    = ref(false)

// ── Lifecycle transitions ─────────────────────────────────────────────────────
const validTransitions: Record<string, DomainLifecycleState[]> = {
  requested:   ['approved', 'retired'],
  approved:    ['provisioned', 'retired'],
  provisioned: ['active', 'disabled', 'retired'],
  active:      ['disabled', 'retired'],
  disabled:    ['active', 'retired'],
  retired:     [],
}

const nextStates = computed(() => {
  const s = store.current?.lifecycle_state
  if (!s) return []
  return (validTransitions[s] || []).filter(st => st !== 'retired')
})

const canRetire = computed(() => {
  const s = store.current?.lifecycle_state
  return s && s !== 'retired'
})

const transitionLabel: Record<string, string> = {
  approved: '核准', provisioned: '佈建完成', active: '啟用', disabled: '停用',
}

async function handleTransition(to: DomainLifecycleState) {
  actionLoading.value = true
  try {
    await store.transition(domainId, { to, reason: `operator: ${to}` })
    message.success(`已轉換至 ${to}`)
    await refreshHistory()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function handleRetire() {
  actionLoading.value = true
  try {
    await store.transition(domainId, { to: 'retired', reason: 'operator retired' })
    message.success('已退役')
    showRetire.value = false
    await refreshHistory()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function refreshHistory() {
  const res = await domainApi.history(domainId) as unknown as ApiResponse<DomainLifecycleHistoryEntry[]>
  history.value = res.data ?? []
}

// ── Edit asset modal ──────────────────────────────────────────────────────────
const showEdit  = ref(false)
const saving    = ref(false)

interface EditForm extends UpdateDomainAssetRequest {
  expiry_ts:        number | null
  registration_ts:  number | null
  _selectedRegistrar: number | null
}

const editForm = ref<EditForm>({
  registrar_account_id: null,
  dns_provider_id:      null,
  auto_renew:           false,
  transfer_lock:        false,
  dnssec_enabled:       false,
  whois_privacy:        false,
  annual_cost:          null,
  currency:             'USD',
  purchase_price:       null,
  fee_fixed:            false,
  purpose:              null,
  notes:                null,
  expiry_ts:            null,
  registration_ts:      null,
  _selectedRegistrar:   null,
})

const accountOptions  = ref<SelectOption[]>([])
const registrarOptions = computed(() => regStore.registrars.map(r => ({ label: r.name, value: r.id })))
const dnsOptions      = computed(() => dnsStore.providers.map(p => ({ label: p.name, value: p.id })))

async function onEditRegistrarChange(rid: number | null) {
  accountOptions.value = []
  if (!rid) { editForm.value.registrar_account_id = null; return }
  await regStore.fetchAccounts(rid)
  accountOptions.value = regStore.accounts.map(a => ({ label: a.account_name, value: a.id }))
}

function openEdit() {
  const d = store.current
  if (!d) return
  editForm.value = {
    registrar_account_id: d.registrar_account_id,
    dns_provider_id:      d.dns_provider_id,
    auto_renew:           d.auto_renew,
    transfer_lock:        d.transfer_lock,
    dnssec_enabled:       d.dnssec_enabled,
    whois_privacy:        d.whois_privacy,
    annual_cost:          d.annual_cost,
    currency:             d.currency ?? 'USD',
    purchase_price:       d.purchase_price,
    fee_fixed:            d.fee_fixed,
    purpose:              d.purpose,
    notes:                d.notes,
    expiry_ts:            d.expiry_date       ? new Date(d.expiry_date).getTime() : null,
    registration_ts:      d.registration_date ? new Date(d.registration_date).getTime() : null,
    _selectedRegistrar:   null,
  }
  showEdit.value = true
}

async function submitEdit() {
  saving.value = true
  try {
    const payload: UpdateDomainAssetRequest = {
      registrar_account_id: editForm.value.registrar_account_id,
      dns_provider_id:      editForm.value.dns_provider_id,
      auto_renew:           editForm.value.auto_renew,
      transfer_lock:        editForm.value.transfer_lock,
      dnssec_enabled:       editForm.value.dnssec_enabled,
      whois_privacy:        editForm.value.whois_privacy,
      annual_cost:          editForm.value.annual_cost,
      currency:             editForm.value.currency,
      purchase_price:       editForm.value.purchase_price,
      fee_fixed:            editForm.value.fee_fixed,
      purpose:              editForm.value.purpose,
      notes:                editForm.value.notes,
      expiry_date:          editForm.value.expiry_ts
        ? new Date(editForm.value.expiry_ts).toISOString().split('T')[0]
        : null,
      registration_date:    editForm.value.registration_ts
        ? new Date(editForm.value.registration_ts).toISOString().split('T')[0]
        : null,
    }
    await store.updateAsset(domainId, payload)
    message.success('資產資料已更新')
    showEdit.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '更新失敗')
  } finally {
    saving.value = false
  }
}

// ── Transfer ──────────────────────────────────────────────────────────────────
const showTransfer  = ref(false)
const transferring  = ref(false)
const transferForm  = ref({ gaining_registrar: '' })

async function submitTransfer() {
  transferring.value = true
  try {
    await store.initiateTransfer(domainId, {
      gaining_registrar: transferForm.value.gaining_registrar || null,
    })
    message.success('轉移已發起')
    showTransfer.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    transferring.value = false
  }
}

async function doCompleteTransfer() {
  actionLoading.value = true
  try {
    await store.completeTransfer(domainId)
    message.success('轉移完成')
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

async function doCancelTransfer() {
  actionLoading.value = true
  try {
    await store.cancelTransfer(domainId)
    message.success('轉移已取消')
  } catch (e: any) {
    message.error(e?.response?.data?.message || '操作失敗')
  } finally {
    actionLoading.value = false
  }
}

// ── SSL ───────────────────────────────────────────────────────────────────────
const showSSLCreate = ref(false)
const sslCreating   = ref(false)
const sslChecking   = ref(false)
const sslForm       = ref({ expires_at: '', issuer: '', cert_type: '', serial_number: '', notes: '' })

const sslStatusType = (status: string) => {
  if (status === 'active')   return 'success'
  if (status === 'expiring') return 'warning'
  if (status === 'expired')  return 'error'
  return 'default'
}

const sslColumns: DataTableColumns<SSLCertResponse> = [
  { title: '到期日', key: 'expires_at', width: 120 },
  { title: '剩餘天數', key: 'days_left', width: 90,
    render: (row): VNodeChild => {
      const color = row.days_left <= 0 ? 'var(--error)' : row.days_left <= 30 ? 'var(--warning)' : undefined
      return h('span', { style: color ? `color: ${color}` : '' }, String(row.days_left))
    },
  },
  { title: '狀態', key: 'status', width: 100,
    render: (row): VNodeChild => h(NTag, { type: sslStatusType(row.status), size: 'small', bordered: false }, { default: () => row.status }),
  },
  { title: '簽發機構', key: 'issuer', ellipsis: { tooltip: true } },
  { title: 'Serial', key: 'serial_number', ellipsis: { tooltip: true } },
  { title: '最後檢查', key: 'last_check_at', width: 170,
    render: (row): VNodeChild => row.last_check_at ? new Date(row.last_check_at).toLocaleString('zh-TW') : '-',
  },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row): VNodeChild => h(NPopconfirm,
      { onPositiveClick: () => handleSSLDelete(row.id) },
      {
        trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '刪除' }),
        default: () => '確認刪除此憑證記錄？',
      }
    ),
  },
]

async function handleSSLCheck() {
  const fqdn = store.current?.fqdn
  if (!fqdn) return
  sslChecking.value = true
  try {
    await sslStore.check(domainId, fqdn)
    message.success('SSL 檢查完成')
  } catch (e: any) {
    message.error(e?.response?.data?.message || 'SSL 檢查失敗')
  } finally {
    sslChecking.value = false
  }
}

async function handleSSLCreate() {
  if (!sslForm.value.expires_at) {
    message.warning('請填寫到期日')
    return
  }
  sslCreating.value = true
  try {
    await sslStore.create(domainId, {
      expires_at:    sslForm.value.expires_at,
      issuer:        sslForm.value.issuer || null,
      cert_type:     sslForm.value.cert_type || null,
      serial_number: sslForm.value.serial_number || null,
      notes:         sslForm.value.notes || null,
    })
    message.success('憑證已新增')
    showSSLCreate.value = false
    sslForm.value = { expires_at: '', issuer: '', cert_type: '', serial_number: '', notes: '' }
    await sslStore.fetchList(domainId)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '新增失敗')
  } finally {
    sslCreating.value = false
  }
}

async function handleSSLDelete(id: number) {
  try {
    await sslStore.deleteCert(id)
    message.success('已刪除')
  } catch (e: any) {
    message.error(e?.response?.data?.message || '刪除失敗')
  }
}

// ── Tags ──────────────────────────────────────────────────────────────────────
const domainTagIds = computed(() => tagStore.domainTags.map(t => t.id))
const allTagOptions = computed(() => tagStore.tags.map(t => ({ label: t.name, value: t.id })))

async function handleTagChange(ids: number[]) {
  try {
    await tagStore.setDomainTags(domainId, ids)
    message.success('標籤已更新')
  } catch (e: any) {
    message.error(e?.response?.data?.message || '更新失敗')
  }
}

// ── DNS Records ──────────────────────────────────────────────────────────────
const dnsLoading   = ref(false)
const dnsResult    = ref<DNSLookupResult | null>(null)

const dnsRecordTypeColor: Record<string, string> = {
  A: 'success', AAAA: 'info', CNAME: 'warning', MX: 'default',
  TXT: 'default', NS: 'info', SOA: 'default', SRV: 'default',
  CAA: 'warning', PTR: 'default',
}

const dnsColumns: DataTableColumns<DNSRecord> = [
  { title: '類型', key: 'type', width: 80,
    render: (row): VNodeChild => h(NTag, { type: dnsRecordTypeColor[row.type] as any, size: 'small', bordered: false }, { default: () => row.type }),
  },
  { title: '值', key: 'value', ellipsis: { tooltip: true },
    render: (row): VNodeChild => {
      const parts = [row.value]
      if ((row.type === 'MX' || row.type === 'SRV') && row.priority !== undefined) {
        parts.unshift(`[${row.priority}]`)
      }
      return h('code', { style: 'font-size:12px; word-break:break-all' }, parts.join(' '))
    },
  },
  { title: 'TTL', key: 'ttl', width: 80,
    render: (row): VNodeChild => h('span', { style: 'font-family:var(--font-mono); font-size:12px' }, `${row.ttl}s`),
  },
]

const driftColumns: DataTableColumns<any> = [
  { title: '狀態', key: 'match', width: 60,
    render: (row: any): VNodeChild => h('span', {
      style: `font-size:14px; color:${row.match ? 'var(--success,#18a058)' : 'var(--error,#d03050)'}`,
    }, row.match ? '✓' : '✗'),
  },
  { title: '類型', key: 'type', width: 70,
    render: (row: any): VNodeChild => h(NTag, { size: 'tiny', bordered: false }, { default: () => row.type }),
  },
  { title: '預期 (Provider)', key: 'expected', ellipsis: { tooltip: true },
    render: (row: any): VNodeChild => row.expected
      ? h('code', { style: 'font-size:12px' }, row.expected)
      : h('span', { style: 'color:var(--text-muted)' }, '—'),
  },
  { title: '實際 (DNS)', key: 'actual', ellipsis: { tooltip: true },
    render: (row: any): VNodeChild => row.actual
      ? h('code', { style: 'font-size:12px' }, row.actual)
      : h('span', { style: 'color:var(--text-muted)' }, '—'),
  },
]

async function handleDNSLookup() {
  dnsLoading.value = true
  try {
    const res = await dnsApi.lookupByDomain(domainId) as unknown as { data: DNSLookupResult }
    dnsResult.value = res.data
  } catch (e: any) {
    message.error(e?.response?.data?.message || 'DNS 查詢失敗')
  } finally {
    dnsLoading.value = false
  }
}

// ── Provider records (CRUD) ──────────────────────────────────────────────────
const provRecords      = ref<ProviderRecord[]>([])
const provLoading      = ref(false)
const showProvCreate   = ref(false)
const provCreating     = ref(false)
const provForm         = ref<CreateProviderRecordRequest>({
  type: 'A', name: '', content: '', ttl: 1, priority: 0, proxied: false,
})

const provRecordTypes = [
  { label: 'A',     value: 'A'     },
  { label: 'AAAA',  value: 'AAAA'  },
  { label: 'CNAME', value: 'CNAME' },
  { label: 'MX',    value: 'MX'    },
  { label: 'TXT',   value: 'TXT'   },
  { label: 'NS',    value: 'NS'    },
  { label: 'SRV',   value: 'SRV'   },
  { label: 'CAA',   value: 'CAA'   },
]

async function loadProvRecords() {
  provLoading.value = true
  try {
    const res = await dnsApi.listProviderRecords(domainId) as any
    provRecords.value = res.data?.items ?? []
  } catch (e: any) {
    message.error(e?.response?.data?.message || '載入 Provider 記錄失敗')
  } finally {
    provLoading.value = false
  }
}

async function handleProvCreate() {
  if (!provForm.value.name || !provForm.value.content) {
    message.warning('請填寫記錄名稱和值')
    return
  }
  provCreating.value = true
  try {
    await dnsApi.createProviderRecord(domainId, provForm.value)
    message.success('記錄已建立')
    showProvCreate.value = false
    provForm.value = { type: 'A', name: store.current?.fqdn ?? '', content: '', ttl: 1, priority: 0, proxied: false }
    await loadProvRecords()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '建立失敗')
  } finally {
    provCreating.value = false
  }
}

async function handleProvDelete(recordId: string) {
  try {
    await dnsApi.deleteProviderRecord(domainId, recordId)
    message.success('記錄已刪除')
    await loadProvRecords()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '刪除失敗')
  }
}

const provColumns: DataTableColumns<ProviderRecord> = [
  { title: '類型', key: 'type', width: 70,
    render: (row): VNodeChild => h(NTag, { type: dnsRecordTypeColor[row.type] || 'default', size: 'small', bordered: false }, { default: () => row.type }),
  },
  { title: '名稱', key: 'name', ellipsis: { tooltip: true }, minWidth: 160 },
  { title: '值', key: 'content', ellipsis: { tooltip: true }, minWidth: 160,
    render: (row): VNodeChild => h('code', { style: 'font-size:12px' }, row.content),
  },
  { title: 'TTL', key: 'ttl', width: 70,
    render: (row): VNodeChild => h('span', { style: 'font-family:var(--font-mono);font-size:12px' }, row.ttl === 1 ? 'Auto' : `${row.ttl}s`),
  },
  { title: '優先級', key: 'priority', width: 70,
    render: (row): VNodeChild => row.priority ? String(row.priority) : '-',
  },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row): VNodeChild => h(NPopconfirm,
      { onPositiveClick: () => handleProvDelete(row.id) },
      {
        trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '刪除' }),
        default: () => `確認刪除 ${row.type} 記錄？`,
      },
    ),
  },
]

// ── Propagation check ────────────────────────────────────────────────────────
const propLoading   = ref(false)
const propResult    = ref<PropagationResult | null>(null)
const propQueryType = ref('A')

const propTypeOptions = [
  { label: 'A',    value: 'A' },
  { label: 'AAAA', value: 'AAAA' },
  { label: 'MX',   value: 'MX' },
  { label: 'NS',   value: 'NS' },
  { label: 'TXT',  value: 'TXT' },
  { label: 'CNAME', value: 'CNAME' },
  { label: 'CAA',  value: 'CAA' },
]

async function handlePropagationCheck() {
  propLoading.value = true
  try {
    const res = await dnsApi.propagationByDomain(domainId, propQueryType.value) as unknown as { data: PropagationResult }
    propResult.value = res.data
  } catch (e: any) {
    message.error(e?.response?.data?.message || '傳播檢查失敗')
  } finally {
    propLoading.value = false
  }
}

function propRecordSummary(resolver: ResolverResult): string {
  if (resolver.error) return `錯誤: ${resolver.error}`
  if (!resolver.records || resolver.records.length === 0) return '（無記錄）'
  return resolver.records.map(r => {
    const prefix = (r.type === 'MX' || r.type === 'SRV') && r.priority !== undefined
      ? `[${r.priority}] ` : ''
    return `${prefix}${r.value}`
  }).join(', ')
}

// ── Drift check ──────────────────────────────────────────────────────────────
const driftLoading = ref(false)
const driftResult  = ref<DriftResult | null>(null)

const driftStatusConfig: Record<string, { label: string; type: string }> = {
  ok:          { label: '無偏差', type: 'success' },
  drift:       { label: '偏差',   type: 'error'   },
  no_expected: { label: '無預期記錄', type: 'warning' },
  error:       { label: '錯誤',   type: 'error'   },
}

async function handleDriftCheck() {
  driftLoading.value = true
  try {
    const res = await dnsApi.driftCheck(domainId) as unknown as { data: DriftResult }
    driftResult.value = res.data
  } catch (e: any) {
    message.error(e?.response?.data?.message || 'Drift 檢查失敗')
  } finally {
    driftLoading.value = false
  }
}

// ── Cost ─────────────────────────────────────────────────────────────────────
const showCostCreate = ref(false)
const costCreating   = ref(false)
const costForm       = ref<{ cost_type: CostType; amount: number; currency: string; paid_at: string; notes: string }>({
  cost_type: 'renewal', amount: 0, currency: 'USD', paid_at: '', notes: '',
})

const costTypeOptions = [
  { label: '註冊', value: 'registration' },
  { label: '續約', value: 'renewal' },
  { label: '轉移', value: 'transfer' },
  { label: 'WHOIS 隱私', value: 'privacy' },
  { label: '其他', value: 'other' },
]

const costCurrencyOptions = [
  { label: 'USD', value: 'USD' }, { label: 'EUR', value: 'EUR' },
  { label: 'TWD', value: 'TWD' }, { label: 'CNY', value: 'CNY' },
  { label: 'JPY', value: 'JPY' }, { label: 'AUD', value: 'AUD' },
]

const costColumns: DataTableColumns<DomainCostResponse> = [
  { title: '類型', key: 'cost_type', width: 90 },
  { title: '金額', key: 'amount', width: 100,
    render: (row): VNodeChild => `${row.amount.toFixed(2)} ${row.currency}` },
  { title: '付款日', key: 'paid_at', width: 110,
    render: (row): VNodeChild => row.paid_at ?? '-' },
  { title: '週期起', key: 'period_start', width: 110,
    render: (row): VNodeChild => row.period_start ?? '-' },
  { title: '週期止', key: 'period_end', width: 110,
    render: (row): VNodeChild => row.period_end ?? '-' },
  { title: '備注', key: 'notes', ellipsis: { tooltip: true } },
  { title: '建立時間', key: 'created_at', width: 170,
    render: (row): VNodeChild => new Date(row.created_at).toLocaleString('zh-TW') },
]

async function handleCostCreate() {
  if (!costForm.value.cost_type || costForm.value.amount <= 0) {
    message.warning('請選擇費用類型並輸入金額')
    return
  }
  costCreating.value = true
  try {
    await costStore.createDomainCost(domainId, {
      cost_type: costForm.value.cost_type,
      amount:    costForm.value.amount,
      currency:  costForm.value.currency,
      paid_at:   costForm.value.paid_at || null,
      notes:     costForm.value.notes || null,
    })
    message.success('費用記錄已新增')
    showCostCreate.value = false
    costForm.value = { cost_type: 'renewal', amount: 0, currency: 'USD', paid_at: '', notes: '' }
  } catch (e: any) {
    message.error(e?.response?.data?.message || '新增失敗')
  } finally {
    costCreating.value = false
  }
}

// ── Helper ────────────────────────────────────────────────────────────────────
function fmtDate(s: string | null | undefined): string {
  return s ? new Date(s).toLocaleDateString('zh-TW') : '-'
}

onMounted(async () => {
  await store.fetchOne(domainId)
  await refreshHistory()
  await Promise.all([
    regStore.fetchList(),
    dnsStore.fetchList(),
    sslStore.fetchList(domainId),
    costStore.fetchDomainCosts(domainId),
    tagStore.fetchList(),
    tagStore.fetchDomainTags(domainId),
  ])
})
</script>

<template>
  <div class="detail-page">
    <PageHeader
      :title="store.current?.fqdn ?? '載入中...'"
      subtitle="域名詳情"
    >
      <template #actions>
        <NSpace v-if="store.current">
          <NButton @click="openEdit">編輯資產</NButton>
          <NButton
            v-for="state in nextStates"
            :key="state"
            type="primary"
            :loading="actionLoading"
            @click="handleTransition(state)"
          >
            {{ transitionLabel[state] || state }}
          </NButton>
          <NButton
            v-if="canRetire"
            type="error"
            :loading="actionLoading"
            @click="showRetire = true"
          >
            退役
          </NButton>
        </NSpace>
      </template>
      <template #hint>
        <PageHint storage-key="domain-detail" title="域名操作說明">
          頂部按鈕依當前狀態動態顯示可執行的操作。<br>
          <strong>核准</strong> → <strong>佈建完成</strong> → <strong>啟用</strong>（可加入發布）<br>
          <strong>停用</strong>：暫停；<strong>退役</strong>：永久終止，無法還原。
        </PageHint>
      </template>
    </PageHeader>

    <div v-if="store.current" class="detail-page__body">
      <!-- Sidebar: core info -->
      <div class="detail-page__sidebar">
        <NDescriptions bordered :column="1" label-placement="left">
          <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
          <NDescriptionsItem label="TLD">{{ store.current.tld ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="狀態">
            <StatusTag :status="store.current.lifecycle_state" />
          </NDescriptionsItem>
          <NDescriptionsItem label="專案 ID">{{ store.current.project_id }}</NDescriptionsItem>
          <NDescriptionsItem label="Registrar 帳號">
            {{ store.current.registrar_account_id != null ? `#${store.current.registrar_account_id}` : '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="DNS Provider">
            {{ store.current.dns_provider_id != null ? `#${store.current.dns_provider_id}` : '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="到期日">{{ fmtDate(store.current.expiry_date) }}</NDescriptionsItem>
          <NDescriptionsItem label="自動續約">{{ store.current.auto_renew ? '是' : '否' }}</NDescriptionsItem>
          <NDescriptionsItem label="到期狀態">{{ store.current.expiry_status ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="建立時間">
            {{ new Date(store.current.created_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
        </NDescriptions>

        <div class="section-title" style="margin-top:16px">標籤</div>
        <NSelect
          :value="domainTagIds"
          :options="allTagOptions"
          multiple
          placeholder="選擇標籤"
          clearable
          @update:value="handleTagChange"
        />
      </div>

      <!-- Main: tabs -->
      <div class="detail-page__main">
        <NTabs type="line" animated>

          <!-- Asset tab -->
          <NTabPane name="asset" tab="資產資訊">
            <div class="tab-section">
              <div class="section-title">登記資訊</div>
              <NDescriptions bordered :column="2">
                <NDescriptionsItem label="註冊日期">{{ fmtDate(store.current.registration_date) }}</NDescriptionsItem>
                <NDescriptionsItem label="到期日">{{ fmtDate(store.current.expiry_date) }}</NDescriptionsItem>
                <NDescriptionsItem label="Grace 到期">{{ fmtDate(store.current.grace_end_date) }}</NDescriptionsItem>
                <NDescriptionsItem label="最後續約">{{ fmtDate(store.current.last_renewed_at) }}</NDescriptionsItem>
                <NDescriptionsItem label="Transfer Lock">{{ store.current.transfer_lock ? '是' : '否' }}</NDescriptionsItem>
                <NDescriptionsItem label="Hold">{{ store.current.hold ? '是' : '否' }}</NDescriptionsItem>
                <NDescriptionsItem label="DNSSEC">{{ store.current.dnssec_enabled ? '啟用' : '停用' }}</NDescriptionsItem>
                <NDescriptionsItem label="WHOIS 隱私">{{ store.current.whois_privacy ? '啟用' : '停用' }}</NDescriptionsItem>
              </NDescriptions>

              <div class="section-title">財務</div>
              <NDescriptions bordered :column="2">
                <NDescriptionsItem label="年費">
                  {{ store.current.annual_cost != null ? `${store.current.annual_cost} ${store.current.currency ?? ''}` : '-' }}
                </NDescriptionsItem>
                <NDescriptionsItem label="購買價格">
                  {{ store.current.purchase_price != null ? `${store.current.purchase_price} ${store.current.currency ?? ''}` : '-' }}
                </NDescriptionsItem>
                <NDescriptionsItem label="費用固定">{{ store.current.fee_fixed ? '是' : '否' }}</NDescriptionsItem>
                <NDescriptionsItem label="用途">{{ store.current.purpose ?? '-' }}</NDescriptionsItem>
                <NDescriptionsItem label="備注" :span="2">{{ store.current.notes ?? '-' }}</NDescriptionsItem>
              </NDescriptions>
            </div>
          </NTabPane>

          <!-- Transfer tab -->
          <NTabPane name="transfer" tab="轉移管理">
            <div class="tab-section">
              <NDescriptions bordered :column="2">
                <NDescriptionsItem label="轉移狀態">
                  {{ store.current.transfer_status ?? '無' }}
                </NDescriptionsItem>
                <NDescriptionsItem label="目標 Registrar">
                  {{ store.current.transfer_gaining_registrar ?? '-' }}
                </NDescriptionsItem>
                <NDescriptionsItem label="發起時間">
                  {{ fmtDate(store.current.transfer_requested_at) }}
                </NDescriptionsItem>
                <NDescriptionsItem label="完成時間">
                  {{ fmtDate(store.current.transfer_completed_at) }}
                </NDescriptionsItem>
                <NDescriptionsItem label="最後轉移">
                  {{ fmtDate(store.current.last_transfer_at) }}
                </NDescriptionsItem>
              </NDescriptions>

              <div class="transfer-actions">
                <NButton
                  type="primary"
                  :disabled="store.current.transfer_status === 'pending'"
                  @click="showTransfer = true"
                >
                  發起轉移
                </NButton>
                <NButton
                  type="success"
                  :disabled="store.current.transfer_status !== 'pending'"
                  :loading="actionLoading"
                  @click="doCompleteTransfer"
                >
                  確認完成
                </NButton>
                <NButton
                  type="warning"
                  :disabled="store.current.transfer_status !== 'pending'"
                  :loading="actionLoading"
                  @click="doCancelTransfer"
                >
                  取消轉移
                </NButton>
              </div>
            </div>
          </NTabPane>

          <!-- SSL tab -->
          <NTabPane name="ssl" :tab="`SSL 憑證 (${sslStore.certs.length})`">
            <div class="tab-section">
              <div style="display:flex; gap:8px; margin-bottom: 12px;">
                <NButton type="primary" size="small" @click="showSSLCreate = true">手動新增</NButton>
                <NButton size="small" :loading="sslChecking" @click="handleSSLCheck">立即檢查</NButton>
              </div>
              <NDataTable
                :columns="sslColumns"
                :data="sslStore.certs"
                :loading="sslStore.loading"
                :row-key="(r: SSLCertResponse) => r.id"
                size="small"
                :max-height="320"
                scroll-x="700"
              />
            </div>
          </NTabPane>

          <!-- DNS Records tab -->
          <NTabPane name="dns" :tab="`DNS 查詢 (${dnsResult?.records?.length ?? '-'})`">
            <div class="tab-section">
              <div style="display:flex; gap:8px; margin-bottom: 12px; align-items:center; flex-wrap:wrap;">
                <NButton type="primary" size="small" :loading="dnsLoading" @click="handleDNSLookup">
                  {{ dnsResult ? '重新查詢' : '查詢 DNS 記錄' }}
                </NButton>
                <span v-if="dnsResult" class="dns-meta">
                  解析器：<code>{{ dnsResult.nameserver }}</code>
                  ・耗時 {{ dnsResult.elapsed_ms }}ms
                  ・{{ new Date(dnsResult.queried_at).toLocaleString('zh-TW') }}
                </span>
              </div>

              <template v-if="dnsResult && !dnsResult.error">
                <NDataTable
                  :columns="dnsColumns"
                  :data="dnsResult.records"
                  :row-key="(r: DNSRecord) => `${r.type}-${r.value}`"
                  size="small"
                  :max-height="400"
                  striped
                />
                <div v-if="dnsResult.records.length === 0" style="padding:16px; text-align:center; color:var(--text-muted); font-size:13px;">
                  未找到任何 DNS 記錄
                </div>
              </template>

              <template v-else-if="dnsResult?.error">
                <div style="padding:16px; color:var(--error); font-size:13px;">
                  查詢錯誤：{{ dnsResult.error }}
                </div>
              </template>

              <div v-else style="padding:24px; text-align:center; color:var(--text-muted); font-size:13px;">
                點擊「查詢 DNS 記錄」按鈕以取得此域名的即時 DNS 解析結果（A / AAAA / CNAME / MX / TXT / NS / SOA / SRV / CAA）
              </div>
            </div>
          </NTabPane>

          <!-- Provider Records (CRUD) tab -->
          <NTabPane name="provider-records" :tab="`解析管理 (${provRecords.length})`">
            <div class="tab-section">
              <div style="display:flex; gap:8px; margin-bottom:12px; align-items:center;">
                <NButton
                  size="small"
                  :loading="provLoading"
                  :disabled="!store.current?.dns_provider_id"
                  @click="loadProvRecords"
                >
                  重新載入
                </NButton>
                <NButton
                  type="primary"
                  size="small"
                  :disabled="!store.current?.dns_provider_id"
                  @click="showProvCreate = true"
                >
                  新增記錄
                </NButton>
                <span v-if="!store.current?.dns_provider_id" style="font-size:12px; color:var(--text-muted)">
                  此域名未設定 DNS Provider，無法管理記錄
                </span>
              </div>

              <NDataTable
                v-if="provRecords.length > 0 || provLoading"
                :columns="provColumns"
                :data="provRecords"
                :loading="provLoading"
                :row-key="(r: ProviderRecord) => r.id"
                size="small"
                :max-height="400"
                striped
                :scroll-x="600"
              />
              <div v-else style="padding:24px; text-align:center; color:var(--text-muted); font-size:13px;">
                點擊「重新載入」從 DNS Provider API 取得記錄，或「新增記錄」建立新的 DNS 解析。
              </div>
            </div>
          </NTabPane>

          <!-- Propagation tab -->
          <NTabPane name="propagation" tab="傳播檢測">
            <div class="tab-section">
              <div style="display:flex; gap:8px; margin-bottom:12px; align-items:center; flex-wrap:wrap;">
                <NSelect
                  v-model:value="propQueryType"
                  :options="propTypeOptions"
                  style="width:100px"
                  size="small"
                />
                <NButton type="primary" size="small" :loading="propLoading" @click="handlePropagationCheck">
                  檢測傳播狀態
                </NButton>
                <template v-if="propResult">
                  <NTag :type="propResult.consistent ? 'success' : 'warning'" size="small" :bordered="false">
                    {{ propResult.consistent ? '一致' : '不一致' }}
                  </NTag>
                  <span class="dns-meta">
                    耗時 {{ propResult.total_ms }}ms
                    ・{{ new Date(propResult.queried_at).toLocaleString('zh-TW') }}
                  </span>
                </template>
              </div>

              <template v-if="propResult">
                <div class="prop-grid">
                  <div
                    v-for="resolver in propResult.resolvers"
                    :key="resolver.address"
                    class="prop-card"
                    :class="{ 'prop-card--auth': resolver.authoritative, 'prop-card--error': !!resolver.error }"
                  >
                    <div class="prop-card__header">
                      <span class="prop-card__label">{{ resolver.label }}</span>
                      <span class="prop-card__meta">{{ resolver.address }} ・{{ resolver.elapsed_ms }}ms</span>
                    </div>
                    <div v-if="resolver.error" class="prop-card__error">
                      {{ resolver.error }}
                    </div>
                    <div v-else-if="resolver.records && resolver.records.length > 0" class="prop-card__records">
                      <div
                        v-for="(rec, idx) in resolver.records"
                        :key="idx"
                        class="prop-record"
                      >
                        <NTag :type="dnsRecordTypeColor[rec.type] || 'default'" size="tiny" :bordered="false">{{ rec.type }}</NTag>
                        <code class="prop-record__value">{{ rec.value }}</code>
                        <span class="prop-record__ttl">TTL {{ rec.ttl }}s</span>
                      </div>
                    </div>
                    <div v-else class="prop-card__empty">（無記錄）</div>
                  </div>
                </div>
              </template>

              <div v-else style="padding:24px; text-align:center; color:var(--text-muted); font-size:13px;">
                選擇記錄類型後點擊「檢測傳播狀態」，同時查詢 Google / Cloudflare / Quad9 / OpenDNS + 權威 NS，比對結果是否一致。
              </div>
            </div>
          </NTabPane>

          <!-- Drift tab -->
          <NTabPane name="drift" tab="Drift 檢測">
            <div class="tab-section">
              <div style="display:flex; gap:8px; margin-bottom:12px; align-items:center; flex-wrap:wrap;">
                <NButton
                  type="primary"
                  size="small"
                  :loading="driftLoading"
                  :disabled="!store.current?.dns_provider_id"
                  @click="handleDriftCheck"
                >
                  檢查 Provider ↔ DNS 偏差
                </NButton>
                <span v-if="!store.current?.dns_provider_id" style="font-size:12px; color:var(--text-muted)">
                  此域名未設定 DNS Provider，無法比對
                </span>
                <template v-if="driftResult">
                  <NTag
                    :type="(driftStatusConfig[driftResult.status]?.type as any) ?? 'default'"
                    size="small"
                    :bordered="false"
                  >
                    {{ driftStatusConfig[driftResult.status]?.label ?? driftResult.status }}
                  </NTag>
                  <span class="dns-meta">
                    Provider: {{ driftResult.provider_label }} ({{ driftResult.provider_name }})
                    ・耗時 {{ driftResult.elapsed_ms }}ms
                  </span>
                </template>
              </div>

              <!-- Error state -->
              <template v-if="driftResult?.error">
                <NAlert type="error" style="margin-bottom:12px">
                  {{ driftResult.error }}
                </NAlert>
              </template>

              <!-- Drift records table -->
              <template v-if="driftResult && driftResult.records && driftResult.records.length > 0">
                <div class="drift-summary">
                  <span class="drift-stat good">{{ driftResult.match_count }} 匹配</span>
                  <span v-if="driftResult.missing_count > 0" class="drift-stat bad">{{ driftResult.missing_count }} 缺失</span>
                  <span v-if="driftResult.extra_count > 0" class="drift-stat warn">{{ driftResult.extra_count }} 多餘</span>
                  <span v-if="driftResult.drift_count > 0" class="drift-stat bad">{{ driftResult.drift_count }} 偏差</span>
                </div>
                <NDataTable
                  :columns="driftColumns"
                  :data="driftResult.records"
                  :row-key="(r: any) => `${r.type}-${r.expected}-${r.actual}`"
                  size="small"
                  :max-height="400"
                  striped
                />
              </template>

              <div v-else-if="!driftResult" style="padding:24px; text-align:center; color:var(--text-muted); font-size:13px;">
                比對 DNS Provider API（期望記錄）與實際 DNS 解析結果，偵測偏差。<br>
                需先在域名設定中綁定 DNS Provider（Cloudflare 等）。
              </div>
            </div>
          </NTabPane>

          <!-- Cost tab -->
          <NTabPane name="cost" :tab="`費用記錄 (${costStore.domainCosts.length})`">
            <div class="tab-section">
              <div style="display:flex; gap:8px; margin-bottom:12px; align-items:center;">
                <span style="font-size:13px; color:var(--text-muted)">
                  年費：
                  <strong>
                    {{ store.current?.annual_cost != null
                      ? `${store.current.annual_cost} ${store.current.currency ?? ''}`
                      : '未設定' }}
                  </strong>
                  <template v-if="store.current?.fee_fixed">（已固定）</template>
                </span>
                <NButton size="small" type="primary" @click="showCostCreate = true">新增費用記錄</NButton>
              </div>
              <NDataTable
                :columns="costColumns"
                :data="costStore.domainCosts"
                :loading="costStore.loading"
                :row-key="(r: DomainCostResponse) => r.id"
                size="small"
                :max-height="320"
                scroll-x="760"
              />
            </div>
          </NTabPane>

          <!-- History tab -->
          <NTabPane name="history" :tab="`狀態歷史 (${history.length})`">
            <NTimeline class="history-timeline">
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
      </div>
    </div>

    <!-- Retire confirm -->
    <ConfirmModal
      v-model:show="showRetire"
      title="退役域名"
      :content="`確定要退役 ${store.current?.fqdn ?? ''} 嗎？此操作無法還原。`"
      type="danger"
      :loading="actionLoading"
      confirm-text="確認退役"
      @confirm="handleRetire"
    />

    <!-- Edit asset modal -->
    <NModal
      v-model:show="showEdit"
      preset="card"
      title="編輯資產資料"
      style="width: 600px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="110px">
        <NFormItem label="Registrar">
          <NSpace vertical style="width:100%">
            <NSelect
              v-model:value="editForm._selectedRegistrar"
              :options="registrarOptions"
              placeholder="選擇 Registrar"
              clearable
              @update:value="onEditRegistrarChange"
            />
            <NSelect
              v-model:value="(editForm as any).registrar_account_id"
              :options="accountOptions"
              placeholder="選擇帳號"
              clearable
              :disabled="accountOptions.length === 0 && editForm._selectedRegistrar === null"
            />
          </NSpace>
        </NFormItem>
        <NFormItem label="DNS Provider">
          <NSelect
            v-model:value="(editForm as any).dns_provider_id"
            :options="dnsOptions"
            placeholder="選擇 DNS Provider"
            clearable
          />
        </NFormItem>
        <NFormItem label="註冊日期">
          <NDatePicker
            v-model:value="(editForm as any).registration_ts"
            type="date"
            clearable
            style="width:100%"
          />
        </NFormItem>
        <NFormItem label="到期日">
          <NDatePicker
            v-model:value="(editForm as any).expiry_ts"
            type="date"
            clearable
            style="width:100%"
          />
        </NFormItem>
        <NFormItem label="自動續約">
          <NSwitch v-model:value="editForm.auto_renew" />
        </NFormItem>
        <NFormItem label="Transfer Lock">
          <NSwitch v-model:value="editForm.transfer_lock" />
        </NFormItem>
        <NFormItem label="DNSSEC">
          <NSwitch v-model:value="editForm.dnssec_enabled" />
        </NFormItem>
        <NFormItem label="WHOIS 隱私">
          <NSwitch v-model:value="editForm.whois_privacy" />
        </NFormItem>
        <NFormItem label="年費">
          <NSpace>
            <NInputNumber
              v-model:value="(editForm as any).annual_cost"
              :min="0"
              :precision="2"
              style="width:120px"
            />
            <NSelect
              v-model:value="(editForm as any).currency"
              :options="[{label:'USD',value:'USD'},{label:'EUR',value:'EUR'},{label:'CNY',value:'CNY'},{label:'TWD',value:'TWD'}]"
              style="width:90px"
            />
          </NSpace>
        </NFormItem>
        <NFormItem label="費用固定">
          <NSwitch v-model:value="editForm.fee_fixed" />
        </NFormItem>
        <NFormItem label="用途">
          <NInput v-model:value="(editForm as any).purpose" clearable />
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

    <!-- Cost create modal -->
    <NModal
      v-model:show="showCostCreate"
      preset="card"
      title="新增費用記錄"
      style="width: 460px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px">
        <NFormItem label="費用類型" required>
          <NSelect v-model:value="(costForm as any).cost_type" :options="costTypeOptions" />
        </NFormItem>
        <NFormItem label="金額" required>
          <NSpace>
            <NInputNumber
              v-model:value="(costForm as any).amount"
              :min="0" :precision="2"
              style="width:120px"
            />
            <NSelect
              v-model:value="(costForm as any).currency"
              :options="costCurrencyOptions"
              style="width:90px"
            />
          </NSpace>
        </NFormItem>
        <NFormItem label="付款日">
          <NInput v-model:value="costForm.paid_at" placeholder="YYYY-MM-DD（選填）" clearable />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="costForm.notes" type="textarea" :rows="2" clearable />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCostCreate = false">取消</NButton>
          <NButton type="primary" :loading="costCreating" @click="handleCostCreate">新增</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- SSL create modal -->
    <NModal
      v-model:show="showSSLCreate"
      preset="card"
      title="手動新增 SSL 憑證記錄"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="110px">
        <NFormItem label="到期日" required>
          <NInput
            v-model:value="sslForm.expires_at"
            placeholder="YYYY-MM-DD"
            clearable
          />
        </NFormItem>
        <NFormItem label="簽發機構">
          <NInput v-model:value="sslForm.issuer" placeholder="例：Let's Encrypt" clearable />
        </NFormItem>
        <NFormItem label="憑證類型">
          <NSelect
            v-model:value="(sslForm as any).cert_type"
            :options="[{label:'DV',value:'dv'},{label:'OV',value:'ov'},{label:'EV',value:'ev'}]"
            clearable
            placeholder="選填"
          />
        </NFormItem>
        <NFormItem label="Serial Number">
          <NInput v-model:value="sslForm.serial_number" placeholder="選填" clearable />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="sslForm.notes" type="textarea" :rows="2" clearable />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showSSLCreate = false">取消</NButton>
          <NButton type="primary" :loading="sslCreating" @click="handleSSLCreate">新增</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Create provider record modal -->
    <NModal
      v-model:show="showProvCreate"
      preset="card"
      title="新增 DNS 記錄"
      style="width: 520px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="80px">
        <NFormItem label="類型" required>
          <NSelect v-model:value="(provForm as any).type" :options="provRecordTypes" style="width:120px" />
        </NFormItem>
        <NFormItem label="名稱" required>
          <NInput v-model:value="provForm.name" :placeholder="store.current?.fqdn ?? 'example.com'" />
        </NFormItem>
        <NFormItem label="值" required>
          <NInput v-model:value="provForm.content" placeholder="1.2.3.4 / cdn.example.com / ..." />
        </NFormItem>
        <NFormItem label="TTL">
          <NInputNumber v-model:value="(provForm as any).ttl" :min="0" style="width:120px" />
          <span style="margin-left:8px; font-size:12px; color:var(--text-muted)">0 或 1 = 自動</span>
        </NFormItem>
        <NFormItem v-if="provForm.type === 'MX' || provForm.type === 'SRV'" label="優先級">
          <NInputNumber v-model:value="(provForm as any).priority" :min="0" style="width:120px" />
        </NFormItem>
        <NFormItem label="Proxied">
          <NSwitch v-model:value="provForm.proxied" />
          <span style="margin-left:8px; font-size:12px; color:var(--text-muted)">Cloudflare CDN 代理</span>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showProvCreate = false">取消</NButton>
          <NButton type="primary" :loading="provCreating" @click="handleProvCreate">建立</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Initiate transfer modal -->
    <NModal
      v-model:show="showTransfer"
      preset="card"
      title="發起域名轉移"
      style="width: 440px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="110px">
        <NFormItem label="目標 Registrar">
          <NInput
            v-model:value="transferForm.gaining_registrar"
            placeholder="輸入目標 Registrar 名稱（選填）"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showTransfer = false">取消</NButton>
          <NButton type="primary" :loading="transferring" @click="submitTransfer">發起</NButton>
        </NSpace>
      </template>
    </NModal>
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
}
.detail-page__sidebar {
  width: 300px;
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
.history-timeline {
  padding-left: var(--space-4);
}
.tab-section {
  padding: 8px 0;
}
.section-title {
  font-size: 14px;
  font-weight: 600;
  margin: 16px 0 8px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
}
.transfer-actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}
/* ── Propagation grid ────────────────────────────────────────────────────── */
.prop-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
}
.prop-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px;
  background: var(--bg-surface);
}
.prop-card--auth {
  border-left: 3px solid var(--primary);
}
.prop-card--error {
  border-left: 3px solid var(--error, #d03050);
}
.prop-card__header {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-bottom: 8px;
}
.prop-card__label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}
.prop-card__meta {
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.prop-card__error {
  font-size: 12px;
  color: var(--error, #d03050);
  word-break: break-all;
}
.prop-card__empty {
  font-size: 12px;
  color: var(--text-muted);
}
.prop-card__records {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.prop-record {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.prop-record__value {
  font-family: var(--font-mono);
  font-size: 12px;
  word-break: break-all;
  flex: 1;
}
.prop-record__ttl {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}

/* ── Drift ───────────────────────────────────────────────────────────────── */
.drift-summary {
  display: flex;
  gap: 12px;
  margin-bottom: 10px;
  font-size: 13px;
  font-weight: 600;
}
.drift-stat.good { color: var(--success, #18a058); }
.drift-stat.bad  { color: var(--error,   #d03050); }
.drift-stat.warn { color: var(--warning, #f0a020); }

.dns-meta {
  font-size: 12px;
  color: var(--text-muted);
}
.dns-meta code {
  font-family: var(--font-mono);
  background: var(--bg-hover);
  padding: 1px 4px;
  border-radius: 3px;
}
</style>
