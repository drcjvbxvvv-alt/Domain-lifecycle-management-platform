<script setup lang="ts">
import { ref, computed, onMounted, watch, h } from 'vue'
import {
  NCard, NDescriptions, NDescriptionsItem, NButton, NSpace,
  NSelect, NSpin, NAlert, NTag, NText, NDivider, NPopconfirm,
  NDataTable, NEmpty, useMessage,
} from 'naive-ui'
import type { DataTableColumns, SelectOption } from 'naive-ui'
import { cdnBindingApi } from '@/api/cdnbinding'
import type { CDNBindingResponse, CDNDomainStatus, CDNBusinessType } from '@/api/cdnbinding'
import { useCDNStore } from '@/stores/cdn'

const props = defineProps<{ domainId: number }>()
const emit = defineEmits<{ (e: 'updated'): void }>()

const message = useMessage()
const cdnStore = useCDNStore()

const loading     = ref(false)
const binding     = ref(false)
const unbinding   = ref<number | null>(null)
const refreshing  = ref<number | null>(null)
const bindings    = ref<CDNBindingResponse[]>([])

// New binding form
const selectedAccountId   = ref<number | null>(null)
const selectedBusinessType = ref<CDNBusinessType>('web')

// ── Account options ────────────────────────────────────────────────────────────

const accountOptions = computed<SelectOption[]>(() =>
  cdnStore.allAccounts.map(a => ({
    label: `${a.account_name} (${cdnStore.providers.find(p => p.id === a.cdn_provider_id)?.name ?? 'CDN'})`,
    value: a.id,
    disabled: bindings.value.some(b => b.cdn_account_id === a.id),
  }))
)

const businessTypeOptions: SelectOption[] = [
  { label: '網頁加速 (web)', value: 'web' },
  { label: '下載加速 (download)', value: 'download' },
  { label: '媒體加速 (media)', value: 'media' },
]

// ── Status helpers ─────────────────────────────────────────────────────────────

function statusTagType(s: CDNDomainStatus): 'default' | 'info' | 'success' | 'error' | 'warning' {
  switch (s) {
    case 'online':       return 'success'
    case 'configuring':  return 'info'
    case 'checking':     return 'warning'
    default:             return 'default'  // offline
  }
}

function statusLabel(s: CDNDomainStatus): string {
  switch (s) {
    case 'online':       return '已上線'
    case 'offline':      return '未上線'
    case 'configuring':  return '配置中'
    case 'checking':     return '審核中'
    default:             return s
  }
}

function accountName(accountId: number): string {
  const a = cdnStore.allAccounts.find(a => a.id === accountId)
  if (!a) return `#${accountId}`
  const p = cdnStore.providers.find(p => p.id === a.cdn_provider_id)
  return `${a.account_name}${p ? ` (${p.name})` : ''}`
}

// ── Data loading ──────────────────────────────────────────────────────────────

async function loadBindings() {
  loading.value = true
  try {
    const res = await cdnBindingApi.list(props.domainId)
    bindings.value = res.data.items ?? []
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '載入 CDN 綁定失敗')
  } finally {
    loading.value = false
  }
}

// ── Bind ──────────────────────────────────────────────────────────────────────

async function bindAccount() {
  if (!selectedAccountId.value) return
  binding.value = true
  try {
    await cdnBindingApi.bind(props.domainId, {
      cdn_account_id: selectedAccountId.value,
      business_type: selectedBusinessType.value,
    })
    message.success('CDN 加速域名已建立')
    selectedAccountId.value = null
    await loadBindings()
    emit('updated')
  } catch (e: any) {
    const msg = e?.response?.data?.message ?? '綁定失敗'
    if (e?.response?.status === 409) {
      message.warning(msg)
    } else {
      message.error(msg)
    }
  } finally {
    binding.value = false
  }
}

// ── Unbind ────────────────────────────────────────────────────────────────────

async function unbindBinding(bindingId: number) {
  unbinding.value = bindingId
  try {
    await cdnBindingApi.unbind(props.domainId, bindingId)
    message.success('已解除 CDN 綁定')
    await loadBindings()
    emit('updated')
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '解除綁定失敗')
  } finally {
    unbinding.value = null
  }
}

// ── Refresh status ────────────────────────────────────────────────────────────

