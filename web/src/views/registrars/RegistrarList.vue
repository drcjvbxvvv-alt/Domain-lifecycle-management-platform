<script setup lang="ts">
import { onMounted, ref, h, computed } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import type { VNodeChild } from 'vue'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput, NSelect, NTag,
  NDivider, NText, NPopconfirm, useMessage,
} from 'naive-ui'
import { AppTable, PageHeader } from '@/components'
import { useRegistrarStore } from '@/stores/registrar'
import type { RegistrarResponse } from '@/types/registrar'

const router  = useRouter()
const store   = useRegistrarStore()
const message = useMessage()

// ── 供應商類型 ─────────────────────────────────────────────────────────────────
const apiTypeOptions = [
  { label: 'GoDaddy',    value: 'godaddy'      },
  { label: 'Namecheap',  value: 'namecheap'    },
  { label: '阿里雲萬網', value: 'aliyun'       },
  { label: '騰訊雲',     value: 'tencentcloud' },
  { label: '其他',       value: 'manual'       },
]

const apiTypeLabel: Record<string, string> = {
  godaddy:      'GoDaddy',
  namecheap:    'Namecheap',
  aliyun:       '阿里雲萬網',
  tencentcloud: '騰訊雲',
  manual:       '其他',
}

const isNamecheap = computed(() => form.value.api_type === 'namecheap')
const isAliyun    = computed(() => form.value.api_type === 'aliyun')

// ── Create modal state ────────────────────────────────────────────────────────
const showCreate = ref(false)
const creating   = ref(false)

const form = ref({
  // 基本資訊
  name:     '',
  api_type: null as string | null,
  url:      null as string | null,
  notes:    null as string | null,
  // GoDaddy
  gd_key:         '',
  gd_secret:      '',
  gd_environment: 'production' as 'production' | 'ote',
  // Namecheap
  nc_api_user:    '',
  nc_api_key:     '',
  nc_client_ip:   '',
  nc_environment: 'production' as 'production' | 'sandbox',
  // Aliyun
  al_access_key_id:     '',
  al_access_key_secret: '',
})

const godaddyEnvOptions = [
  { label: '正式環境 (Production)', value: 'production' },
  { label: '沙盒環境 (OTE)',        value: 'ote'        },
]
const namecheapEnvOptions = [
  { label: '正式環境 (Production)', value: 'production' },
  { label: '沙盒環境 (Sandbox)',    value: 'sandbox'    },
]

const isGoDaddy  = computed(() => form.value.api_type === 'godaddy')
const isNamecheap = computed(() => form.value.api_type === 'namecheap')
const isAliyun    = computed(() => form.value.api_type === 'aliyun')

function openCreate() {
  form.value = {
    name: '', api_type: null, url: null, notes: null,
    gd_key: '', gd_secret: '', gd_environment: 'production',
    nc_api_user: '', nc_api_key: '', nc_client_ip: '', nc_environment: 'production',
    al_access_key_id: '', al_access_key_secret: '',
  }
  showCreate.value = true
}

async function submitCreate() {
  if (!form.value.name.trim()) {
    message.warning('請輸入名稱')
    return
  }
  if (isGoDaddy.value && (!form.value.gd_key.trim() || !form.value.gd_secret.trim())) {
    message.warning('請填入 GoDaddy API Key 和 Secret')
    return
  }
  if (isNamecheap.value && (!form.value.nc_api_user.trim() || !form.value.nc_api_key.trim() || !form.value.nc_client_ip.trim())) {
    message.warning('請填入 Namecheap API User、API Key 和 Client IP')
    return
  }
  if (isAliyun.value && (!form.value.al_access_key_id.trim() || !form.value.al_access_key_secret.trim())) {
    message.warning('請填入阿里雲 AccessKey ID 和 AccessKey Secret')
    return
  }

  creating.value = true
  try {
    const registrar = await store.create({
      name:     form.value.name,
      api_type: form.value.api_type,
      url:      form.value.url,
      notes:    form.value.notes,
    })

    // 自動建立預設帳號（附帶憑證）
    if (registrar) {
      let credentials: Record<string, unknown> | null = null

      if (isGoDaddy.value && form.value.gd_key.trim()) {
        credentials = {
          key:         form.value.gd_key.trim(),
          secret:      form.value.gd_secret.trim(),
          environment: form.value.gd_environment,
        }
      } else if (isNamecheap.value && form.value.nc_api_user.trim()) {
        credentials = {
          api_user:    form.value.nc_api_user.trim(),
          api_key:     form.value.nc_api_key.trim(),
          username:    form.value.nc_api_user.trim(),
          client_ip:   form.value.nc_client_ip.trim(),
          environment: form.value.nc_environment,
        }
      } else if (isAliyun.value && form.value.al_access_key_id.trim()) {
        credentials = {
          access_key_id:     form.value.al_access_key_id.trim(),
          access_key_secret: form.value.al_access_key_secret.trim(),
        }
      }

      if (credentials) {
        await store.createAccount(registrar.id, {
          account_name: '預設帳號',
          is_default:   true,
          credentials,
        })
      }
    }

    message.success('建立成功')
    showCreate.value = false
    if (registrar) {
      router.push({ name: 'RegistrarDetail', params: { id: registrar.id } })
    }
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '建立失敗')
  } finally {
    creating.value = false
  }
}

