<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NTag, NModal, NForm, NFormItem,
  NInput, NSelect, NSwitch, NDatePicker, NPopconfirm,
  useMessage,
} from 'naive-ui'
import type { SelectOption } from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { maintenanceApi } from '@/api/maintenance'
import type {
  MaintenanceWindowResponse,
  MaintenanceWindowWithTargets,
  CreateMaintenanceRequest,
  MaintenanceStrategy,
} from '@/types/maintenance'

const message = useMessage()

// ── State ──────────────────────────────────────────────────────────────────────

const loading  = ref(false)
const windows  = ref<MaintenanceWindowResponse[]>([])
const showModal = ref(false)
const editing   = ref<MaintenanceWindowWithTargets | null>(null)
const saving    = ref(false)

// Form model
const form = ref<{
  title: string
  description: string
  strategy: MaintenanceStrategy
  active: boolean
  // single
  start_at: number | null    // timestamp (ms) for NDatePicker
  end_at: number | null
  // recurring_weekly
  weekdays: number[]
  start_time: string
  duration_minutes: number
  timezone: string
  // recurring_monthly
  day_of_month: number
  // cron
  cron_expression: string
}>({
  title: '',
  description: '',
  strategy: 'single',
  active: true,
  start_at: null,
  end_at: null,
  weekdays: [],
  start_time: '02:00',
  duration_minutes: 120,
  timezone: 'UTC',
  day_of_month: 1,
  cron_expression: '0 2 * * 1',
})

// ── Options ────────────────────────────────────────────────────────────────────

const strategyOptions: SelectOption[] = [
  { label: '單次 (Single)', value: 'single' },
  { label: '每週重複', value: 'recurring_weekly' },
  { label: '每月重複', value: 'recurring_monthly' },
  { label: 'Cron 表達式', value: 'cron' },
]

const weekdayOptions: SelectOption[] = [
  { label: '週日', value: 0 },
  { label: '週一', value: 1 },
  { label: '週二', value: 2 },
  { label: '週三', value: 3 },
  { label: '週四', value: 4 },
  { label: '週五', value: 5 },
  { label: '週六', value: 6 },
]

const timezoneOptions: SelectOption[] = [
  { label: 'UTC', value: 'UTC' },
  { label: 'Asia/Taipei (UTC+8)', value: 'Asia/Taipei' },
  { label: 'Asia/Shanghai (UTC+8)', value: 'Asia/Shanghai' },
  { label: 'Asia/Tokyo (UTC+9)', value: 'Asia/Tokyo' },
  { label: 'America/New_York (UTC-5)', value: 'America/New_York' },
  { label: 'Europe/London (UTC±0/+1)', value: 'Europe/London' },
]

// ── Data fetch ─────────────────────────────────────────────────────────────────

async function fetchList() {
  loading.value = true
  try {
    const res = await maintenanceApi.list() as any
    windows.value = res?.data?.items ?? []
  } catch {
    message.error('載入維護視窗失敗')
  } finally {
    loading.value = false
  }
}

// ── Modal helpers ──────────────────────────────────────────────────────────────

function openCreate() {
  editing.value = null
  resetForm()
  showModal.value = true
}

async function openEdit(id: number) {
  try {
    const res = await maintenanceApi.get(id) as any
    const w: MaintenanceWindowWithTargets = res?.data
    editing.value = w
    form.value.title       = w.title
    form.value.description = w.description ?? ''
    form.value.strategy    = w.strategy
    form.value.active      = w.active
    form.value.start_at    = w.start_at ? new Date(w.start_at).getTime() : null
    form.value.end_at      = w.end_at   ? new Date(w.end_at).getTime()   : null
    if (w.recurrence) {
      const r = w.recurrence as any
      form.value.weekdays         = r.weekdays         ?? []
      form.value.start_time       = r.start_time       ?? '02:00'
      form.value.duration_minutes = r.duration_minutes ?? 120
      form.value.timezone         = r.timezone         ?? 'UTC'
      form.value.day_of_month     = r.day_of_month     ?? 1
      form.value.cron_expression  = r.expression       ?? '0 2 * * 1'
    }
    showModal.value = true
  } catch {
    message.error('載入維護視窗詳情失敗')
  }
}

