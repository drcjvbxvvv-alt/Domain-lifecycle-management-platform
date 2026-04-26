<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import {
  NButton, NSpace, NInput, NForm, NFormItem, NSwitch,
  NCard, NModal, NPopconfirm, NTag, NDivider, useMessage,
} from 'naive-ui'
import { PageHeader } from '@/components'
import { statusPageApi } from '@/api/statuspage'
import type {
  StatusPageResponse,
  StatusPageGroup,
  StatusPageIncident,
  CreateIncidentRequest,
  IncidentSeverity,
} from '@/types/statuspage'
import type { SelectOption } from 'naive-ui'
import { NSelect } from 'naive-ui'

const route   = useRoute()
const message = useMessage()
const pageID  = Number(route.params.id)

// ── State ──────────────────────────────────────────────────────────────────────

const pageData  = ref<StatusPageResponse | null>(null)
const groups    = ref<StatusPageGroup[]>([])
const incidents = ref<StatusPageIncident[]>([])
const loading   = ref(false)

// Group form
const showGroupModal  = ref(false)
const editingGroup    = ref<StatusPageGroup | null>(null)
const groupForm       = ref({ name: '', sort_order: 0 })
const savingGroup     = ref(false)

// Monitor form
const showMonitorModal  = ref(false)
const activeGroupID     = ref<number>(0)
const monitorForm       = ref({ domain_id: '', display_name: '' })
const savingMonitor     = ref(false)

// Incident form
const showIncidentModal  = ref(false)
const editingIncident    = ref<StatusPageIncident | null>(null)
const incidentForm       = ref<{
  title: string
  content: string
  severity: IncidentSeverity
  pinned: boolean
  active: boolean
}>({ title: '', content: '', severity: 'info', pinned: false, active: true })
const savingIncident = ref(false)

const severityOptions: SelectOption[] = [
  { label: '資訊 (info)', value: 'info' },
  { label: '警告 (warning)', value: 'warning' },
  { label: '重大 (danger)', value: 'danger' },
]

const severityType: Record<IncidentSeverity, 'info' | 'warning' | 'error'> = {
  info: 'info', warning: 'warning', danger: 'error',
}

// ── Data fetch ─────────────────────────────────────────────────────────────────

async function fetchAll() {
  loading.value = true
  try {
    const [pageRes, incRes] = await Promise.all([
      statusPageApi.get(pageID) as any,
      statusPageApi.listIncidents(pageID) as any,
    ])
    pageData.value = pageRes?.data?.page ?? null
    groups.value   = pageRes?.data?.groups ?? []
    incidents.value = incRes?.data?.items ?? []
  } catch {
    message.error('載入狀態頁失敗')
  } finally {
    loading.value = false
  }
}

// ── Group actions ──────────────────────────────────────────────────────────────

function openCreateGroup() {
  editingGroup.value = null
  groupForm.value = { name: '', sort_order: groups.value.length }
  showGroupModal.value = true
}

function openEditGroup(g: StatusPageGroup) {
  editingGroup.value = g
  groupForm.value = { name: g.name, sort_order: g.sort_order }
  showGroupModal.value = true
}

async function saveGroup() {
  if (!groupForm.value.name) return
  savingGroup.value = true
  try {
    if (editingGroup.value) {
      await statusPageApi.updateGroup(pageID, editingGroup.value.id, groupForm.value)
    } else {
      await statusPageApi.createGroup(pageID, groupForm.value)
    }
    message.success('已儲存')
    showGroupModal.value = false
    await fetchAll()
  } catch {
    message.error('儲存失敗')
  } finally {
    savingGroup.value = false
  }
}

async function deleteGroup(groupID: number) {
  try {
    await statusPageApi.deleteGroup(pageID, groupID)
    message.success('已刪除')
    await fetchAll()
  } catch {
    message.error('刪除失敗')
  }
}

// ── Monitor actions ────────────────────────────────────────────────────────────

function openAddMonitor(groupID: number) {
  activeGroupID.value = groupID
  monitorForm.value = { domain_id: '', display_name: '' }
  showMonitorModal.value = true
}

async function saveMonitor() {
  const did = parseInt(monitorForm.value.domain_id)
  if (!did) { message.warning('請輸入域名 ID'); return }
  savingMonitor.value = true
  try {
    await statusPageApi.addMonitor(pageID, activeGroupID.value, {
      domain_id:    did,
      display_name: monitorForm.value.display_name || undefined,
    })
    message.success('已新增')
    showMonitorModal.value = false
    await fetchAll()
  } catch {
    message.error('新增失敗')
  } finally {
    savingMonitor.value = false
  }
}

// ── Incident actions ───────────────────────────────────────────────────────────

function openCreateIncident() {
  editingIncident.value = null
  incidentForm.value = { title: '', content: '', severity: 'info', pinned: false, active: true }
  showIncidentModal.value = true
}

function openEditIncident(inc: StatusPageIncident) {
  editingIncident.value = inc
  incidentForm.value = {
    title:    inc.title,
    content:  inc.content ?? '',
    severity: inc.severity,
    pinned:   inc.pinned,
    active:   inc.active,
  }
  showIncidentModal.value = true
}

