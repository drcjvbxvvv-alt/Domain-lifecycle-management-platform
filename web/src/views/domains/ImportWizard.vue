<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  NSteps, NStep, NButton, NUpload, NUploadDragger,
  NSelect, NCard, NSpin, NAlert, NDataTable, NProgress,
  NSpace, useMessage,
} from 'naive-ui'
import type { UploadFileInfo, DataTableColumns, SelectOption } from 'naive-ui'
import type { VNodeChild } from 'vue'
import { h } from 'vue'
import { PageHeader } from '@/components'
import { useImportStore } from '@/stores/import'
import { useProjectStore } from '@/stores/project'
import { useRegistrarStore } from '@/stores/registrar'
import type { ParsedRow, RowError, ImportJob } from '@/types/import'

const router    = useRouter()
const store     = useImportStore()
const projects  = useProjectStore()
const registrars = useRegistrarStore()
const message   = useMessage()

// ── Wizard step state ─────────────────────────────────────────────────────────
// Steps: 0 Upload → 1 Preview → 2 Defaults → 3 Confirm → 4 Progress
const currentStep = ref(0)

// Step 0 — Upload
const fileList        = ref<UploadFileInfo[]>([])
const selectedFile    = ref<File | null>(null)
const previewing      = ref(false)

// Step 1 — Preview
const previewRows     = ref<ParsedRow[]>([])
const previewErrors   = ref<RowError[]>([])

// Step 2 — Defaults
const selectedProjectId        = ref<number | null>(null)
const selectedRegistrarAccount = ref<number | null>(null)

// Step 3 — Confirm / start
const submitting = ref(false)

// Step 4 — Progress
const activeJob   = ref<ImportJob | null>(null)
const pollTimer   = ref<ReturnType<typeof setInterval> | null>(null)

// ── Computed options ──────────────────────────────────────────────────────────
const projectOptions = computed<SelectOption[]>(() =>
  projects.projects.map(p => ({ label: `${p.name} (${p.slug})`, value: p.id })),
)

const registrarOptions = computed<SelectOption[]>(() => [
  { label: '（不指定）', value: null as any },
  ...registrars.accounts.map(a => ({ label: a.account_name, value: a.id })),
])

// ── Preview columns ───────────────────────────────────────────────────────────
const previewColumns: DataTableColumns<ParsedRow> = [
  { title: 'FQDN',          key: 'fqdn',       minWidth: 200, ellipsis: { tooltip: true } },
  { title: '到期日',        key: 'expiry_date', width: 120, render: r => r.expiry_date ?? '-' },
  { title: '自動續約',      key: 'auto_renew',  width: 80,  render: r => r.auto_renew ? '✓' : '✗' },
  { title: '標籤',          key: 'tags',        width: 160, render: r => r.tags?.join(', ') ?? '-' },
  { title: '備注',          key: 'notes',       minWidth: 100, render: r => r.notes ?? '-' },
]

const errorColumns: DataTableColumns<RowError> = [
  { title: '行',   key: 'line',   width: 60 },
  { title: 'FQDN', key: 'fqdn',  width: 200 },
  {
    title: '原因',
    key: 'reason',
    render: (row): VNodeChild =>
      h('span', { style: 'color:var(--error)' }, row.reason),
  },
]

// ── Step 0: file selection ────────────────────────────────────────────────────
function onFileChange(data: { fileList: UploadFileInfo[] }) {
  fileList.value = data.fileList
  if (data.fileList.length > 0 && data.fileList[0].file) {
    selectedFile.value = data.fileList[0].file
  } else {
    selectedFile.value = null
  }
}

async function doPreview() {
  if (!selectedFile.value) {
    message.warning('請先選擇 CSV 檔案')
    return
  }
  previewing.value = true
  try {
    const result = await store.preview(selectedFile.value)
    previewRows.value   = result.rows
    previewErrors.value = result.errors
    currentStep.value   = 1
  } catch (e: any) {
    message.error(e?.response?.data?.message || '解析失敗，請確認 CSV 格式')
  } finally {
    previewing.value = false
  }
}

// ── Step 2 → 3: confirm ───────────────────────────────────────────────────────
function goToDefaults() {
  currentStep.value = 2
}

function goToConfirm() {
  if (!selectedProjectId.value) {
    message.warning('請選擇目標專案')
    return
  }
  currentStep.value = 3
}

// ── Step 3: submit ────────────────────────────────────────────────────────────
async function submitImport() {
  if (!selectedFile.value || !selectedProjectId.value) return
  submitting.value = true
  try {
    const job = await store.upload(
      selectedFile.value,
      selectedProjectId.value,
      selectedRegistrarAccount.value ?? undefined,
    )
    activeJob.value = job
    currentStep.value = 4
    startPolling(job.id)
  } catch (e: any) {
    message.error(e?.response?.data?.message || '匯入失敗')
  } finally {
    submitting.value = false
  }
}

