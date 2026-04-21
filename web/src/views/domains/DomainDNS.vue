<script setup lang="ts">
import { ref, computed, onMounted, watch, h } from 'vue'
import { useMessage, NButton, NDataTable, NTag, NSelect, NInput, NInputNumber,
         NModal, NAlert, NPopconfirm, NTooltip, NSpin, NForm, NFormItem,
         type DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import { useDNSRecordStore } from '@/stores/dnsrecord'
import type { ManagedRecord, ValidationError } from '@/types/dnsrecord'
import { validateRecord, planTotalChanges } from '@/types/dnsrecord'
import type { DomainPermissionLevel } from '@/types/permission'
import { hasPermission } from '@/types/permission'
import { dnsTemplateApi } from '@/api/dnstemplate'
import type { DNSTemplate } from '@/types/dnstemplate'

const props = defineProps<{
  domainId: number
  fqdn: string
  hasDnsProvider: boolean
  /** Caller's effective permission on this domain. Empty = no access. */
  myPermission?: DomainPermissionLevel | ''
  /** ISO timestamp of last detected drift (from domains.last_drift_at). */
  lastDriftAt?: string | null
}>()

/** Whether the current user can edit records (editor or above). */
const canEdit = () => hasPermission(props.myPermission ?? '', 'editor')
/** Whether the current user can apply plans (admin only). */
const canApply = () => hasPermission(props.myPermission ?? '', 'admin')

// ── Drift indicator ───────────────────────────────────────────────────────────
/** True if drift was detected within the last 24 hours. */
const hasDrift = computed(() => {
  if (!props.lastDriftAt) return false
  const age = Date.now() - new Date(props.lastDriftAt).getTime()
  return age < 24 * 60 * 60 * 1000
})

// ── Apply Template modal ──────────────────────────────────────────────────────
const showTemplateModal   = ref(false)
const templateLoading     = ref(false)
const templateApplying    = ref(false)
const templates           = ref<DNSTemplate[]>([])
const selectedTemplateId  = ref<number | null>(null)
const templateVars        = ref<Record<string, string>>({})

const selectedTemplate = computed(() =>
  templates.value.find(t => t.id === selectedTemplateId.value) ?? null
)
const templateVarKeys = computed(() =>
  selectedTemplate.value ? Object.keys(selectedTemplate.value.variables) : []
)

async function openTemplateModal() {
  showTemplateModal.value = true
  selectedTemplateId.value = null
  templateVars.value = {}
  templateLoading.value = true
  try {
    const res = await dnsTemplateApi.list()
    templates.value = res.data.items
  } catch {
    message.error('載入範本失敗')
  } finally {
    templateLoading.value = false
  }
}

function onTemplateSelect(id: number | null) {
  selectedTemplateId.value = id
  // Pre-fill variable map with empty strings for each key
  const tmpl = templates.value.find(t => t.id === id)
  templateVars.value = tmpl ? Object.fromEntries(Object.keys(tmpl.variables).map(k => [k, ''])) : {}
}

async function handleApplyTemplate() {
  if (!selectedTemplateId.value) { message.warning('請選擇一個範本'); return }
  // Check all required vars are filled
  const missing = templateVarKeys.value.filter(k => !templateVars.value[k]?.trim())
  if (missing.length) { message.warning(`請填入變數：${missing.join(', ')}`); return }

  templateApplying.value = true
  try {
    const res = await dnsTemplateApi.applyTemplate(props.domainId, selectedTemplateId.value, templateVars.value)
    const rendered = res.data.records
    // Stage each rendered record as a create
    for (const r of rendered) {
      dnsStore.stageCreate({
        type: r.type,
        name: r.name,
        content: r.content,
        ttl: r.ttl,
        priority: r.priority,
      })
    }
    message.success(`已暫存 ${rendered.length} 筆記錄，請在右上角套用計畫`)
    showTemplateModal.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '套用範本失敗')
  } finally {
    templateApplying.value = false
  }
}

const message  = useMessage()
const dnsStore = useDNSRecordStore()

