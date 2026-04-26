<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NModal, NCard, NForm, NFormItem, NInput, NSwitch,
  NDescriptions, NDescriptionsItem, NSpace, NTag, NPopconfirm,
  NSpin, useMessage,
} from 'naive-ui'
import { PageHeader, AppTable } from '@/components'
import { useCDNStore } from '@/stores/cdn'
import { CDN_PROVIDER_TYPES } from '@/api/cdn'
import type { CDNAccountResponse, CreateCDNAccountRequest, UpdateCDNAccountRequest } from '@/api/cdn'

const route   = useRoute()
const router  = useRouter()
const store   = useCDNStore()
const message = useMessage()

const providerId = Number(route.params.id)

// ── Edit provider modal ───────────────────────────────────────────────────────
const showEdit  = ref(false)
const editing   = ref(false)
const editForm  = ref({ name: '', provider_type: '', description: '' })

function openEdit() {
  const p = store.current
  if (!p) return
  editForm.value = {
    name:          p.name,
    provider_type: p.provider_type,
    description:   p.description ?? '',
  }
  showEdit.value = true
}

async function handleEdit() {
  editing.value = true
  try {
    await store.update(providerId, {
      name:          editForm.value.name,
      provider_type: editForm.value.provider_type,
      description:   editForm.value.description || null,
    })
    message.success('更新成功')
    showEdit.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '更新失敗')
  } finally {
    editing.value = false
  }
}

async function handleDeleteProvider() {
  try {
    await store.remove(providerId)
    message.success('供應商已刪除')
    router.push('/cdn-providers')
  } catch (e: any) {
    message.error(e?.response?.data?.message || '刪除失敗（可能有帳號尚未移除）')
  }
}

// ── Create account modal ──────────────────────────────────────────────────────
const showCreateAccount = ref(false)
const creatingAccount   = ref(false)
const accountForm = ref<CreateCDNAccountRequest & { credentialsJson: string }>({
  account_name:   '',
  credentialsJson: '{}',
  notes:          null,
  enabled:        true,
})

function openCreateAccount() {
  accountForm.value = { account_name: '', credentialsJson: '{}', notes: null, enabled: true }
  showCreateAccount.value = true
}

async function handleCreateAccount() {
  if (!accountForm.value.account_name) {
    message.warning('請填寫帳號名稱')
    return
  }
  creatingAccount.value = true
  try {
    let creds: Record<string, unknown> = {}
    try { creds = JSON.parse(accountForm.value.credentialsJson) } catch { /* keep empty */ }
    await store.createAccount(providerId, {
      account_name: accountForm.value.account_name,
      credentials:  creds,
      notes:        accountForm.value.notes,
      enabled:      accountForm.value.enabled,
    })
    message.success('帳號建立成功')
    showCreateAccount.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '建立失敗')
  } finally {
    creatingAccount.value = false
  }
}

// ── Edit account modal ────────────────────────────────────────────────────────
const showEditAccount  = ref(false)
const editingAccount   = ref(false)
const editAccountId    = ref<number | null>(null)
const editAccountForm  = ref<UpdateCDNAccountRequest & { credentialsJson: string }>({
  account_name:   '',
  credentialsJson: '{}',
  notes:          null,
  enabled:        true,
})

function openEditAccount(row: CDNAccountResponse) {
  editAccountId.value = row.id
  editAccountForm.value = {
    account_name:    row.account_name,
    credentialsJson: JSON.stringify(row.credentials ?? {}, null, 2),
    notes:           row.notes,
    enabled:         row.enabled,
  }
  showEditAccount.value = true
}

async function handleEditAccount() {
  if (!editAccountId.value) return
  editingAccount.value = true
  try {
    let creds: Record<string, unknown> = {}
    try { creds = JSON.parse(editAccountForm.value.credentialsJson) } catch { /* keep */ }
    await store.updateAccount(editAccountId.value, providerId, {
      account_name: editAccountForm.value.account_name,
      credentials:  creds,
      notes:        editAccountForm.value.notes,
      enabled:      editAccountForm.value.enabled,
    })
    message.success('帳號更新成功')
    showEditAccount.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '更新失敗')
  } finally {
    editingAccount.value = false
  }
}

async function handleDeleteAccount(id: number) {
  try {
    await store.removeAccount(id, providerId)
    message.success('帳號已刪除')
  } catch (e: any) {
    message.error(e?.response?.data?.message || '刪除失敗（可能有域名尚在使用此帳號）')
  }
}

// ── Computed ──────────────────────────────────────────────────────────────────
const providerTypeLabel = computed(() => {
  const map: Record<string, string> = {}
  CDN_PROVIDER_TYPES.forEach(t => { map[t.value] = t.label })
  return map
})

// ── Table columns ─────────────────────────────────────────────────────────────
const accountColumns: DataTableColumns<CDNAccountResponse> = [
  { title: '帳號名稱', key: 'account_name', minWidth: 150 },
  {
    title: '狀態', key: 'enabled', width: 80,
    render: (row): VNodeChild => h(NTag, {
      size: 'small',
      type: row.enabled ? 'success' : 'default',
    }, { default: () => row.enabled ? '啟用' : '停用' }),
  },
  {
    title: '備注', key: 'notes', ellipsis: { tooltip: true }, minWidth: 160,
    render: (row) => row.notes ?? '-',
  },
  {
    title: '建立時間', key: 'created_at', width: 140,
    render: (row) => new Date(row.created_at).toLocaleDateString('zh-TW'),
  },
  {
    title: '操作', key: 'actions', width: 130, fixed: 'right',
    render: (row): VNodeChild => h(NSpace, {}, {
      default: () => [
        h(NButton, {
          size: 'small', quaternary: true, type: 'primary',
          onClick: () => openEditAccount(row),
        }, { default: () => '編輯' }),
        h(NPopconfirm, {
          onPositiveClick: () => handleDeleteAccount(row.id),
        }, {
          trigger: () => h(NButton, {
            size: 'small', quaternary: true, type: 'error',
          }, { default: () => '刪除' }),
          default: () => '確認刪除此帳號？',
        }),
      ],
    }),
  },
]