// ── Step 4: progress polling ──────────────────────────────────────────────────
function startPolling(jobId: number) {
  pollTimer.value = setInterval(async () => {
    try {
      const updated = await store.pollJob(jobId)
      activeJob.value = updated
      if (updated.status === 'completed' || updated.status === 'failed') {
        stopPolling()
      }
    } catch { /* keep polling */ }
  }, 2000)
}

function stopPolling() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

const progressPercent = computed(() => {
  const j = activeJob.value
  if (!j || j.total_count === 0) return 0
  return Math.round(((j.imported_count + j.skipped_count + j.failed_count) / j.total_count) * 100)
})

const progressStatus = computed<'default' | 'success' | 'error'>(() => {
  const s = activeJob.value?.status
  if (s === 'completed') return 'success'
  if (s === 'failed')    return 'error'
  return 'default'
})

function goToJobHistory() {
  stopPolling()
  router.push('/domains/import/history')
}
</script>

<template>
  <div class="import-wizard">
    <PageHeader title="批次匯入域名" subtitle="上傳 CSV 檔案以批次新增域名" />

    <div class="wizard-body">
      <NSteps :current="currentStep" class="wizard-steps">
        <NStep title="上傳 CSV"   description="選擇檔案" />
        <NStep title="預覽"       description="確認資料" />
        <NStep title="設定預設值" description="選擇專案" />
        <NStep title="確認"       description="開始匯入" />
        <NStep title="進度"       description="等待完成" />
      </NSteps>

      <!-- ── Step 0: Upload ─────────────────────────────────────────────── -->
      <div v-if="currentStep === 0" class="wizard-panel">
        <NSpin :show="previewing">
          <NUpload
            :max="1"
            accept=".csv,text/csv"
            :file-list="fileList"
            :show-file-list="true"
            @change="onFileChange"
          >
            <NUploadDragger class="upload-dragger">
              <p class="upload-hint">拖拽或點擊選擇 CSV 檔案</p>
              <p class="upload-sub">支援格式：fqdn, expiry_date, auto_renew, registrar_account_id, dns_provider_id, tags, notes</p>
            </NUploadDragger>
          </NUpload>

          <NAlert type="info" class="csv-hint">
            <strong>CSV 範例：</strong><br>
            <code>fqdn,expiry_date,auto_renew,tags,notes</code><br>
            <code>example.com,2027-03-15,true,"production;core",Main site</code>
          </NAlert>

          <div class="step-actions">
            <NButton type="primary" :loading="previewing" :disabled="!selectedFile" @click="doPreview">
              下一步：預覽
            </NButton>
          </div>
        </NSpin>
      </div>

      <!-- ── Step 1: Preview ────────────────────────────────────────────── -->
      <div v-if="currentStep === 1" class="wizard-panel">
        <div class="preview-summary">
          <NSpace>
            <span class="summary-item good">✓ 有效：{{ previewRows.length }} 筆</span>
            <span class="summary-item bad" v-if="previewErrors.length > 0">✗ 錯誤：{{ previewErrors.length }} 筆</span>
          </NSpace>
        </div>

        <h4 class="section-label">有效資料（將被匯入）</h4>
        <NDataTable
          :columns="previewColumns"
          :data="previewRows"
          :max-height="260"
          size="small"
          striped
        />

        <template v-if="previewErrors.length > 0">
          <h4 class="section-label error-label">解析錯誤（將被跳過）</h4>
          <NDataTable
            :columns="errorColumns"
            :data="previewErrors"
            :max-height="180"
            size="small"
          />
        </template>

        <div class="step-actions">
          <NButton @click="currentStep = 0">上一步</NButton>
          <NButton type="primary" :disabled="previewRows.length === 0" @click="goToDefaults">
            下一步：設定預設值
          </NButton>
        </div>
      </div>

      <!-- ── Step 2: Defaults ───────────────────────────────────────────── -->
      <div v-if="currentStep === 2" class="wizard-panel">
        <div class="form-row">
          <label class="form-label">目標專案 <span class="required">*</span></label>
          <NSelect
            v-model:value="selectedProjectId"
            :options="projectOptions"
            :loading="projects.loading"
            placeholder="選擇專案"
            style="width: 280px"
          />
        </div>
        <div class="form-row">
          <label class="form-label">預設 Registrar 帳號（選填）</label>
          <NSelect
            v-model:value="selectedRegistrarAccount"
            :options="registrarOptions"
            placeholder="不指定"
            clearable
            style="width: 280px"
          />
          <p class="form-hint">CSV 中有填寫 registrar_account_id 的列優先使用 CSV 的值</p>
        </div>

        <div class="step-actions">
          <NButton @click="currentStep = 1">上一步</NButton>
          <NButton type="primary" @click="goToConfirm">下一步：確認</NButton>
        </div>
      </div>

      <!-- ── Step 3: Confirm ────────────────────────────────────────────── -->
      <div v-if="currentStep === 3" class="wizard-panel">
        <NCard class="confirm-card">
          <div class="confirm-row">
            <span class="confirm-label">檔案</span>
            <span>{{ selectedFile?.name }}</span>
          </div>
          <div class="confirm-row">
            <span class="confirm-label">目標專案</span>
            <span>{{ projectOptions.find(p => p.value === selectedProjectId)?.label }}</span>
          </div>
          <div class="confirm-row">
            <span class="confirm-label">有效列數</span>
            <span>{{ previewRows.length }}</span>
          </div>
          <div class="confirm-row" v-if="previewErrors.length > 0">
            <span class="confirm-label">將跳過（錯誤）</span>
            <span class="error-text">{{ previewErrors.length }}</span>
          </div>
        </NCard>

        <div class="step-actions">
          <NButton @click="currentStep = 2">上一步</NButton>
          <NButton type="primary" :loading="submitting" @click="submitImport">
            開始匯入
          </NButton>
        </div>
      </div>

      <!-- ── Step 4: Progress ───────────────────────────────────────────── -->
      <div v-if="currentStep === 4" class="wizard-panel">
        <NCard v-if="activeJob" class="progress-card">
          <div class="progress-status">
            <span :class="['status-badge', `status-${activeJob.status}`]">
              {{ activeJob.status }}
            </span>
          </div>

          <NProgress
            type="line"
            :percentage="progressPercent"
            :status="progressStatus"
            :show-indicator="true"
            class="progress-bar"
          />

          <div class="progress-counters">
            <div class="counter good">
              <div class="counter-value">{{ activeJob.imported_count }}</div>
              <div class="counter-label">已匯入</div>
            </div>
            <div class="counter skip">
              <div class="counter-value">{{ activeJob.skipped_count }}</div>
              <div class="counter-label">已跳過</div>
            </div>
            <div class="counter bad">
              <div class="counter-value">{{ activeJob.failed_count }}</div>
              <div class="counter-label">失敗</div>
            </div>
            <div class="counter total">
              <div class="counter-value">{{ activeJob.total_count }}</div>
              <div class="counter-label">總計</div>
            </div>
          </div>

          <NAlert v-if="activeJob.status === 'completed'" type="success">
            匯入完成！已匯入 {{ activeJob.imported_count }} 筆域名。
          </NAlert>
          <NAlert v-if="activeJob.status === 'failed'" type="error">
            匯入失敗。請查看歷史記錄了解詳情。
          </NAlert>
        </NCard>

        <NSpin v-else :show="true" style="min-height: 200px" />

        <div class="step-actions">
          <NButton @click="router.push('/domains')">前往域名列表</NButton>
          <NButton type="primary" @click="goToJobHistory">查看匯入歷史</NButton>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.import-wizard {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
}
.wizard-body {
  padding: 24px var(--content-padding);
  display: flex;
  flex-direction: column;
  gap: 24px;
  max-width: 800px;
}
.wizard-steps {
  flex-shrink: 0;
}
.wizard-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* Upload step */
.upload-dragger {
  padding: 40px;
  text-align: center;
}
.upload-hint {
  font-size: 16px;
  font-weight: 500;
  color: var(--text);
  margin-bottom: 8px;
}
.upload-sub {
  font-size: 12px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.csv-hint {
  font-size: 13px;
}
.csv-hint code {
  font-family: var(--font-mono);
  font-size: 12px;
}

/* Preview step */
.preview-summary {
  display: flex;
  gap: 12px;
}
.summary-item {
  font-size: 14px;
  font-weight: 600;
}
.summary-item.good { color: var(--success, #18a058); }
.summary-item.bad  { color: var(--error,   #d03050); }
.section-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-muted);
  margin: 0;
}
.error-label { color: var(--error, #d03050); }

/* Defaults step */
.form-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.form-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--text);
}
.form-label .required { color: var(--error, #d03050); }
.form-hint {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
}

/* Confirm step */
.confirm-card { max-width: 500px; }
.confirm-row {
  display: flex;
  gap: 16px;
  padding: 6px 0;
  border-bottom: 1px solid var(--border);
  font-size: 14px;
}
.confirm-row:last-child { border-bottom: none; }
.confirm-label {
  min-width: 110px;
  color: var(--text-muted);
  font-weight: 500;
}
.error-text { color: var(--error, #d03050); }

/* Progress step */
.progress-card { max-width: 500px; }
.progress-status { margin-bottom: 16px; }
.status-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
}
.status-pending    { background: var(--bg-hover); color: var(--text-muted); }
.status-processing { background: #e8f4fd; color: #2080f0; }
.status-completed  { background: #edfbea; color: #18a058; }
.status-failed     { background: #ffe7e7; color: #d03050; }
.progress-bar { margin-bottom: 20px; }
.progress-counters {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 16px;
}
.counter {
  text-align: center;
  padding: 12px 8px;
  border-radius: 8px;
  background: var(--bg-hover);
}
.counter.good  { border-top: 3px solid #18a058; }
.counter.skip  { border-top: 3px solid #f0a020; }
.counter.bad   { border-top: 3px solid #d03050; }
.counter.total { border-top: 3px solid var(--primary); }
.counter-value { font-size: 24px; font-weight: 700; color: var(--text); }
.counter-label { font-size: 12px; color: var(--text-muted); margin-top: 2px; }

/* Step actions */
.step-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
</style>