// ── Type filter ───────────────────────────────────────────────────────────────

const typeFilter = ref<string>('ALL')
const typeOptions = [
  { label: '全部', value: 'ALL' },
  { label: 'A',    value: 'A' },
  { label: 'AAAA', value: 'AAAA' },
  { label: 'CNAME', value: 'CNAME' },
  { label: 'MX',   value: 'MX' },
  { label: 'TXT',  value: 'TXT' },
  { label: 'NS',   value: 'NS' },
  { label: 'SRV',  value: 'SRV' },
  { label: 'CAA',  value: 'CAA' },
  { label: 'PTR',  value: 'PTR' },
]

const filteredRecords = computed(() => {
  const all = dnsStore.stagedRecords
  if (typeFilter.value === 'ALL') return all
  return all.filter(r => r.type.toUpperCase() === typeFilter.value)
})

// ── Record type color map ─────────────────────────────────────────────────────

const typeColor: Record<string, string> = {
  A: 'success', AAAA: 'info', CNAME: 'warning',
  MX: 'error', TXT: 'default', NS: 'primary',
  SRV: 'warning', CAA: 'error', PTR: 'default',
}

// ── Inline create form ────────────────────────────────────────────────────────

const showCreateForm = ref(false)
const createForm = ref<Partial<ManagedRecord>>({
  type: 'A', name: '', content: '', ttl: 300, priority: undefined,
})
const createErrors = ref<ValidationError[]>([])

function createFieldError(field: string): string | undefined {
  return createErrors.value.find(e => e.field === field)?.message
}

const recordTypeOptionsForForm = [
  'A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR',
].map(t => ({ label: t, value: t }))

function handleCreateSubmit() {
  createErrors.value = validateRecord(createForm.value)
  if (createErrors.value.length > 0) return

  dnsStore.stageCreate({
    type: createForm.value.type!,
    name: createForm.value.name!,
    content: createForm.value.content!,
    ttl: createForm.value.ttl ?? 300,
    priority: createForm.value.priority,
  })

  // Reset form
  createForm.value = { type: 'A', name: '', content: '', ttl: 300 }
  createErrors.value = []
  showCreateForm.value = false
  message.success('記錄已暫存，點擊「套用變更」以提交到 Provider')
}

// ── Inline edit ───────────────────────────────────────────────────────────────

const editingId = ref<string | null>(null)
const editForm  = ref<Partial<ManagedRecord>>({})
const editErrors = ref<ValidationError[]>([])

function editFieldError(field: string): string | undefined {
  return editErrors.value.find(e => e.field === field)?.message
}

function startEdit(record: ManagedRecord) {
  editingId.value = record.id ?? null
  editForm.value = { ...record }
  editErrors.value = []
}

function cancelEdit() {
  editingId.value = null
  editForm.value = {}
  editErrors.value = []
}

function confirmEdit() {
  if (!editingId.value) return
  editErrors.value = validateRecord(editForm.value)
  if (editErrors.value.length > 0) return

  dnsStore.stageUpdate(editingId.value, editForm.value)
  cancelEdit()
}

// ── Stage delete ─────────────────────────────────────────────────────────────

function handleStageDelete(record: ManagedRecord) {
  if (record._action === 'delete') {
    // Undo the staged delete
    dnsStore.stageUpdate(record.id!, { _action: undefined })
  } else if (record.id) {
    dnsStore.stageDelete(record.id)
  } else if (record._tempId) {
    dnsStore.removeStagedCreate(record._tempId)
  }
}

// ── Plan modal ────────────────────────────────────────────────────────────────

const showPlanModal = ref(false)

function openPlanModal() {
  if (planTotalChanges(dnsStore.plan) === 0) {
    message.warning('沒有待套用的變更')
    return
  }
  showPlanModal.value = true
}

// ── Apply ─────────────────────────────────────────────────────────────────────