onMounted(async () => {
  await store.fetchOne(providerId)
  await store.fetchAccounts(providerId)
})
</script>

<template>
  <div class="detail-page">
    <NSpin :show="store.loading && !store.current">
      <PageHeader
        :title="store.current?.name ?? 'CDN 供應商'"
        subtitle="CDN 供應商詳情與帳號管理"
        back-path="/cdn-providers"
      >
        <template #actions>
          <NButton @click="openEdit">編輯供應商</NButton>
          <NPopconfirm @positive-click="handleDeleteProvider">
            <template #trigger>
              <NButton type="error">刪除供應商</NButton>
            </template>
            確認刪除此供應商？（需先移除所有帳號）
          </NPopconfirm>
        </template>
      </PageHeader>

      <!-- Provider info -->
      <NCard class="info-card">
        <NDescriptions bordered :column="3" size="small">
          <NDescriptionsItem label="名稱">{{ store.current?.name ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="類型">
            <NTag size="small" type="info">
              {{ providerTypeLabel[store.current?.provider_type ?? ''] ?? store.current?.provider_type ?? '-' }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem label="說明">{{ store.current?.description ?? '-' }}</NDescriptionsItem>
        </NDescriptions>
      </NCard>

      <!-- Accounts -->
      <NCard title="帳號列表" class="accounts-card">
        <template #header-extra>
          <NButton type="primary" size="small" @click="openCreateAccount">新增帳號</NButton>
        </template>
        <AppTable
          :columns="accountColumns"
          :data="store.accounts"
          :loading="store.loading"
          :row-key="(r: CDNAccountResponse) => r.id"
        />
      </NCard>
    </NSpin>

    <!-- Edit provider modal -->
    <NModal v-model:show="showEdit" :mask-closable="!editing">
      <NCard title="編輯 CDN 供應商" :bordered="false" style="width: 480px">
        <NForm label-placement="left" label-width="100px">
          <NFormItem label="名稱" required>
            <NInput v-model:value="editForm.name" />
          </NFormItem>
          <NFormItem label="說明">
            <NInput v-model:value="editForm.description" type="textarea" :rows="2" />
          </NFormItem>
        </NForm>
        <template #action>
          <NSpace justify="end">
            <NButton :disabled="editing" @click="showEdit = false">取消</NButton>
            <NButton type="primary" :loading="editing" @click="handleEdit">儲存</NButton>
          </NSpace>
        </template>
      </NCard>
    </NModal>

    <!-- Create account modal -->
    <NModal v-model:show="showCreateAccount" :mask-closable="!creatingAccount">
      <NCard title="新增 CDN 帳號" :bordered="false" style="width: 520px">
        <NForm label-placement="left" label-width="100px">
          <NFormItem label="帳號名稱" required>
            <NInput v-model:value="accountForm.account_name" placeholder="例：直播2、馬甲1" />
          </NFormItem>
          <NFormItem label="憑證 (JSON)">
            <NInput
              v-model:value="accountForm.credentialsJson"
              type="textarea"
              :rows="5"
              placeholder='{"api_key": "...", "secret": "..."}'
              font-size="12px"
            />
          </NFormItem>
          <NFormItem label="備注">
            <NInput v-model:value="(accountForm as any).notes" type="textarea" :rows="2" clearable />
          </NFormItem>
          <NFormItem label="啟用">
            <NSwitch v-model:value="accountForm.enabled" />
          </NFormItem>
        </NForm>
        <template #action>
          <NSpace justify="end">
            <NButton :disabled="creatingAccount" @click="showCreateAccount = false">取消</NButton>
            <NButton type="primary" :loading="creatingAccount" @click="handleCreateAccount">建立</NButton>
          </NSpace>
        </template>
      </NCard>
    </NModal>

    <!-- Edit account modal -->
    <NModal v-model:show="showEditAccount" :mask-closable="!editingAccount">
      <NCard title="編輯 CDN 帳號" :bordered="false" style="width: 520px">
        <NForm label-placement="left" label-width="100px">
          <NFormItem label="帳號名稱" required>
            <NInput v-model:value="editAccountForm.account_name" />
          </NFormItem>
          <NFormItem label="憑證 (JSON)">
            <NInput
              v-model:value="editAccountForm.credentialsJson"
              type="textarea"
              :rows="5"
              font-size="12px"
            />
          </NFormItem>
          <NFormItem label="備注">
            <NInput v-model:value="(editAccountForm as any).notes" type="textarea" :rows="2" clearable />
          </NFormItem>
          <NFormItem label="啟用">
            <NSwitch v-model:value="editAccountForm.enabled" />
          </NFormItem>
        </NForm>
        <template #action>
          <NSpace justify="end">
            <NButton :disabled="editingAccount" @click="showEditAccount = false">取消</NButton>
            <NButton type="primary" :loading="editingAccount" @click="handleEditAccount">儲存</NButton>
          </NSpace>
        </template>
      </NCard>
    </NModal>
  </div>
</template>

<style scoped>
.detail-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: auto;
}
.info-card {
  margin: 16px var(--content-padding, 16px) 0;
}
.accounts-card {
  margin: 16px var(--content-padding, 16px);
  flex: 1;
  min-height: 0;
}
</style>
