<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import {
  NCard, NDescriptions, NDescriptionsItem, NButton, NSpace,
  NSelect, NSpin, NAlert, NTag, NText, NDivider, useMessage,
} from 'naive-ui'
import type { SelectOption } from 'naive-ui'
import { dnsBindingApi } from '@/api/dnsbinding'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import type { DNSBindingStatus, NSDelegationStatus } from '@/types/domain'

const props = defineProps<{ domainId: number }>()
const emit = defineEmits<{ (e: 'updated'): void }>()

const message = useMessage()
const providerStore = useDNSProviderStore()

const loading   = ref(false)
const saving    = ref(false)
const verifying = ref(false)
const status    = ref<DNSBindingStatus | null>(null)

// Selected provider in the dropdown (undefined = no change pending).
const selectedProviderId = ref<number | null | undefined>(undefined)

// ── Provider options ──────────────────────────────────────────────────────────

const providerOptions = computed<SelectOption[]>(() =>
  providerStore.providers.map(p => ({ label: p.name, value: p.id }))
)

// The currently bound provider id or null.
const currentProviderId = computed(() =>
  status.value?.dns_provider_id ?? null
)

// ── Delegation status helpers ─────────────────────────────────────────────────

function delegationTagType(s: NSDelegationStatus): 'default' | 'info' | 'success' | 'error' | 'warning' {
  switch (s) {
    case 'verified': return 'success'
    case 'pending':  return 'info'
    case 'mismatch': return 'error'
    default:         return 'default'
  }
}

function delegationLabel(s: NSDelegationStatus): string {
  switch (s) {
    case 'verified': return '已驗證'
    case 'pending':  return '等待中'
    case 'mismatch': return '不符合'
    default:         return '未設定'
  }
}

// ── Data loading ──────────────────────────────────────────────────────────────

async function loadStatus() {
  loading.value = true
  try {
    const res = await dnsBindingApi.getStatus(props.domainId)
    status.value = res.data.data
    selectedProviderId.value = status.value?.dns_provider_id ?? null
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '載入 NS 狀態失敗')
  } finally {
    loading.value = false
  }
}

// ── Save ──────────────────────────────────────────────────────────────────────

async function save() {
  saving.value = true
  try {
    // selectedProviderId.value is either a number (bind) or null (unbind).
    const res = await dnsBindingApi.bind(props.domainId, selectedProviderId.value ?? null)
    status.value = res.data.data
    selectedProviderId.value = status.value?.dns_provider_id ?? null
    message.success(selectedProviderId.value ? '已綁定 DNS Provider' : '已解除 DNS Provider 綁定')
    emit('updated')
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '儲存失敗')
  } finally {
    saving.value = false
  }
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

onMounted(async () => {
  await Promise.all([
    providerStore.fetchList(),
    loadStatus(),
  ])
})

watch(() => props.domainId, loadStatus)

async function triggerVerify() {
  verifying.value = true
  try {
    await dnsBindingApi.triggerVerify(props.domainId)
    message.success('NS 驗證任務已排入佇列，請稍後刷新頁面查看結果')
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '觸發驗證失敗')
  } finally {
    verifying.value = false
  }
}

// Derived: did the user change the selection?
const isDirty = computed(() => {
  const cur = status.value?.dns_provider_id ?? null
  const sel = selectedProviderId.value ?? null
  return cur !== sel
})
</script>

<template>
  <NSpin :show="loading">
    <NCard title="NS 委派設定" size="small">
      <NSpace vertical :size="16">

        <!-- Current status -------------------------------------------------- -->
        <NDescriptions v-if="status" bordered :column="2" size="small">
          <NDescriptionsItem label="DNS Provider">
            <span v-if="status.dns_provider_id">
              {{ providerStore.providers.find(p => p.id === status!.dns_provider_id)?.name ?? `#${status.dns_provider_id}` }}
            </span>
            <NText v-else depth="3">未設定</NText>
          </NDescriptionsItem>

          <NDescriptionsItem label="委派狀態">
            <NTag
              :type="delegationTagType(status.ns_delegation_status)"
              size="small"
              :bordered="false"
            >
              {{ delegationLabel(status.ns_delegation_status) }}
            </NTag>
          </NDescriptionsItem>

          <NDescriptionsItem v-if="status.expected_nameservers.length" label="預期 NS" :span="2">
            <NSpace>
              <NTag
                v-for="ns in status.expected_nameservers"
                :key="ns"
                size="small"
                type="info"
                :bordered="false"
              >{{ ns }}</NTag>
            </NSpace>
          </NDescriptionsItem>

          <NDescriptionsItem v-if="status.actual_nameservers.length" label="實際 NS" :span="2">
            <NSpace>
              <NTag
                v-for="ns in status.actual_nameservers"
                :key="ns"
                size="small"
                :type="status.ns_delegation_status === 'verified' ? 'success' : 'warning'"
                :bordered="false"
              >{{ ns }}</NTag>
            </NSpace>
          </NDescriptionsItem>

          <NDescriptionsItem v-if="status.ns_verified_at" label="驗證時間">
            {{ new Date(status.ns_verified_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>

          <NDescriptionsItem v-if="status.ns_last_checked_at" label="上次檢查">
            {{ new Date(status.ns_last_checked_at).toLocaleString('zh-TW') }}
          </NDescriptionsItem>
        </NDescriptions>

        <NAlert
          v-if="status?.ns_delegation_status === 'mismatch'"
          type="error"
          title="NS 不符合"
        >
          實際的 Nameserver 與 DNS Provider 提供的不符，請檢查域名的 NS 記錄是否正確委派。
        </NAlert>

        <NAlert
          v-if="status?.ns_delegation_status === 'pending'"
          type="info"
          title="等待 NS 傳播"
        >
          NS 委派已設定，正在等待 DNS 傳播。通常需要 24–48 小時。
        </NAlert>

        <!-- Edit ------------------------------------------------------------ -->
        <NDivider style="margin:0" />

        <NSpace align="center" justify="space-between" style="width:100%">
          <NSpace align="center">
            <NSelect
              v-model:value="selectedProviderId"
              :options="providerOptions"
              placeholder="選擇 DNS Provider"
              clearable
              style="width:280px"
            />
            <NButton
              type="primary"
              :loading="saving"
              :disabled="!isDirty"
              @click="save"
            >
              {{ selectedProviderId ? '儲存綁定' : '解除綁定' }}
            </NButton>
          </NSpace>

          <NButton
            v-if="status?.dns_provider_id && status?.ns_delegation_status !== 'verified'"
            :loading="verifying"
            @click="triggerVerify"
          >
            手動觸發驗證
          </NButton>
        </NSpace>

      </NSpace>
    </NCard>
  </NSpin>
</template>