async function handleApply(force = false) {
  if (!force && !dnsStore.safetyResult.passed) {
    message.warning('安全閾值超標，請確認後選擇強制套用')
    return
  }

  const result = await dnsStore.applyPlan(props.domainId, props.fqdn)
  if (result.success) {
    showPlanModal.value = false
    message.success(`成功套用 ${planTotalChanges(dnsStore.plan)} 項變更`)
  } else {
    message.error(result.error ?? '套用失敗')
  }
}

// ── Discard ───────────────────────────────────────────────────────────────────

function handleDiscard() {
  dnsStore.discardChanges()
  message.info('已捨棄所有暫存變更')
}

// ── Load ──────────────────────────────────────────────────────────────────────

async function loadRecords() {
  if (!props.hasDnsProvider) return
  try {
    await dnsStore.fetchRecords(props.domainId, props.fqdn)
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '載入記錄失敗')
  }
}

onMounted(loadRecords)
watch(() => props.domainId, loadRecords)

// ── Table columns ─────────────────────────────────────────────────────────────

const columns = computed<DataTableColumns<ManagedRecord>>(() => [
  {
    title: '狀態', key: '_action', width: 60,
    render: (row): VNodeChild => {
      if (row._action === 'create') return h(NTag, { type: 'success', size: 'small', bordered: false }, { default: () => '新增' })
      if (row._action === 'update') return h(NTag, { type: 'warning', size: 'small', bordered: false }, { default: () => '修改' })
      if (row._action === 'delete') return h(NTag, { type: 'error',   size: 'small', bordered: false }, { default: () => '刪除' })
      return h('span', { style: 'color:var(--text-muted);font-size:11px' }, '—')
    },
  },
  {
    title: '類型', key: 'type', width: 70, sorter: 'default',
    render: (row): VNodeChild =>
      h(NTag, { type: (typeColor[row.type] || 'default') as any, size: 'small', bordered: false }, { default: () => row.type }),
  },
  {
    title: '名稱', key: 'name', sorter: 'default', minWidth: 150,
    render: (row): VNodeChild => {
      if (editingId.value === row.id) {
        return h(NInput, {
          value: editForm.value.name,
          size: 'small',
          status: editFieldError('name') ? 'error' : undefined,
          onUpdateValue: (v: string) => { editForm.value.name = v },
        })
      }
      const style = row._action === 'delete' ? 'text-decoration:line-through;color:var(--text-muted)' : ''
      return h('span', { style, class: 'dns-name' }, row.name)
    },
  },
  {
    title: '值', key: 'content', minWidth: 180, ellipsis: { tooltip: true },
    render: (row): VNodeChild => {
      if (editingId.value === row.id) {
        return h(NInput, {
          value: editForm.value.content,
          size: 'small',
          status: editFieldError('content') ? 'error' : undefined,
          onUpdateValue: (v: string) => { editForm.value.content = v },
        })
      }
      const style = row._action === 'delete'
        ? 'text-decoration:line-through;color:var(--text-muted);font-size:12px;font-family:var(--font-mono)'
        : 'font-size:12px;font-family:var(--font-mono)'
      return h('code', { style }, row.content)
    },
  },
  {
    title: 'TTL', key: 'ttl', width: 80,
    render: (row): VNodeChild => {
      if (editingId.value === row.id) {
        return h(NInputNumber, {
          value: editForm.value.ttl,
          size: 'small', min: 1, style: 'width:70px',
          onUpdateValue: (v: number | null) => { editForm.value.ttl = v ?? 300 },
        })
      }
      return h('span', { style: 'font-family:var(--font-mono);font-size:12px' },
        row.ttl === 1 ? 'Auto' : `${row.ttl}s`)
    },
  },
  {
    title: '優先級', key: 'priority', width: 72,
    render: (row): VNodeChild => {
      if (editingId.value === row.id && (editForm.value.type === 'MX' || editForm.value.type === 'SRV')) {
        return h(NInputNumber, {
          value: editForm.value.priority,
          size: 'small', min: 0, style: 'width:62px',
          onUpdateValue: (v: number | null) => { editForm.value.priority = v ?? undefined },
        })
      }
      return h('span', {}, row.priority != null ? String(row.priority) : '—')
    },
  },
  {
    title: '操作', key: '_ops', width: 130, fixed: 'right',
    render: (row): VNodeChild => {
      if (editingId.value === row.id) {
        return h('div', { style: 'display:flex;gap:4px' }, [
          h(NButton, { size: 'small', type: 'primary', onClick: confirmEdit }, { default: () => '確認' }),
          h(NButton, { size: 'small', onClick: cancelEdit }, { default: () => '取消' }),
        ])
      }

      const btns = []

      // Edit/delete only available to editors and above
      if (canEdit()) {
        // Can't edit a delete-staged record
        if (row._action !== 'delete' && row.id) {
          btns.push(h(NButton, {
            size: 'small', quaternary: true,
            onClick: () => startEdit(row),
          }, { default: () => '編輯' }))
        }

        if (row._action === 'delete') {
          btns.push(h(NButton, {
            size: 'small', quaternary: true, type: 'warning',
            onClick: () => handleStageDelete(row),
          }, { default: () => '復原' }))
        } else {
          btns.push(h(NPopconfirm,
            { onPositiveClick: () => handleStageDelete(row) },
            {
              trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => '刪除' }),
              default: () => `暫存刪除 ${row.type} 記錄？（套用前可復原）`,
            },
          ))
        }
      }

      return h('div', { style: 'display:flex;gap:4px' }, btns)
    },
  },
])