function resetForm() {
  form.value = {
    title: '', description: '', strategy: 'single', active: true,
    start_at: null, end_at: null,
    weekdays: [], start_time: '02:00', duration_minutes: 120,
    timezone: 'UTC', day_of_month: 1, cron_expression: '0 2 * * 1',
  }
}

function buildPayload(): CreateMaintenanceRequest {
  const payload: CreateMaintenanceRequest = {
    title:       form.value.title,
    description: form.value.description || undefined,
    strategy:    form.value.strategy,
    active:      form.value.active,
  }
  if (form.value.strategy === 'single') {
    if (form.value.start_at) payload.start_at = new Date(form.value.start_at).toISOString()
    if (form.value.end_at)   payload.end_at   = new Date(form.value.end_at).toISOString()
  } else if (form.value.strategy === 'recurring_weekly') {
    payload.recurrence = {
      weekdays:         form.value.weekdays,
      start_time:       form.value.start_time,
      duration_minutes: form.value.duration_minutes,
      timezone:         form.value.timezone,
    }
  } else if (form.value.strategy === 'recurring_monthly') {
    payload.recurrence = {
      day_of_month:     form.value.day_of_month,
      start_time:       form.value.start_time,
      duration_minutes: form.value.duration_minutes,
      timezone:         form.value.timezone,
    }
  } else if (form.value.strategy === 'cron') {
    payload.recurrence = {
      expression:       form.value.cron_expression,
      duration_minutes: form.value.duration_minutes,
      timezone:         form.value.timezone,
    }
  }
  return payload
}

async function save() {
  if (!form.value.title.trim()) {
    message.warning('請輸入標題')
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    if (editing.value) {
      await maintenanceApi.update(editing.value.id, payload)
      message.success('更新成功')
    } else {
      await maintenanceApi.create(payload)
      message.success('建立成功')
    }
    showModal.value = false
    await fetchList()
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '儲存失敗')
  } finally {
    saving.value = false
  }
}

async function remove(id: number) {
  try {
    await maintenanceApi.delete(id)
    message.success('已刪除')
    windows.value = windows.value.filter(w => w.id !== id)
  } catch {
    message.error('刪除失敗')
  }
}

// ── Columns ────────────────────────────────────────────────────────────────────

const strategyLabel: Record<MaintenanceStrategy, string> = {
  single:             '單次',
  recurring_weekly:   '每週',
  recurring_monthly:  '每月',
  cron:               'Cron',
}

const columns = computed((): DataTableColumns<MaintenanceWindowResponse> => [
  {
    title: '標題', key: 'title', ellipsis: { tooltip: true },
  },
  {
    title: '類型', key: 'strategy', width: 90,
    render: (row): VNodeChild =>
      h(NTag, { size: 'small', type: 'info' }, { default: () => strategyLabel[row.strategy] ?? row.strategy }),
  },
  {
    title: '時間範圍 / 排程', key: 'schedule', ellipsis: { tooltip: true },
    render: (row) => {
      if (row.strategy === 'single') {
        const s = row.start_at ? new Date(row.start_at).toLocaleString('zh-TW') : '-'
        const e = row.end_at   ? new Date(row.end_at).toLocaleString('zh-TW')   : '-'
        return `${s} ~ ${e}`
      }
      const r = row.recurrence as any
      if (!r) return '-'
      if (row.strategy === 'recurring_weekly') return `每週 ${(r.weekdays ?? []).join('/')} ${r.start_time} (${r.duration_minutes}min)`
      if (row.strategy === 'recurring_monthly') return `每月 ${r.day_of_month}日 ${r.start_time} (${r.duration_minutes}min)`
      if (row.strategy === 'cron') return `${r.expression} (${r.duration_minutes}min)`
      return '-'
    },
  },
  {
    title: '狀態', key: 'active', width: 80,
    render: (row): VNodeChild =>
      h(NTag, { type: row.active ? 'success' : 'default', size: 'small' },
        { default: () => row.active ? '啟用' : '停用' }),
  },
  {
    title: '操作', key: 'actions', width: 130,
    render: (row): VNodeChild =>
      h(NSpace, { size: 'small' }, {
        default: () => [
          h(NButton, { size: 'small', onClick: () => openEdit(row.id) }, { default: () => '編輯' }),
          h(NPopconfirm, { onPositiveClick: () => remove(row.id) }, {
            trigger: () => h(NButton, { size: 'small', type: 'error' }, { default: () => '刪除' }),
            default: () => '確定刪除此維護視窗？',
          }),
        ],
      }),
  },
])

