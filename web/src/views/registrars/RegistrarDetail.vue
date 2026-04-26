<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput, NSwitch,
  NPopconfirm, NDescriptions, NDescriptionsItem, NSpin, NAlert,
  NSelect, NTag, NDivider, NStatistic, NGrid, NGridItem,
  NList, NListItem, NText, NEllipsis,
  useMessage,
} from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { useRegistrarStore } from '@/stores/registrar'
import type {
  RegistrarAccountResponse,
  CreateRegistrarAccountRequest,
  GoDaddyCredentials,
  NamecheapCredentials,
  AliyunCredentials,
  SyncResult,
} from '@/types/registrar'

const route   = useRoute()
const router  = useRouter()
const store   = useRegistrarStore()
const message = useMessage()

const registrarId = Number(route.params.id)

// ── Edit registrar modal ──────────────────────────────────────────────────────
const showEdit = ref(false)
const saving   = ref(false)
const editForm = ref({
  name:     '',
  url:      null as string | null,
  api_type: null as string | null,
  notes:    null as string | null,
})

const apiTypeOptions = [
  { label: 'GoDaddy', value: 'godaddy' },
  { label: 'Namecheap', value: 'namecheap' },
  { label: '阿里雲萬網', value: 'aliyun' },
  { label: '騰訊雲', value: 'tencentcloud' },
  { label: '其他 (手動)', value: 'manual' },
]

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
  credentials:  {},
})

function openCreateAccount() {
  accountForm.value = { account_name: '', is_default: false, notes: null, credentials: {} }
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

// ── Edit credentials modal ────────────────────────────────────────────────────
const showCredentials = ref(false)
const credAccountId   = ref<number | null>(null)
const credAccountName = ref('')
const savingCreds     = ref(false)

const isGoDaddy   = computed(() => store.current?.api_type === 'godaddy')
const isNamecheap = computed(() => store.current?.api_type === 'namecheap')
const isAliyun    = computed(() => store.current?.api_type === 'aliyun')

// ── Per-provider forms ────────────────────────────────────────────────────────
const godaddyForm = ref<GoDaddyCredentials>({ key: '', secret: '', environment: 'production' })
const godaddyEnvOptions = [
  { label: '正式環境 (Production)', value: 'production' },
  { label: '沙盒環境 (OTE)',        value: 'ote'        },
]

const namecheapForm = ref<NamecheapCredentials>({
  api_user: '', api_key: '', username: '', client_ip: '', environment: 'production',
})
const namecheapEnvOptions = [
  { label: '正式環境 (Production)', value: 'production' },
  { label: '沙盒環境 (Sandbox)',    value: 'sandbox'    },
]

const aliyunForm = ref<AliyunCredentials>({ access_key_id: '', access_key_secret: '' })

// Generic JSON fallback for unsupported provider types
const rawCredsJSON   = ref('')
const credsJSONError = ref('')

function openCredentials(account: RegistrarAccountResponse) {
  credAccountId.value   = account.id
  credAccountName.value = account.account_name
  godaddyForm.value     = { key: '', secret: '', environment: 'production' }
  namecheapForm.value   = { api_user: '', api_key: '', username: '', client_ip: '', environment: 'production' }
  aliyunForm.value      = { access_key_id: '', access_key_secret: '' }
  rawCredsJSON.value    = '{}'
  credsJSONError.value  = ''
  showCredentials.value = true
}

async function submitCredentials() {
  if (credAccountId.value === null) return

  let credentials: Record<string, unknown>

  if (isGoDaddy.value) {
    if (!godaddyForm.value.key.trim() || !godaddyForm.value.secret.trim()) {
      message.warning('Key 和 Secret 不能為空')
      return
    }
    credentials = { ...godaddyForm.value }

  } else if (isNamecheap.value) {
    if (!namecheapForm.value.api_user.trim() || !namecheapForm.value.api_key.trim() || !namecheapForm.value.client_ip.trim()) {
      message.warning('API User、API Key 和 Client IP 不能為空')
      return
    }
    credentials = {
      ...namecheapForm.value,
      // username defaults to api_user if left blank
      username: namecheapForm.value.username.trim() || namecheapForm.value.api_user.trim(),
    }

  } else if (isAliyun.value) {
    if (!aliyunForm.value.access_key_id.trim() || !aliyunForm.value.access_key_secret.trim()) {
      message.warning('AccessKey ID 和 Secret 不能為空')
      return
    }
    credentials = { ...aliyunForm.value }

  } else {
    try {
      credentials = JSON.parse(rawCredsJSON.value)
      credsJSONError.value = ''
    } catch {
      credsJSONError.value = 'JSON 格式錯誤'
      return
    }
  }

  savingCreds.value = true
  try {
    await store.updateAccount(credAccountId.value, registrarId, {
      account_name: credAccountName.value,
      credentials,
    })
    message.success('憑證已更新')
    showCredentials.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '更新失敗')
  } finally {
    savingCreds.value = false
  }
}