// ── Plan modal helpers ────────────────────────────────────────────────────────

function changeLabel(action: string) {
  return action === 'create' ? '新增' : action === 'update' ? '修改' : '刪除'
}
function changeType(action: string): 'success' | 'warning' | 'error' {
  return action === 'create' ? 'success' : action === 'update' ? 'warning' : 'error'
}
</script>

<template>
  <div class="domain-dns">
    <!-- No provider warning -->
    <template v-if="!hasDnsProvider">
      <NAlert type="warning" style="margin-bottom:12px">
        此域名尚未設定 DNS Provider，請先在域名設定中綁定 Cloudflare 等供應商。
      </NAlert>
    </template>

    <template v-else>
      <!-- Drift indicator banner -->
      <NAlert v-if="hasDrift" type="warning" style="margin-bottom:12px">
        ⚠️ Drift 偵測到 DNS 記錄異動（最近偵測時間：{{ lastDriftAt ? new Date(lastDriftAt).toLocaleString('zh-TW') : '' }}）
        — 請確認 DNS 供應商側的記錄是否符合預期。
      </NAlert>

      <!-- Toolbar -->
      <div class="dns-toolbar">
        <NSelect
          v-model:value="typeFilter"
          :options="typeOptions"
          size="small"
          style="width:100px"
        />
        <NButton
          size="small"
          :loading="dnsStore.loading"
          @click="loadRecords"
        >
          重新載入
        </NButton>
        <NButton
          v-if="canEdit()"
          type="primary"
          size="small"
          @click="showCreateForm = true"
        >
          + 新增記錄
        </NButton>
        <NButton
          v-if="canEdit()"
          size="small"
          @click="openTemplateModal"
        >
          套用範本
        </NButton>

        <div style="flex:1" />

        <!-- Pending changes indicator (editor can stage; apply requires admin) -->
        <template v-if="dnsStore.hasPendingChanges && canEdit()">
          <NTag type="warning" size="small" :bordered="false">
            {{ dnsStore.pendingChanges.length }} 項待套用
          </NTag>
          <NButton size="small" @click="handleDiscard">捨棄全部</NButton>
          <NButton v-if="canApply()" size="small" type="primary" @click="openPlanModal">
            預覽 &amp; 套用
          </NButton>
          <NTooltip v-else trigger="hover">
            <template #trigger>
              <NButton size="small" type="primary" disabled>預覽 &amp; 套用</NButton>
            </template>
            需要域名 admin 權限才能套用變更
          </NTooltip>
        </template>
      </div>

      <!-- Create form -->
      <div v-if="showCreateForm" class="dns-create-form">
        <div class="dns-create-form__title">新增 DNS 記錄（暫存）</div>
        <div class="dns-create-form__fields">
          <div class="dns-field">
            <label class="dns-field__label">類型</label>
            <NSelect
              v-model:value="createForm.type"
              :options="recordTypeOptionsForForm"
              size="small"
              style="width:100px"
            />
          </div>
          <div class="dns-field">
            <label class="dns-field__label">名稱</label>
            <NInput
              v-model:value="createForm.name"
              size="small"
              placeholder="@ 或 sub.example.com"
              :status="createFieldError('name') ? 'error' : undefined"
            />
            <span v-if="createFieldError('name')" class="dns-field__error">{{ createFieldError('name') }}</span>
          </div>
          <div class="dns-field" style="flex:2">
            <label class="dns-field__label">值</label>
            <NInput
              v-model:value="createForm.content"
              size="small"
              placeholder="記錄值"
              :status="createFieldError('content') ? 'error' : undefined"
            />
            <span v-if="createFieldError('content')" class="dns-field__error">{{ createFieldError('content') }}</span>
          </div>
          <div class="dns-field" style="width:90px">
            <label class="dns-field__label">TTL</label>
            <NInputNumber
              v-model:value="createForm.ttl"
              size="small"
              :min="1"
              :status="createFieldError('ttl') ? 'error' : undefined"
            />
          </div>
          <div v-if="createForm.type === 'MX' || createForm.type === 'SRV'" class="dns-field" style="width:80px">
            <label class="dns-field__label">優先級</label>
            <NInputNumber
              v-model:value="createForm.priority"
              size="small"
              :min="0"
              :status="createFieldError('priority') ? 'error' : undefined"
            />
            <span v-if="createFieldError('priority')" class="dns-field__error">{{ createFieldError('priority') }}</span>
          </div>
        </div>
        <div class="dns-create-form__actions">
          <NButton size="small" type="primary" @click="handleCreateSubmit">暫存</NButton>
          <NButton size="small" @click="showCreateForm = false; createErrors = []">取消</NButton>
        </div>
      </div>

      <!-- Record table -->
      <NSpin :show="dnsStore.loading">
        <NDataTable
          v-if="filteredRecords.length > 0 || dnsStore.loading"
          :columns="columns"
          :data="filteredRecords"
          :row-key="(r: ManagedRecord) => r.id ?? r._tempId ?? `${r.type}-${r.name}-${r.content}`"
          :row-class-name="(r: ManagedRecord) => r._action === 'delete' ? 'row-delete' : r._action === 'create' ? 'row-create' : r._action === 'update' ? 'row-update' : ''"
          size="small"
          :max-height="480"
          scroll-x="800"
          striped
        />
        <div
          v-else
          style="padding:32px; text-align:center; color:var(--text-muted); font-size:13px;"
        >
          尚無 DNS 記錄。點擊「新增記錄」新增第一筆。
        </div>
      </NSpin>
    </template>

    <!-- Plan preview modal -->
    <NModal v-model:show="showPlanModal" style="max-width:680px" preset="card" title="變更預覽 (Plan)">
      <div class="plan-modal">
        <!-- Safety status -->
        <div class="plan-safety" :class="{ 'plan-safety--warn': !dnsStore.safetyResult.passed }">
          <span class="plan-safety__icon">{{ dnsStore.safetyResult.passed ? '✅' : '⚠️' }}</span>
          <span class="plan-safety__text">
            <template v-if="dnsStore.safetyResult.passed">安全檢查通過</template>
            <template v-else>安全閾值超標：{{ dnsStore.safetyResult.reason }}</template>
          </span>
          <span class="plan-safety__detail">
            更新 {{ Math.round(dnsStore.safetyResult.update_pct * 100) }}%
            ・刪除 {{ Math.round(dnsStore.safetyResult.delete_pct * 100) }}%
            （閾值 {{ Math.round(dnsStore.safetyResult.update_threshold * 100) }}% / {{ Math.round(dnsStore.safetyResult.delete_threshold * 100) }}%）
            ・共 {{ dnsStore.safetyResult.existing_count }} 筆現有記錄
          </span>
        </div>

        <!-- Summary -->
        <div class="plan-summary">
          <NTag type="success" :bordered="false" size="small" v-if="dnsStore.plan.creates.length > 0">
            +{{ dnsStore.plan.creates.length }} 新增
          </NTag>
          <NTag type="warning" :bordered="false" size="small" v-if="dnsStore.plan.updates.length > 0">
            ~{{ dnsStore.plan.updates.length }} 修改
          </NTag>
          <NTag type="error" :bordered="false" size="small" v-if="dnsStore.plan.deletes.length > 0">
            -{{ dnsStore.plan.deletes.length }} 刪除
          </NTag>
        </div>

        <!-- Change list -->
        <div class="plan-changes">
          <div
            v-for="(change, idx) in [...dnsStore.plan.creates, ...dnsStore.plan.updates, ...dnsStore.plan.deletes]"
            :key="idx"
            class="plan-change"
            :class="`plan-change--${change.action}`"
          >
            <NTag :type="changeType(change.action)" size="small" :bordered="false" style="min-width:40px">
              {{ changeLabel(change.action) }}
            </NTag>
            <div class="plan-change__detail">
              <template v-if="change.action === 'create' && change.after">
                <span class="plan-rec-type">{{ change.after.type }}</span>
                <code class="plan-rec-name">{{ change.after.name }}</code>
                <span class="plan-rec-arrow">→</span>
                <code class="plan-rec-value">{{ change.after.content }}</code>
                <span class="plan-rec-ttl">TTL {{ change.after.ttl }}</span>
              </template>
              <template v-else-if="change.action === 'delete' && change.before">
                <span class="plan-rec-type">{{ change.before.type }}</span>
                <code class="plan-rec-name">{{ change.before.name }}</code>
                <span class="plan-rec-arrow">→</span>
                <code class="plan-rec-value plan-rec-value--delete">{{ change.before.content }}</code>
              </template>
              <template v-else-if="change.action === 'update' && change.before && change.after">
                <span class="plan-rec-type">{{ change.after.type }}</span>
                <code class="plan-rec-name">{{ change.after.name }}</code>
                <span class="plan-rec-arrow">：</span>
                <code class="plan-rec-value plan-rec-value--before">{{ change.before.content }}</code>
                <span class="plan-rec-arrow">→</span>
                <code class="plan-rec-value">{{ change.after.content }}</code>
              </template>
            </div>
          </div>
        </div>

        <!-- Actions -->
        <div class="plan-actions">
          <NButton @click="showPlanModal = false">取消</NButton>
          <template v-if="dnsStore.safetyResult.passed">
            <NButton
              type="primary"
              :loading="dnsStore.applying"
              @click="handleApply(false)"
            >
              套用變更
            </NButton>
          </template>
          <template v-else>
            <NTooltip>
              <template #trigger>
                <NButton
                  type="error"
                  :loading="dnsStore.applying"
                  @click="handleApply(true)"
                >
                  強制套用
                </NButton>
              </template>
              安全閾值超標，強制套用可能導致大量記錄變更。
            </NTooltip>
          </template>
        </div>
      </div>
    </NModal>

    <!-- Apply Template modal -->
    <NModal v-model:show="showTemplateModal" preset="card" title="套用 DNS 範本"
            style="width:520px;max-width:95vw" :mask-closable="false">
      <NSpin :show="templateLoading">
        <NForm label-placement="top" :show-feedback="false">
          <NFormItem label="選擇範本">
            <NSelect
              :value="selectedTemplateId"
              :options="templates.map(t => ({ label: `${t.name} (${t.record_count} 筆記錄)`, value: t.id }))"
              placeholder="請選擇範本"
              clearable
              @update:value="onTemplateSelect"
            />
          </NFormItem>

          <!-- Variable inputs — only shown when template is selected -->
          <template v-if="selectedTemplate && templateVarKeys.length > 0">
            <div style="margin-bottom:8px;font-weight:500;font-size:13px">填入變數值</div>
            <NFormItem v-for="varKey in templateVarKeys" :key="varKey" :label="`{{${varKey}}}`">
              <NInput
                v-model:value="templateVars[varKey]"
                :placeholder="`請輸入 ${varKey} 的值`"
                size="small"
              />
            </NFormItem>
          </template>

          <!-- Preview of records this template will create -->
          <template v-if="selectedTemplate">
            <div style="margin-top:12px;margin-bottom:6px;font-weight:500;font-size:13px">
              範本記錄預覽（{{ selectedTemplate.record_count }} 筆）
            </div>
            <div v-for="(r, i) in selectedTemplate.records" :key="i"
                 style="font-size:12px;font-family:monospace;color:#555;margin-bottom:2px">
              <NTag size="small" :bordered="false" type="info" style="margin-right:4px">{{ r.type }}</NTag>
              {{ r.name }} → {{ r.content }}
              <span style="color:#999;margin-left:4px">TTL {{ r.ttl }}</span>
            </div>
          </template>
        </NForm>

        <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:16px">
          <NButton @click="showTemplateModal = false">取消</NButton>
          <NButton
            type="primary"
            :loading="templateApplying"
            :disabled="!selectedTemplateId"
            @click="handleApplyTemplate"
          >
            暫存記錄
          </NButton>
        </div>
      </NSpin>
    </NModal>
  </div>
