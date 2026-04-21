<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import type { DataTableColumns } from 'naive-ui'
import {
  NButton, NModal, NForm, NFormItem, NInput, NInputNumber,
  NSpace, NPopconfirm, NTag, NSelect, NDataTable,
  useMessage,
} from 'naive-ui'
import { PageHeader } from '@/components'
import { dnsTemplateApi } from '@/api/dnstemplate'
import type { DNSTemplate, TemplateRecord } from '@/types/dnstemplate'

const message  = useMessage()
const loading  = ref(false)
const templates = ref<DNSTemplate[]>([])

// ── Load ──────────────────────────────────────────────────────────────────────
async function load() {
  loading.value = true
  try {
    const res = await dnsTemplateApi.list()
    templates.value = res.data.items
  } catch {
    message.error('載入範本列表失敗')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// ── Form state ────────────────────────────────────────────────────────────────
const showModal  = ref(false)
const saving     = ref(false)
const editingId  = ref<number | null>(null)

interface FormRecord { name: string; type: string; content: string; ttl: number; priority: number }
interface FormState {
  name: string
  description: string
  records: FormRecord[]
}

const emptyForm = (): FormState => ({
  name: '',
  description: '',
  records: [{ name: '@', type: 'A', content: '', ttl: 300, priority: 0 }],
})

const form = ref<FormState>(emptyForm())

const recordTypeOptions = ['A','AAAA','CNAME','MX','TXT','NS','SRV','CAA'].map(t => ({ label: t, value: t }))

// Computed: auto-extracted variable names from all record placeholders
const detectedVariables = computed(() => {
  const varRe = /\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}/g
  const seen = new Set<string>()
  for (const r of form.value.records) {
    for (const field of [r.name, r.content]) {
      let m
      while ((m = varRe.exec(field)) !== null) seen.add(m[1])
      varRe.lastIndex = 0
    }
  }
  return [...seen]
})

function openCreate() {
  editingId.value = null
  form.value = emptyForm()
  showModal.value = true
}

function openEdit(tmpl: DNSTemplate) {
  editingId.value = tmpl.id
  form.value = {
    name: tmpl.name,
    description: tmpl.description ?? '',
    records: tmpl.records.map(r => ({
      name: r.name,
      type: r.type,
      content: r.content,
      ttl: r.ttl,
      priority: r.priority ?? 0,
    })),
  }
  showModal.value = true
}

function addRecord() {
  form.value.records.push({ name: '', type: 'A', content: '', ttl: 300, priority: 0 })
}
function removeRecord(index: number) {
  form.value.records.splice(index, 1)
}

async function handleSave() {
  if (!form.value.name.trim()) { message.warning('範本名稱不可為空'); return }
  if (form.value.records.length === 0) { message.warning('至少需要一筆記錄'); return }

  saving.value = true
  try {
    const req = {
      name: form.value.name.trim(),
      description: form.value.description.trim() || undefined,
      records: form.value.records.map(r => ({
        name: r.name, type: r.type, content: r.content, ttl: r.ttl,
        priority: r.priority || undefined,
      } as TemplateRecord)),
    }

    if (editingId.value) {
      await dnsTemplateApi.update(editingId.value, req)
      message.success('範本已更新')
    } else {
      await dnsTemplateApi.create(req)
      message.success('範本已建立')
    }
    showModal.value = false
    await load()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '儲存失敗')
  } finally {
    saving.value = false
  }
}

async function handleDelete(id: number) {
  try {
    await dnsTemplateApi.delete(id)
    message.success('範本已刪除')
    await load()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '刪除失敗')
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const columns: DataTableColumns<DNSTemplate> = [
  { title: '名稱', key: 'name', width: 200 },
  { title: '說明', key: 'description', ellipsis: true,
    render: r => r.description || '—' },
  { title: '記錄數', key: 'record_count', width: 90 },
  {
    title: '變數', key: 'variables', width: 220,
    render: r => {
      const keys = Object.keys(r.variables)
      if (!keys.length) return h('span', { style: 'color:#999' }, '無')
      return h(NSpace, { size: 4 }, () =>
        keys.map(k => h(NTag, { size: 'small', bordered: false }, () => `{{${k}}}`))
      )
    },
  },
  { title: '更新時間', key: 'updated_at', width: 180,
    render: r => new Date(r.updated_at).toLocaleString('zh-TW') },
  {
    title: '操作', key: '_ops', width: 140, fixed: 'right',
    render: r => h(NSpace, { size: 6 }, () => [
      h(NButton, { size: 'small', onClick: () => openEdit(r) }, () => '編輯'),
      h(NPopconfirm, { onPositiveClick: () => handleDelete(r.id) }, {
        trigger: () => h(NButton, { size: 'small', type: 'error', ghost: true }, () => '刪除'),
        default: () => `確認刪除範本「${r.name}」？`,
      }),
    ]),
  },
]
</script>

<template>
  <div style="padding:20px">
    <PageHeader title="DNS 記錄範本" subtitle="可套用至域名的 DNS 記錄藍圖" />

    <div style="margin-bottom:16px">
      <NButton type="primary" @click="openCreate">+ 新增範本</NButton>
    </div>

    <NDataTable
      :columns="columns"
      :data="templates"
      :loading="loading"
      :pagination="false"
      size="small"
      striped
    />

    <!-- Create / Edit modal -->
    <NModal v-model:show="showModal" preset="card" :title="editingId ? '編輯 DNS 範本' : '新增 DNS 範本'"
            style="width:760px;max-width:95vw" :mask-closable="false">
      <NForm label-placement="top" :show-feedback="false">
        <NFormItem label="範本名稱 *">
          <NInput v-model:value="form.name" placeholder="e.g. Standard Web" />
        </NFormItem>
        <NFormItem label="說明">
          <NInput v-model:value="form.description" placeholder="可選" />
        </NFormItem>

        <!-- Records editor -->
        <NFormItem label="記錄列表">
          <div style="width:100%">
            <div v-for="(rec, i) in form.records" :key="i"
                 style="display:flex;gap:6px;align-items:center;margin-bottom:6px;flex-wrap:wrap">
              <NInput v-model:value="rec.name" size="small" placeholder="名稱（@, www, …）" style="width:110px" />
              <NSelect v-model:value="rec.type" :options="recordTypeOptions" size="small" style="width:90px" />
              <NInput v-model:value="rec.content" size="small" placeholder="值（支援 {{var}}）" style="flex:1;min-width:120px" />
              <NInputNumber v-model:value="rec.ttl" :min="1" size="small" style="width:80px" />
              <NInputNumber v-if="rec.type === 'MX' || rec.type === 'SRV'"
                            v-model:value="rec.priority" :min="0" size="small" style="width:70px" placeholder="優先" />
              <NButton size="small" type="error" ghost :disabled="form.records.length <= 1" @click="removeRecord(i)">✕</NButton>
            </div>
            <NButton size="small" dashed @click="addRecord">+ 新增記錄</NButton>
          </div>
        </NFormItem>

        <!-- Detected variables (read-only display) -->
        <NFormItem v-if="detectedVariables.length > 0" label="偵測到的變數">
          <NSpace size="small">
            <NTag v-for="v in detectedVariables" :key="v" size="small" bordered><span v-text="'{{' + v + '}}'"/></NTag>
          </NSpace>
        </NFormItem>
      </NForm>

      <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:16px">
        <NButton @click="showModal = false">取消</NButton>
        <NButton type="primary" :loading="saving" @click="handleSave">
          {{ editingId ? '儲存變更' : '建立範本' }}
        </NButton>
      </div>
    </NModal>
  </div>
</template>
