<script setup lang="ts">
import { onMounted, ref, computed, h } from 'vue'
import type { DataTableColumns, SelectOption } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NTag, NModal, NCard, NDataTable, NSpin, NSelect, NSpace,
} from 'naive-ui'
import { PageHeader, PageHint } from '@/components'
import { useImportStore } from '@/stores/import'
import { useProjectStore } from '@/stores/project'
import type { ImportJob, RowError } from '@/types/import'

const store    = useImportStore()
const projects = useProjectStore()

const filterProjectId = ref<number | null>(null)
const projectOptions  = computed<SelectOption[]>(() => [
  { label: '全部專案', value: null as any },
  ...projects.projects.map(p => ({ label: `${p.name}`, value: p.id })),
])

// Detail modal
const showDetail  = ref(false)
const detailJob   = ref<ImportJob | null>(null)
const detailErrors = ref<RowError[]>([])

function openDetail(job: ImportJob) {
  detailJob.value = job
  try {
    detailErrors.value = job.error_details ? JSON.parse(job.error_details) : []
  } catch {
    detailErrors.value = []
  }
  showDetail.value = true
}

const statusColor: Record<string, string> = {
  pending:    'default',
  fetching:   'info',
  processing: 'info',
  completed:  'success',
  failed:     'error',
}

const columns: DataTableColumns<ImportJob> = [
  { title: 'ID',   key: 'id',   width: 60 },
  { title: '狀態', key: 'status', width: 110,
    render: (row): VNodeChild => h(NTag, { type: statusColor[row.status] as any, size: 'small' }, { default: () => row.status }),
  },
  { title: '專案', key: 'project_id', width: 80,
    render: (row) => `#${row.project_id}` },
  { title: '總計', key: 'total_count',    width: 70 },
  { title: '匯入', key: 'imported_count', width: 70,
    render: (row): VNodeChild => h('span', { style: 'color:var(--success,#18a058)' }, String(row.imported_count)) },
  { title: '跳過', key: 'skipped_count',  width: 70,
    render: (row): VNodeChild => h('span', { style: 'color:var(--warning,#f0a020)' }, String(row.skipped_count)) },
  { title: '失敗', key: 'failed_count',   width: 70,
    render: (row): VNodeChild => {
      if (row.failed_count === 0) return String(row.failed_count)
      return h('span', { style: 'color:var(--error,#d03050)' }, String(row.failed_count))
    },
  },
  { title: '建立時間', key: 'created_at', width: 160,
    render: (row) => row.created_at.replace('T', ' ').replace('Z', '') },
  { title: '完成時間', key: 'completed_at', width: 160,
    render: (row) => row.completed_at ? row.completed_at.replace('T', ' ').replace('Z', '') : '-' },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row): VNodeChild => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => openDetail(row),
    }, { default: () => '詳情' }),
  },
]

const errorColumns: DataTableColumns<RowError> = [
  { title: '行',   key: 'line',   width: 60 },
  { title: 'FQDN', key: 'fqdn',  width: 220 },
  { title: '原因', key: 'reason', minWidth: 200,
    render: (row): VNodeChild => h('span', { style: 'color:var(--error,#d03050)' }, row.reason),
  },
]

function loadJobs() {
  store.fetchJobs(filterProjectId.value ?? undefined)
}

onMounted(async () => {
  await projects.fetchList()
  loadJobs()
})
</script>

<template>
  <div class="import-history">
    <PageHeader title="匯入歷史" subtitle="CSV 批次匯入記錄">
      <template #actions>
        <NSelect
          v-model:value="filterProjectId"
          :options="projectOptions"
          style="width: 200px"
          @update:value="loadJobs"
        />
      </template>
      <template #hint>
        <PageHint storage-key="import-history" title="匯入歷史說明">
          每次 CSV 匯入都會建立一筆記錄，可在此追蹤匯入進度與錯誤詳情。
        </PageHint>
      </template>
    </PageHeader>

    <NSpin :show="store.loading" style="min-height:200px">
      <NDataTable
        :columns="columns"
        :data="store.jobs"
        :loading="store.loading"
        :row-key="(r: ImportJob) => r.id"
        size="small"
        striped
        :scroll-x="900"
      />
    </NSpin>

    <!-- Detail modal -->
    <NModal v-model:show="showDetail">
      <NCard
        v-if="detailJob"
        :title="`匯入詳情 #${detailJob.id}`"
        :bordered="false"
        style="width: 660px"
      >
        <NSpace vertical>
          <div class="detail-row">
            <span class="detail-label">狀態</span>
            <NTag :type="(statusColor[detailJob.status] as any)" size="small">{{ detailJob.status }}</NTag>
          </div>
          <div class="detail-row">
            <span class="detail-label">建立時間</span>
            <span>{{ detailJob.created_at }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">完成時間</span>
            <span>{{ detailJob.completed_at ?? '-' }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">匯入 / 跳過 / 失敗 / 總計</span>
            <span>
              <span style="color:var(--success,#18a058)">{{ detailJob.imported_count }}</span> /
              <span style="color:var(--warning,#f0a020)">{{ detailJob.skipped_count }}</span> /
              <span style="color:var(--error,#d03050)">{{ detailJob.failed_count }}</span> /
              {{ detailJob.total_count }}
            </span>
          </div>

          <template v-if="detailErrors.length > 0">
            <h4 style="margin:0;font-size:13px;color:var(--text-muted)">錯誤明細</h4>
            <NDataTable
              :columns="errorColumns"
              :data="detailErrors"
              :max-height="300"
              size="small"
            />
          </template>
        </NSpace>

        <template #action>
          <NButton @click="showDetail = false">關閉</NButton>
        </template>
      </NCard>
    </NModal>
  </div>
</template>

<style scoped>
.import-history {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.detail-row {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 14px;
}
.detail-label {
  min-width: 160px;
  color: var(--text-muted);
  font-weight: 500;
}
</style>
