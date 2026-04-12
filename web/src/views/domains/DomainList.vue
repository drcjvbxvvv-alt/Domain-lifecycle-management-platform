<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton, NModal, NCard, NForm, NFormItem, NInput, NSelect, useMessage } from 'naive-ui'
import { AppTable, PageHeader, StatusTag, PageHint } from '@/components'
import { useDomainStore } from '@/stores/domain'
import { useProjectStore } from '@/stores/project'
import type { DomainResponse } from '@/types/domain'

const route  = useRoute()
const router = useRouter()
const store  = useDomainStore()
const projectStore = useProjectStore()
const message = useMessage()

// routeProjectId — set when accessed via /projects/:id/domains
const routeProjectId = route.params.id as string | undefined

// selectedProjectId — used for the project filter selector on the global domain list
const selectedProjectId = ref<number | null>(
  routeProjectId ? Number(routeProjectId) : null
)

const projectOptions = computed(() =>
  projectStore.projects.map(p => ({ label: `${p.name} (${p.slug})`, value: p.id }))
)

const selectedProjectName = computed(() => {
  if (routeProjectId) return projectStore.current?.name ?? `專案 #${routeProjectId}`
  if (selectedProjectId.value) {
    return projectStore.projects.find(p => p.id === selectedProjectId.value)?.name ?? ''
  }
  return ''
})

const showCreate = ref(false)
const creating   = ref(false)
const form = ref({
  project_id: null as number | null,
  fqdn: '',
  dns_provider: '',
  dns_zone: '',
})

const columns: DataTableColumns<DomainResponse> = [
  { title: '域名', key: 'fqdn', ellipsis: { tooltip: true } },
  { title: '狀態', key: 'lifecycle_state', width: 140,
    render: (row) => h(StatusTag, { status: row.lifecycle_state }) },
  { title: 'DNS Provider', key: 'dns_provider', width: 140,
    render: (row) => row.dns_provider ?? '-' },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/domains/${row.uuid}`),
    }, { default: () => '查看' }) },
]

function onProjectChange(val: number | null) {
  if (val) store.fetchList({ project_id: val })
  else store.domains = []
}

function openCreate() {
  form.value = {
    project_id: selectedProjectId.value,
    fqdn: '',
    dns_provider: '',
    dns_zone: '',
  }
  showCreate.value = true
}

async function handleCreate() {
  if (!form.value.project_id || !form.value.fqdn) {
    message.warning('請選擇專案並填寫域名')
    return
  }
  creating.value = true
  try {
    await store.register({
      project_id: form.value.project_id,
      fqdn: form.value.fqdn,
      dns_provider: form.value.dns_provider || undefined,
      dns_zone: form.value.dns_zone || undefined,
    })
    message.success('域名註冊成功')
    showCreate.value = false
    if (selectedProjectId.value) store.fetchList({ project_id: selectedProjectId.value })
  } catch (e: any) {
    message.error(e?.response?.data?.message || '註冊失敗')
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  if (!routeProjectId) {
    await projectStore.fetchList()
  } else {
    store.fetchList({ project_id: Number(routeProjectId) })
  }
})
</script>

<template>
  <div class="list-page">
    <PageHeader title="域名管理" :subtitle="selectedProjectName || '請先選擇專案'">
      <template #actions>
        <NSelect
          v-if="!routeProjectId"
          v-model:value="selectedProjectId"
          :options="projectOptions"
          :loading="projectStore.loading"
          placeholder="選擇專案"
          style="width: 200px"
          @update:value="onProjectChange"
        />
        <NButton type="primary" :disabled="!selectedProjectId" @click="openCreate">
          註冊域名
        </NButton>
      </template>
      <template #hint>
        <PageHint storage-key="domain-list" title="域名管理說明">
          生命週期流程：<strong>requested → approved → provisioned → active → disabled → retired</strong><br>
          只有 <strong>active</strong> 狀態的域名才能被包含在新的發布中。<br>
          新域名建立後進入 requested；點擊「查看」再按「核准」，系統將自動建立 DNS 記錄。
        </PageHint>
      </template>
    </PageHeader>
    <AppTable
      :columns="columns"
      :data="store.domains"
      :loading="store.loading"
      :row-key="(r) => r.uuid"
    />

    <NModal v-model:show="showCreate" :mask-closable="!creating">
      <NCard title="註冊域名" :bordered="false" style="width: 480px">
        <NForm label-placement="left" label-width="80">
          <NFormItem label="專案" required>
            <span style="font-size: 14px; color: var(--text-primary)">
              {{ selectedProjectName }}
            </span>
          </NFormItem>
          <NFormItem
            label="域名"
            required
            feedback="填入完整域名，例如：shop.example.com、www.my-site.com"
            :show-feedback="true"
          >
            <NInput v-model:value="form.fqdn" placeholder="例：shop.example.com" />
          </NFormItem>
          <NFormItem label="DNS Provider">
            <NInput v-model:value="form.dns_provider" placeholder="cloudflare（選填）" />
          </NFormItem>
          <NFormItem label="DNS Zone">
            <NInput v-model:value="form.dns_zone" placeholder="example.com（選填）" />
          </NFormItem>
        </NForm>
        <template #action>
          <div style="display: flex; justify-content: flex-end; gap: 8px">
            <NButton @click="showCreate = false" :disabled="creating">取消</NButton>
            <NButton type="primary" :loading="creating" @click="handleCreate">註冊</NButton>
          </div>
        </template>
      </NCard>
    </NModal>
  </div>
</template>

<style scoped>
.list-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
</style>