// ── Delete ────────────────────────────────────────────────────────────────────
async function deleteRegistrar(id: number) {
  try {
    await store.remove(id)
    message.success('已刪除')
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '刪除失敗')
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const columns: DataTableColumns<RegistrarResponse> = [
  { title: '名稱', key: 'name', ellipsis: { tooltip: true } },
  {
    title: '供應商', key: 'api_type', width: 140,
    render: (row) => {
      if (!row.api_type) return h(NText, { depth: 3 }, { default: () => '未設定' })
      const label = apiTypeLabel[row.api_type] ?? row.api_type
      return h(NTag, { size: 'small', type: 'info' }, { default: () => label })
    },
  },
  {
    title: '備注', key: 'notes', ellipsis: { tooltip: true },
    render: (row) => row.notes ?? '-',
  },
  {
    title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW'),
  },
  {
    title: '操作', key: 'actions', width: 140, fixed: 'right',
    render: (row): VNodeChild => h(NSpace, { size: 'small' }, {
      default: () => [
        h(NButton, {
          size: 'small', type: 'primary', ghost: true,
          onClick: () => router.push({ name: 'RegistrarDetail', params: { id: row.id } }),
        }, { default: () => '管理帳號' }),
        h(NPopconfirm, {
          onPositiveClick: () => deleteRegistrar(row.id),
        }, {
          trigger: () => h(NButton, { size: 'small', type: 'error', ghost: true }, { default: () => '刪除' }),
          default: () => '確定刪除？若有帳號或域名依附將無法刪除。',
        }),
      ],
    }),
  },
]

onMounted(() => store.fetchList())
</script>

<template>
  <div>
    <PageHeader title="域名註冊商管理">
      <template #actions>
        <NButton type="primary" @click="openCreate">新增註冊商</NButton>
      </template>
    </PageHeader>

    <AppTable
      :columns="columns"
      :data="store.registrars"
      :loading="store.loading"
      :row-key="(row) => row.id"
    />

    <!-- ── 新增註冊商 modal ──────────────────────────────────────────── -->
    <NModal
      v-model:show="showCreate"
      preset="card"
      title="新增域名註冊商"
      style="width: 500px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="110px" :model="form">

        <!-- 基本資訊 -->
        <NFormItem label="名稱" required>
          <NInput v-model:value="form.name" placeholder="例：我的 GoDaddy" />
        </NFormItem>
        <NFormItem label="供應商" required>
          <NSelect
            v-model:value="form.api_type"
            :options="apiTypeOptions"
            placeholder="請選擇"
          />
        </NFormItem>
        <NFormItem label="備注">
          <NInput v-model:value="(form as any).notes" type="textarea" :rows="2" clearable />
        </NFormItem>

        <!-- GoDaddy 憑證 -->
        <template v-if="isGoDaddy">
          <NDivider style="margin: 8px 0">
            <NText depth="3" style="font-size: 12px">
              GoDaddy API 憑證（從 developer.godaddy.com/keys 取得）
            </NText>
          </NDivider>
          <NFormItem label="Key" required>
            <NInput v-model:value="form.gd_key" type="password" show-password-on="click" placeholder="dKy4Gxxx..." />
          </NFormItem>
          <NFormItem label="Secret" required>
            <NInput v-model:value="form.gd_secret" type="password" show-password-on="click" placeholder="Sdxxx..." />
          </NFormItem>
          <NFormItem label="環境">
            <NSelect v-model:value="form.gd_environment" :options="godaddyEnvOptions" />
          </NFormItem>
        </template>

        <!-- Namecheap 憑證 -->
        <template v-if="isNamecheap">
          <NDivider style="margin: 8px 0">
            <NText depth="3" style="font-size: 12px">
              Namecheap API 憑證（Profile → Tools → Namecheap API Access）
            </NText>
          </NDivider>
          <NFormItem label="API User" required>
            <NInput v-model:value="form.nc_api_user" placeholder="你的 Namecheap 使用者名稱" />
          </NFormItem>
          <NFormItem label="API Key" required>
            <NInput v-model:value="form.nc_api_key" type="password" show-password-on="click" placeholder="32 位元 hex key" />
          </NFormItem>
          <NFormItem label="Client IP" required>
            <NInput v-model:value="form.nc_client_ip" placeholder="本伺服器的 IP，須加入白名單" />
          </NFormItem>
          <NFormItem label="環境">
            <NSelect v-model:value="form.nc_environment" :options="namecheapEnvOptions" />
          </NFormItem>
        </template>

        <!-- 阿里雲憑證 -->
        <template v-if="isAliyun">
          <NDivider style="margin: 8px 0">
            <NText depth="3" style="font-size: 12px">
              阿里雲 AccessKey（RAM 控制台 → 存取控制 → AccessKey 管理）
            </NText>
          </NDivider>
          <NFormItem label="AccessKey ID" required>
            <NInput v-model:value="form.al_access_key_id" placeholder="LTAI5t..." />
          </NFormItem>
          <NFormItem label="AccessKey Secret" required>
            <NInput v-model:value="form.al_access_key_secret" type="password" show-password-on="click" placeholder="Secret..." />
          </NFormItem>
        </template>

      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="creating" @click="submitCreate">建立</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>
