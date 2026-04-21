<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput, NSwitch,
  NPopconfirm, NDescriptions, NDescriptionsItem, NSpin, NAlert,
  useMessage,
} from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { useRegistrarStore } from '@/stores/registrar'
import type {
  RegistrarAccountResponse,
  CreateRegistrarAccountRequest,
} from '@/types/registrar'

const route  = useRoute()
const router = useRouter()
const store  = useRegistrarStore()
const message = useMessage()

const registrarId = Number(route.params.id)

// ── Edit registrar modal ──────────────────────────────────────────────────────
const showEdit = ref(false)
const saving   = ref(false)
const editForm = ref({ name: '', url: null as string | null, api_type: null as string | null, notes: null as string | null })

function openEdit() {
  if (!store.current) return
  editForm.value = {
    name:     store.current.name,
    url:      store.current.url,
    api_type: store.current.api_type,
    notes:    store.current.notes,
  }
  showEdit.value = true
}

async function submitEdit() {
  if (!editForm.value.name.trim()) {
    message.warning('名稱必填')
    return
  }
  saving.value = true
  try {
    await store.update(registrarId, editForm.value)
    message.success('已更新')
    showEdit.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '更新失敗')
  } finally {
    saving.value = false
  }
}

// ── Create account modal ──────────────────────────────────────────────────────
const showCreateAccount = ref(false)
const creatingAccount   = ref(false)
const accountForm = ref<CreateRegistrarAccountRequest>({
  account_name: '',
  is_default:   false,
  notes:        null,
})

function openCreateAccount() {
  accountForm.value = { account_name: '', is_default: false, notes: null }
  showCreateAccount.value = true
}

async function submitCreateAccount() {
  if (!accountForm.value.account_name.trim()) {
    message.warning('帳號名稱必填')
    return
  }
  creatingAccount.value = true
  try {
    await store.createAccount(registrarId, accountForm.value)
    message.success('帳號建立成功')
    showCreateAccount.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '建立失敗')
  } finally {
    creatingAccount.value = false
  }
}

// ── Delete account ────────────────────────────────────────────────────────────
async function deleteAccount(id: number) {
  try {
    await store.removeAccount(id, registrarId)
    message.success('帳號已刪除')
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '刪除失敗')
  }
}

// ── Table columns for accounts ────────────────────────────────────────────────
const accountColumns: DataTableColumns<RegistrarAccountResponse> = [
  { title: '帳號名稱', key: 'account_name', ellipsis: { tooltip: true } },
  { title: '預設', key: 'is_default', width: 80,
    render: (row) => row.is_default ? '✓' : '-' },
  { title: '備注', key: 'notes', ellipsis: { tooltip: true },
    render: (row) => row.notes ?? '-' },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  {
    title: '操作', key: 'actions', width: 120, fixed: 'right',
    render: (row): VNodeChild => h(NPopconfirm, {
      onPositiveClick: () => deleteAccount(row.id),
    }, {
      trigger: () => h(NButton, {
        size: 'small', type: 'error', ghost: true,
      }, { default: () => '刪除' }),
      default: () => '確定刪除此帳號？域名依附中將無法刪除。',
    }),
  },
]

onMounted(async () => {
  await store.fetchOne(registrarId)
  await store.fetchAccounts(registrarId)
})
</script>

<template>
  <div>
    <PageHeader :title="store.current?.name ?? '載入中…'" @back="router.back()">
      <template #extra>
        <NSpace>
          <NButton @click="openEdit">編輯</NButton>
          <NButton type="primary" @click="openCreateAccount">新增帳號</NButton>
        </NSpace>
      </template>
    </PageHeader>

    <NSpin :show="store.loading">
      <!-- Registrar info -->
      <NDescriptions v-if="store.current" bordered :column="2" class="mb-4">
        <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
        <NDescriptionsItem label="API 類型">{{ store.current.api_type ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem label="網址">{{ store.current.url ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem label="建立時間">{{ new Date(store.current.created_at).toLocaleString('zh-TW') }}</NDescriptionsItem>
        <NDescriptionsItem label="備注" :span="2">{{ store.current.notes ?? '-' }}</NDescriptionsItem>
      </NDescriptions>

      <!-- Accounts table -->
      <div class="section-title">帳號列表</div>
      <AppTable
        :columns="accountColumns"
        :data="store.accounts"
        :loading="store.loading"
        :row-key="(row) => row.id"
      />
    </NSpin>

    <!-- Edit registrar modal -->
    <NModal
      v-model:show="showEdit"
      preset="card"
      title="編輯 Registrar"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="editForm">
        <NFormItem label="名稱" required>
          <NInput v-model:value="editForm.name" />
        </NFormItem>
        <NFormItem label="API 類型">
          <NInput v-model:value="(editForm as any).api_type" clearable />
        </NFormItem>
        <NFormItem label="網址">
          <NInput v-model:value="(editForm as any).url" clearable />
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

    <!-- Create account modal -->
    <NModal
      v-model:show="showCreateAccount"
      preset="card"
      title="新增 Registrar 帳號"
      style="width: 480px"
      :mask-closable="false"
    >
      <NAlert type="warning" class="mb-3">
        Credentials（API 金鑰）請在帳號建立後另外填寫，或透過後台直接管理。
      </NAlert>
      <NForm label-placement="left" label-width="100px" :model="accountForm">
        <NFormItem label="帳號名稱" required>
          <NInput v-model:value="accountForm.account_name" placeholder="e.g. company-main" />
        </NFormItem>
        <NFormItem label="設為預設">
          <NSwitch v-model:value="accountForm.is_default" />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="(accountForm as any).notes" type="textarea" :rows="2" clearable />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreateAccount = false">取消</NButton>
          <NButton type="primary" :loading="creatingAccount" @click="submitCreateAccount">建立</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.mb-4 { margin-bottom: 16px; }
.mb-3 { margin-bottom: 12px; }
.section-title {
  font-size: 15px;
  font-weight: 600;
  margin: 16px 0 8px;
}
</style>
