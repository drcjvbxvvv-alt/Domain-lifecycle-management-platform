<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs, NTabPane, NDescriptions, NDescriptionsItem,
  NTimeline, NTimelineItem, NButton, NSpace, NModal, NForm,
  NFormItem, NInput, NInputNumber, NSelect, NSwitch, NDatePicker,
  useMessage,
} from 'naive-ui'
import { PageHeader, StatusTag, ConfirmModal, PageHint } from '@/components'
import { useDomainStore } from '@/stores/domain'
import { useRegistrarStore } from '@/stores/registrar'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import { domainApi } from '@/api/domain'
import type { DomainLifecycleHistoryEntry, UpdateDomainAssetRequest } from '@/types/domain'
import type { DomainLifecycleState, ApiResponse } from '@/types/common'
import type { SelectOption } from 'naive-ui'

const route    = useRoute()
const store    = useDomainStore()
const regStore = useRegistrarStore()
const dnsStore = useDNSProviderStore()
const message  = useMessage()

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

// ── Helper ────────────────────────────────────────────────────────────────────
function fmtDate(s: string | null | undefined): string {
  return s ? new Date(s).toLocaleDateString('zh-TW') : '-'
}

onMounted(async () => {
  await store.fetchOne(domainId)
  await refreshHistory()
  await Promise.all([regStore.fetchList(), dnsStore.fetchList()])
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
</style>