async function refreshBindingStatus(bindingId: number) {
  refreshing.value = bindingId
  try {
    await cdnBindingApi.refreshStatus(props.domainId, bindingId)
    message.success('狀態已刷新')
    await loadBindings()
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '刷新狀態失敗')
  } finally {
    refreshing.value = null
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────

const columns: DataTableColumns<CDNBindingResponse> = [
  {
    title: 'CDN 帳號',
    key: 'cdn_account_id',
    render: row => accountName(row.cdn_account_id),
  },
  {
    title: '業務類型',
    key: 'business_type',
    width: 100,
    render: row => {
      const map: Record<CDNBusinessType, string> = {
        web: '網頁',
        download: '下載',
        media: '媒體',
      }
      return map[row.business_type] ?? row.business_type
    },
  },
  {
    title: '狀態',
    key: 'status',
    width: 90,
    render: row => h(NTag, { type: statusTagType(row.status), size: 'small', bordered: false }, () => statusLabel(row.status)),
  },
  {
    title: 'CNAME',
    key: 'cdn_cname',
    render: row => row.cdn_cname
      ? h('code', { style: 'font-size:12px' }, row.cdn_cname)
      : h(NText, { depth: 3 }, () => '—'),
  },
  {
    title: '建立時間',
    key: 'created_at',
    width: 160,
    render: row => new Date(row.created_at).toLocaleString('zh-TW'),
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render: row => h(NSpace, { size: 'small' }, () => [
      h(NButton, {
        size: 'tiny',
        loading: refreshing.value === row.id,
        onClick: () => refreshBindingStatus(row.id),
      }, () => '刷新狀態'),
      h(NPopconfirm, {
        onPositiveClick: () => unbindBinding(row.id),
      }, {
        trigger: () => h(NButton, {
          size: 'tiny',
          type: 'error',
          ghost: true,
          loading: unbinding.value === row.id,
        }, () => '解除'),
        default: () => `確定解除 ${accountName(row.cdn_account_id)} 的 CDN 綁定？此操作將同時從 CDN 供應商刪除加速域名。`,
      }),
    ]),
  },
]

// ── Lifecycle ─────────────────────────────────────────────────────────────────

onMounted(async () => {
  await Promise.all([
    cdnStore.fetchList(),
    cdnStore.fetchAllAccounts(),
    loadBindings(),
  ])
})

watch(() => props.domainId, loadBindings)
</script>

<template>
  <NSpin :show="loading">
    <NSpace vertical :size="16">

      <!-- Bindings list -------------------------------------------------------- -->
      <NCard title="CDN 加速綁定" size="small">
        <template v-if="bindings.length > 0">
          <NDataTable
            :columns="columns"
            :data="bindings"
            :row-key="row => row.id"
            size="small"
            :pagination="false"
          />
        </template>
        <NEmpty v-else description="尚無 CDN 綁定" style="padding: 24px 0" />
      </NCard>

      <!-- CNAME hint ----------------------------------------------------------- -->
      <NAlert
        v-if="bindings.some(b => b.cdn_cname && b.status !== 'online')"
        type="info"
        title="DNS 設定提示"
      >
        <p>域名已在 CDN 供應商建立，請在 DNS 管理中新增 CNAME 記錄，將加速域名指向下方的 CNAME 值：</p>
        <ul style="margin: 8px 0; padding-left: 18px">
          <li v-for="b in bindings.filter(b => b.cdn_cname)" :key="b.id">
            <strong>{{ accountName(b.cdn_account_id) }}</strong>：
            <code style="user-select: all">{{ b.cdn_cname }}</code>
          </li>
        </ul>
      </NAlert>

      <!-- New binding form ----------------------------------------------------- -->
      <NCard title="新增 CDN 綁定" size="small">
        <NSpace align="center" :wrap="false" style="flex-wrap: wrap; gap: 8px">
          <NSelect
            v-model:value="selectedAccountId"
            :options="accountOptions"
            placeholder="選擇 CDN 帳號"
            filterable
            style="width: 280px"
          />
          <NSelect
            v-model:value="selectedBusinessType"
            :options="businessTypeOptions"
            style="width: 160px"
          />
          <NButton
            type="primary"
            :loading="binding"
            :disabled="!selectedAccountId"
            @click="bindAccount"
          >
            建立加速域名
          </NButton>
        </NSpace>
        <NText depth="3" style="display: block; margin-top: 8px; font-size: 12px">
          建立後，CDN 供應商會分配一個 CNAME，請在 DNS 設定中將域名指向該 CNAME。
        </NText>
      </NCard>

    </NSpace>
  </NSpin>
</template>