async function saveIncident() {
  if (!incidentForm.value.title) return
  savingIncident.value = true
  try {
    const payload: CreateIncidentRequest = {
      title:    incidentForm.value.title,
      content:  incidentForm.value.content || undefined,
      severity: incidentForm.value.severity,
      pinned:   incidentForm.value.pinned,
    }
    if (editingIncident.value) {
      await statusPageApi.updateIncident(pageID, editingIncident.value.id, { ...payload, active: incidentForm.value.active })
    } else {
      await statusPageApi.createIncident(pageID, payload)
    }
    message.success('已儲存')
    showIncidentModal.value = false
    await fetchAll()
  } catch {
    message.error('儲存失敗')
  } finally {
    savingIncident.value = false
  }
}

async function deleteIncident(id: number) {
  try {
    await statusPageApi.deleteIncident(pageID, id)
    message.success('已刪除')
    incidents.value = incidents.value.filter(i => i.id !== id)
  } catch {
    message.error('刪除失敗')
  }
}

onMounted(fetchAll)
</script>

<template>
  <div>
    <PageHeader :title="`狀態頁 — ${pageData?.title ?? '...'}`">
      <template #actions>
        <NSpace>
          <NButton @click="openCreateIncident">+ 事件公告</NButton>
          <NButton type="primary" @click="openCreateGroup">+ 新增群組</NButton>
        </NSpace>
      </template>
    </PageHeader>

    <!-- Groups -->
    <NCard
      v-for="g in groups"
      :key="g.id"
      :title="g.name"
      class="mb-3"
    >
      <template #header-extra>
        <NSpace size="small">
          <NButton size="small" @click="openEditGroup(g)">編輯</NButton>
          <NButton size="small" @click="openAddMonitor(g.id)">+ Monitor</NButton>
          <NPopconfirm @positive-click="deleteGroup(g.id)">
            <template #trigger>
              <NButton size="small" type="error">刪除群組</NButton>
            </template>
            確定刪除此群組及其所有 Monitor？
          </NPopconfirm>
        </NSpace>
      </template>
      <p class="text-muted" v-if="!loading">排序: {{ g.sort_order }}</p>
    </NCard>

    <NDivider>事件公告</NDivider>

    <!-- Incidents -->
    <div v-if="incidents.length === 0" class="text-muted">目前無事件公告</div>
    <NCard
      v-for="inc in incidents"
      :key="inc.id"
      class="mb-3"
    >
      <template #header>
        <NSpace align="center">
          <NTag :type="severityType[inc.severity]" size="small">{{ inc.severity }}</NTag>
          <span>{{ inc.title }}</span>
          <NTag v-if="inc.pinned" size="small">置頂</NTag>
          <NTag v-if="!inc.active" type="default" size="small">已關閉</NTag>
        </NSpace>
      </template>
      <template #header-extra>
        <NSpace size="small">
          <NButton size="small" @click="openEditIncident(inc)">編輯</NButton>
          <NPopconfirm @positive-click="deleteIncident(inc.id)">
            <template #trigger><NButton size="small" type="error">刪除</NButton></template>
            確定刪除此事件公告？
          </NPopconfirm>
        </NSpace>
      </template>
      <p>{{ inc.content }}</p>
    </NCard>

    <!-- Group Modal -->
    <NModal v-model:show="showGroupModal" :title="editingGroup ? '編輯群組' : '新增群組'" preset="card" style="width:400px" :mask-closable="false">
      <NForm label-placement="left" label-width="80px">
        <NFormItem label="名稱" required>
          <NInput v-model:value="groupForm.name" />
        </NFormItem>
        <NFormItem label="排序">
          <NInput v-model:value="(groupForm.sort_order as any)" style="width:80px" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showGroupModal = false">取消</NButton>
          <NButton type="primary" :loading="savingGroup" @click="saveGroup">儲存</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Monitor Modal -->
    <NModal v-model:show="showMonitorModal" title="新增 Monitor" preset="card" style="width:400px" :mask-closable="false">
      <NForm label-placement="left" label-width="100px">
        <NFormItem label="域名 ID" required>
          <NInput v-model:value="monitorForm.domain_id" placeholder="輸入域名 ID" />
        </NFormItem>
        <NFormItem label="顯示名稱">
          <NInput v-model:value="monitorForm.display_name" placeholder="留空使用 FQDN" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showMonitorModal = false">取消</NButton>
          <NButton type="primary" :loading="savingMonitor" @click="saveMonitor">新增</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Incident Modal -->
    <NModal v-model:show="showIncidentModal" :title="editingIncident ? '編輯事件' : '新增事件公告'" preset="card" style="width:500px" :mask-closable="false">
      <NForm label-placement="left" label-width="80px">
        <NFormItem label="標題" required>
          <NInput v-model:value="incidentForm.title" />
        </NFormItem>
        <NFormItem label="內容">
          <NInput v-model:value="incidentForm.content" type="textarea" :rows="4" placeholder="支援 Markdown" />
        </NFormItem>
        <NFormItem label="嚴重性">
          <NSelect v-model:value="incidentForm.severity" :options="severityOptions" style="width:200px" />
        </NFormItem>
        <NFormItem label="置頂">
          <NSwitch v-model:value="incidentForm.pinned" />
        </NFormItem>
        <NFormItem v-if="editingIncident" label="狀態">
          <NSwitch v-model:value="incidentForm.active">
            <template #checked>進行中</template>
            <template #unchecked>已關閉</template>
          </NSwitch>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showIncidentModal = false">取消</NButton>
          <NButton type="primary" :loading="savingIncident" @click="saveIncident">儲存</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.mb-3 { margin-bottom: 12px; }
.text-muted { color: #999; font-size: 13px; }
</style>