// ── Lifecycle ──────────────────────────────────────────────────────────────────

onMounted(fetchList)
</script>

<template>
  <div>
    <PageHeader title="維護視窗管理">
      <template #actions>
        <NButton type="primary" @click="openCreate">新增維護視窗</NButton>
      </template>
    </PageHeader>

    <AppTable
      :columns="columns"
      :data="windows"
      :loading="loading"
      :row-key="(row) => row.id"
    />

    <!-- Create / Edit Modal -->
    <NModal
      v-model:show="showModal"
      :title="editing ? '編輯維護視窗' : '新增維護視窗'"
      preset="card"
      style="width: 560px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px">
        <NFormItem label="標題" required>
          <NInput v-model:value="form.title" placeholder="例：Nginx 升級維護" />
        </NFormItem>
        <NFormItem label="說明">
          <NInput v-model:value="form.description" type="textarea" :rows="2" placeholder="選填" />
        </NFormItem>
        <NFormItem label="排程類型">
          <NSelect v-model:value="form.strategy" :options="strategyOptions" style="width:220px" />
        </NFormItem>

        <!-- Single: date range picker -->
        <template v-if="form.strategy === 'single'">
          <NFormItem label="開始時間" required>
            <NDatePicker
              v-model:value="(form.start_at as any)"
              type="datetime"
              clearable
              style="width:100%"
            />
          </NFormItem>
          <NFormItem label="結束時間" required>
            <NDatePicker
              v-model:value="(form.end_at as any)"
              type="datetime"
              clearable
              style="width:100%"
            />
          </NFormItem>
        </template>

        <!-- Recurring weekly -->
        <template v-if="form.strategy === 'recurring_weekly'">
          <NFormItem label="重複星期" required>
            <NSelect
              v-model:value="form.weekdays"
              :options="weekdayOptions"
              multiple
              style="width:100%"
              placeholder="選擇重複的星期"
            />
          </NFormItem>
        </template>

        <!-- Recurring monthly -->
        <template v-if="form.strategy === 'recurring_monthly'">
          <NFormItem label="每月第幾天" required>
            <NInput
              v-model:value="(form.day_of_month as any)"
              style="width:100px"
            />
          </NFormItem>
        </template>

        <!-- Cron expression -->
        <template v-if="form.strategy === 'cron'">
          <NFormItem label="Cron 表達式" required>
            <NInput
              v-model:value="form.cron_expression"
              placeholder="例: 0 2 * * 1"
              style="width:200px; font-family: monospace"
            />
          </NFormItem>
        </template>

        <!-- Common recurring fields -->
        <template v-if="form.strategy !== 'single'">
          <NFormItem label="開始時間">
            <NInput
              v-model:value="form.start_time"
              placeholder="HH:MM"
              style="width:100px; font-family: monospace"
            />
          </NFormItem>
          <NFormItem label="持續分鐘">
            <NInput
              v-model:value="(form.duration_minutes as any)"
              style="width:100px"
            />
          </NFormItem>
          <NFormItem label="時區">
            <NSelect
              v-model:value="form.timezone"
              :options="timezoneOptions"
              style="width:260px"
            />
          </NFormItem>
        </template>

        <NFormItem label="狀態">
          <NSwitch v-model:value="form.active">
            <template #checked>啟用</template>
            <template #unchecked>停用</template>
          </NSwitch>
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton @click="showModal = false">取消</NButton>
          <NButton type="primary" :loading="saving" @click="save">
            {{ editing ? '儲存' : '建立' }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>
