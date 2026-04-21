<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NDataTable, NButton, NSpace, NSelect, NInputNumber,
  NPopconfirm, NTag, useMessage,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { permissionApi } from '@/api/permission'
import type { DomainPermission, DomainPermissionLevel } from '@/types/permission'
import { PERMISSION_LEVELS } from '@/types/permission'

// ── Props ─────────────────────────────────────────────────────────────────────
const props = defineProps<{
  domainId: number
  /** Caller's effective permission on this domain. */
  myPermission: DomainPermissionLevel | ''
}>()

const message = useMessage()

// ── State ─────────────────────────────────────────────────────────────────────
const loading   = ref(false)
const granting  = ref(false)
const rows      = ref<DomainPermission[]>([])

// Add-permission form
const newUserId     = ref<number | null>(null)
const newPermission = ref<DomainPermissionLevel>('viewer')

// ── Load ──────────────────────────────────────────────────────────────────────
async function load() {
  loading.value = true
  try {
    const res = await permissionApi.list(props.domainId)
    rows.value = res.data.items
  } catch {
    message.error('無法載入權限列表')
  } finally {
    loading.value = false
  }
}

onMounted(load)

// ── Grant ─────────────────────────────────────────────────────────────────────
async function handleGrant() {
  if (!newUserId.value) {
    message.warning('請輸入使用者 ID')
    return
  }
  granting.value = true
  try {
    await permissionApi.grant(props.domainId, newUserId.value, newPermission.value)
    message.success('已授予權限')
    newUserId.value = null
    await load()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '授予失敗')
  } finally {
    granting.value = false
  }
}

// ── Revoke ────────────────────────────────────────────────────────────────────
async function handleRevoke(userId: number) {
  try {
    await permissionApi.revoke(props.domainId, userId)
    message.success('已撤銷權限')
    await load()
  } catch (e: any) {
    message.error(e?.response?.data?.message || '撤銷失敗')
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────
const permTagType: Record<DomainPermissionLevel, 'info' | 'warning' | 'error'> = {
  viewer: 'info',
  editor: 'warning',
  admin:  'error',
}

const columns: DataTableColumns<DomainPermission> = [
  { title: '使用者名稱', key: 'username', width: 160 },
  { title: '顯示名稱', key: 'display_name', width: 160,
    render: (row) => row.display_name || '—' },
  { title: '使用者 ID', key: 'user_id', width: 100 },
  { title: '權限', key: 'permission', width: 120,
    render: (row) => h(NTag, { type: permTagType[row.permission], size: 'small' }, () => row.permission) },
  { title: '授予時間', key: 'granted_at', width: 180,
    render: (row) => new Date(row.granted_at).toLocaleString('zh-TW') },
  {
    title: '操作', key: 'actions', width: 120,
    render: (row) => {
      if (props.myPermission !== 'admin') return null
      return h(NPopconfirm, { onPositiveClick: () => handleRevoke(row.user_id) }, {
        trigger: () => h(NButton, { size: 'small', type: 'error', ghost: true }, () => '撤銷'),
        default: () => `確認撤銷 ${row.username} 的權限？`,
      })
    },
  },
]

const permLevelOptions = PERMISSION_LEVELS.map(l => ({ label: l, value: l }))
</script>

<template>
  <NSpace vertical :size="20">
    <!-- Grant form (admin only) -->
    <div v-if="myPermission === 'admin'" style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
      <NInputNumber
        v-model:value="newUserId"
        :min="1"
        placeholder="使用者 ID"
        style="width:140px"
      />
      <NSelect
        v-model:value="newPermission"
        :options="permLevelOptions"
        style="width:120px"
      />
      <NButton
        type="primary"
        :loading="granting"
        @click="handleGrant"
      >
        授予權限
      </NButton>
    </div>

    <!-- Permissions table -->
    <NDataTable
      :columns="columns"
      :data="rows"
      :loading="loading"
      :pagination="false"
      size="small"
      striped
    />

    <p v-if="rows.length === 0 && !loading" style="color:#999;margin:0">
      尚未設定任何顯式域名級別權限（使用者可透過全域角色存取）
    </p>
  </NSpace>
</template>