// ── Sync ──────────────────────────────────────────────────────────────────────
const syncingId     = ref<number | null>(null)
const showSyncResult = ref(false)
const syncResult    = ref<SyncResult | null>(null)

async function handleSync(account: RegistrarAccountResponse) {
  syncingId.value = account.id
  try {
    const result = await store.syncAccount(account.id)
    syncResult.value    = result
    showSyncResult.value = true
  } catch (e: any) {
    const msg = e?.response?.data?.message ?? e?.message ?? '同步失敗'
    message.error(msg)
  } finally {
    syncingId.value = null
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const accountColumns: DataTableColumns<RegistrarAccountResponse> = [
  { title: '帳號名稱', key: 'account_name', ellipsis: { tooltip: true } },
  {
    title: '預設', key: 'is_default', width: 70,
    render: (row) => row.is_default ? h(NTag, { type: 'success', size: 'small' }, { default: () => '✓' }) : '-',
  },
  { title: '備注', key: 'notes', ellipsis: { tooltip: true }, render: (row) => row.notes ?? '-' },
  {
    title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW'),
  },
  {
    title: '操作', key: 'actions', width: 220, fixed: 'right',
    render: (row): VNodeChild => h(NSpace, { size: 'small' }, {
      default: () => [
        h(NButton, {
          size: 'small',
          onClick: () => openCredentials(row),
        }, { default: () => '設定憑證' }),
        h(NButton, {
          size: 'small',
          type: 'primary',
          loading: syncingId.value === row.id,
          onClick: () => handleSync(row),
        }, { default: () => '同步域名' }),
        h(NPopconfirm, {
          onPositiveClick: () => deleteAccount(row.id),
        }, {
          trigger: () => h(NButton, { size: 'small', type: 'error', ghost: true }, { default: () => '刪除' }),
          default: () => '確定刪除此帳號？',
        }),
      ],
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
      <template #actions>
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
        <NDescriptionsItem label="API 類型">
          <NTag v-if="store.current.api_type" type="info" size="small">
            {{ store.current.api_type }}
          </NTag>
          <span v-else class="text-gray">
            未設定 — 請編輯並填寫 API 類型才能使用「同步域名」
          </span>
        </NDescriptionsItem>
        <NDescriptionsItem label="網址">{{ store.current.url ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem label="建立時間">
          {{ new Date(store.current.created_at).toLocaleString('zh-TW') }}
        </NDescriptionsItem>
        <NDescriptionsItem label="備注" :span="2">{{ store.current.notes ?? '-' }}</NDescriptionsItem>
      </NDescriptions>

      <!-- Accounts table -->
      <div class="section-title">帳號列表</div>
      <NAlert v-if="!store.current?.api_type" type="warning" class="mb-3" style="margin-bottom:12px">
        尚未設定 API 類型，無法同步域名。請先點擊「編輯」設定 API 類型（例如 godaddy）。
      </NAlert>
      <AppTable
        :columns="accountColumns"
        :data="store.accounts"
        :loading="store.loading"
        :row-key="(row) => row.id"
      />
    </NSpin>

    <!-- ── Edit registrar modal ─────────────────────────────────────────── -->
    <NModal
      v-model:show="showEdit"
      preset="card"
      title="編輯域名註冊商"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="editForm">
        <NFormItem label="名稱" required>
          <NInput v-model:value="editForm.name" />
        </NFormItem>
        <NFormItem label="API 類型">
          <NSelect
            v-model:value="(editForm as any).api_type"
            :options="apiTypeOptions"
            placeholder="選擇供應商類型"
            clearable
          />
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

    <!-- ── Create account modal ────────────────────────────────────────── -->
    <NModal
      v-model:show="showCreateAccount"
      preset="card"
      title="新增帳號"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="accountForm">
        <NFormItem label="帳號名稱" required>
          <NInput v-model:value="accountForm.account_name" placeholder="例：主帳號 GoDaddy" />
        </NFormItem>
        <NFormItem label="設為預設">
          <NSwitch v-model:value="accountForm.is_default" />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="(accountForm as any).notes" type="textarea" :rows="2" clearable />
        </NFormItem>
      </NForm>
      <NAlert type="info" class="mb-3">
        帳號建立後，點擊「設定憑證」填入 API Key/Secret。
      </NAlert>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreateAccount = false">取消</NButton>
          <NButton type="primary" :loading="creatingAccount" @click="submitCreateAccount">建立</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- ── Credentials modal ────────────────────────────────────────────── -->
    <NModal
      v-model:show="showCredentials"
      preset="card"
      :title="`設定憑證 — ${credAccountName}`"
      style="width: 520px"
      :mask-closable="false"
    >
      <NAlert type="warning" style="margin-bottom:16px">
        憑證儲存後伺服器端加密保存，API 回應不會返回憑證內容。
      </NAlert>

      <!-- GoDaddy -->
      <template v-if="isGoDaddy">
        <NForm label-placement="left" label-width="90px" :model="godaddyForm">
          <NFormItem label="Key" required>
            <NInput v-model:value="godaddyForm.key" type="password" show-password-on="click" placeholder="dKy4Gxxx..." />
          </NFormItem>
          <NFormItem label="Secret" required>
            <NInput v-model:value="godaddyForm.secret" type="password" show-password-on="click" placeholder="Sdxxx..." />
          </NFormItem>
          <NFormItem label="環境">
            <NSelect v-model:value="godaddyForm.environment" :options="godaddyEnvOptions" />
          </NFormItem>
        </NForm>
        <NAlert type="info" style="margin-top:8px">
          Key / Secret 從 <a href="https://developer.godaddy.com/keys" target="_blank" rel="noopener">developer.godaddy.com/keys</a> 取得。OTE 為沙盒環境。
        </NAlert>
      </template>

      <!-- Namecheap -->
      <template v-else-if="isNamecheap">
        <NForm label-placement="left" label-width="100px" :model="namecheapForm">
          <NFormItem label="API User" required>
            <NInput v-model:value="namecheapForm.api_user" placeholder="你的 Namecheap 帳號名稱" />
          </NFormItem>
          <NFormItem label="API Key" required>
            <NInput v-model:value="namecheapForm.api_key" type="password" show-password-on="click" placeholder="32 位元 hex key" />
          </NFormItem>
          <NFormItem label="Client IP" required>
            <NInput v-model:value="namecheapForm.client_ip" placeholder="本伺服器對外 IP" />
          </NFormItem>
          <NFormItem label="Username">
            <NInput v-model:value="namecheapForm.username" placeholder="留空則自動同步 API User" />
          </NFormItem>
          <NFormItem label="環境">
            <NSelect v-model:value="namecheapForm.environment" :options="namecheapEnvOptions" />
          </NFormItem>
        </NForm>
        <NAlert type="warning" style="margin-top:8px">
          Client IP 必須加入 Namecheap 白名單：Profile → Tools → Namecheap API Access → Whitelisted IPs
        </NAlert>
      </template>

      <!-- 阿里雲 -->
      <template v-else-if="isAliyun">
        <NForm label-placement="left" label-width="130px" :model="aliyunForm">
          <NFormItem label="AccessKey ID" required>
            <NInput v-model:value="aliyunForm.access_key_id" placeholder="LTAI5t..." />
          </NFormItem>
          <NFormItem label="AccessKey Secret" required>
            <NInput v-model:value="aliyunForm.access_key_secret" type="password" show-password-on="click" placeholder="Secret..." />
          </NFormItem>
        </NForm>
        <NAlert type="info" style="margin-top:8px">
          AccessKey 從阿里雲 RAM 控制台 → 存取控制 → AccessKey 管理 取得。建議使用子帳號並只授予域名唯讀權限。
        </NAlert>
      </template>

      <!-- Generic JSON fallback for unsupported types -->
      <template v-else>
        <NForm label-placement="top">
          <NFormItem label="Credentials JSON">
            <NInput
              v-model:value="rawCredsJSON"
              type="textarea"
              :rows="6"
              placeholder='{"key": "...", "secret": "..."}'
              :status="credsJSONError ? 'error' : undefined"
            />
            <template v-if="credsJSONError" #feedback>{{ credsJSONError }}</template>
          </NFormItem>
        </NForm>
      </template>

      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCredentials = false">取消</NButton>
          <NButton type="primary" :loading="savingCreds" @click="submitCredentials">儲存憑證</NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- ── Sync result modal ─────────────────────────────────────────────── -->
    <NModal
      v-model:show="showSyncResult"
      preset="card"
      title="同步結果"
      style="width: 560px"
    >
      <template v-if="syncResult">
        <NGrid :cols="3" :x-gap="16" style="margin-bottom:20px">
          <NGridItem>
            <NStatistic label="供應商回傳" :value="syncResult.total" />
          </NGridItem>
          <NGridItem>
            <NStatistic label="已更新" :value="syncResult.updated">
              <template #prefix>
                <NTag type="success" size="small" style="margin-right:4px">✓</NTag>
              </template>
            </NStatistic>
          </NGridItem>
          <NGridItem>
            <NStatistic label="不在本系統" :value="syncResult.not_found?.length ?? 0">
              <template #prefix>
                <NTag
                  :type="syncResult.not_found?.length ? 'warning' : 'default'"
                  size="small"
                  style="margin-right:4px"
                >!</NTag>
              </template>
            </NStatistic>
          </NGridItem>
        </NGrid>

        <!-- Not found list -->
        <template v-if="syncResult.not_found?.length">
          <NDivider style="margin:8px 0">
            <NText depth="3" style="font-size:12px">以下域名存在於供應商帳號，但未在本系統登記</NText>
          </NDivider>
          <NList size="small" bordered style="max-height:160px;overflow-y:auto">
            <NListItem v-for="fqdn in syncResult.not_found" :key="fqdn">
              <NEllipsis>{{ fqdn }}</NEllipsis>
            </NListItem>
          </NList>
          <NAlert type="info" style="margin-top:8px">
            如需追蹤這些域名，請至「域名管理」手動新增並綁定此 Registrar 帳號。
          </NAlert>
        </template>

        <!-- Errors -->
        <template v-if="syncResult.errors?.length">
          <NDivider style="margin:8px 0">
            <NText type="error" style="font-size:12px">錯誤 ({{ syncResult.errors.length }})</NText>
          </NDivider>
          <NList size="small" bordered style="max-height:120px;overflow-y:auto">
            <NListItem v-for="e in syncResult.errors" :key="e.fqdn">
              <NText type="error">{{ e.fqdn }}</NText>
              <NText depth="3" style="margin-left:8px;font-size:12px">{{ e.message }}</NText>
            </NListItem>
          </NList>
        </template>
      </template>

      <template #footer>
        <NSpace justify="end">
          <NButton type="primary" @click="showSyncResult = false">確定</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.text-gray { color: var(--n-text-color-disabled); font-size: 13px; }
.mb-3      { margin-bottom: 12px; }
.mb-4      { margin-bottom: 16px; }
</style>