</template>

<style scoped>
.domain-dns {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* Toolbar */
.dns-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* Create form */
.dns-create-form {
  background: var(--card-color);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 12px;
}
.dns-create-form__title {
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 10px;
  color: var(--text-color-1);
}
.dns-create-form__fields {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: flex-end;
}
.dns-create-form__actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}
.dns-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 100px;
}
.dns-field__label {
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 500;
}
.dns-field__error {
  font-size: 11px;
  color: var(--error-color);
}

/* Table row states */
:deep(.row-create td) { background: rgba(74, 222, 128, 0.05) !important; }
:deep(.row-update td) { background: rgba(251, 191, 36, 0.05) !important; }
:deep(.row-delete td) { background: rgba(248, 113, 113, 0.05) !important; opacity: 0.7; }

.dns-name {
  font-family: var(--font-mono);
  font-size: 12px;
}

/* Plan modal */
.plan-modal {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.plan-safety {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  background: rgba(74, 222, 128, 0.08);
  border: 1px solid rgba(74, 222, 128, 0.3);
  flex-wrap: wrap;
}
.plan-safety--warn {
  background: rgba(251, 191, 36, 0.08);
  border-color: rgba(251, 191, 36, 0.4);
}
.plan-safety__icon { font-size: 16px; }
.plan-safety__text { font-size: 13px; font-weight: 500; }
.plan-safety__detail { font-size: 11px; color: var(--text-muted); margin-left: auto; }

.plan-summary {
  display: flex;
  gap: 6px;
}

.plan-changes {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 320px;
  overflow-y: auto;
}
.plan-change {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  font-size: 12px;
}
.plan-change--create { background: rgba(74, 222, 128, 0.06); }
.plan-change--update { background: rgba(251, 191, 36, 0.06); }
.plan-change--delete { background: rgba(248, 113, 113, 0.06); }

.plan-change__detail {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  min-width: 0;
}
.plan-rec-type {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  min-width: 36px;
}
.plan-rec-name {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--text-color-1);
}
.plan-rec-arrow { color: var(--text-muted); }
.plan-rec-value {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--text-color-2);
  word-break: break-all;
}
.plan-rec-value--before {
  text-decoration: line-through;
  color: var(--text-muted);
}
.plan-rec-value--delete {
  text-decoration: line-through;
  color: var(--error-color);
}
.plan-rec-ttl {
  font-size: 11px;
  color: var(--text-muted);
}

.plan-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 4px;
  border-top: 1px solid var(--border-color);
}
</style>
