<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { SelectOption } from 'naive-ui'
import {
  NButton, NSpace, NModal, NForm, NFormItem, NInput, NSelect,
  NDescriptions, NDescriptionsItem, NSpin, useMessage,
} from 'naive-ui'
import { PageHeader } from '@/components'
import { useDNSProviderStore } from '@/stores/dnsprovider'
import type { UpdateDNSProviderRequest } from '@/types/dnsprovider'

const route   = useRoute()
const router  = useRouter()
const store   = useDNSProviderStore()
const message = useMessage()

const providerId = Number(route.params.id)

// ── Edit modal ────────────────────────────────────────────────────────────────
const showEdit  = ref(false)
const saving    = ref(false)
const typeOptions = ref<SelectOption[]>([])
const editForm  = ref<UpdateDNSProviderRequest>({
  name:          '',
  provider_type: 'cloudflare',
  notes:         null,
})

function openEdit() {
  if (!store.current) return
  editForm.value = {
    name:          store.current.name,
    provider_type: store.current.provider_type,
    notes:         store.current.notes,
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
    await store.update(providerId, editForm.value)
    message.success('已更新')
    showEdit.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message ?? '更新失敗')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await Promise.all([store.fetchOne(providerId), store.fetchTypes()])
  typeOptions.value = store.supportedTypes.map(t => ({ label: t, value: t }))
})
</script>

<template>
  <div>
    <PageHeader :title="store.current?.name ?? '載入中…'" @back="router.back()">
      <template #extra>
        <NButton @click="openEdit">編輯</NButton>
      </template>
    </PageHeader>

    <NSpin :show="store.loading">
      <NDescriptions v-if="store.current" bordered :column="2">
        <NDescriptionsItem label="UUID">{{ store.current.uuid }}</NDescriptionsItem>
        <NDescriptionsItem label="Provider 類型">{{ store.current.provider_type }}</NDescriptionsItem>
        <NDescriptionsItem label="建立時間">{{ new Date(store.current.created_at).toLocaleString('zh-TW') }}</NDescriptionsItem>
        <NDescriptionsItem label="更新時間">{{ new Date(store.current.updated_at).toLocaleString('zh-TW') }}</NDescriptionsItem>
        <NDescriptionsItem label="備注" :span="2">{{ store.current.notes ?? '-' }}</NDescriptionsItem>
      </NDescriptions>
    </NSpin>

    <!-- Edit modal -->
    <NModal
      v-model:show="showEdit"
      preset="card"
      title="編輯 DNS Provider"
      style="width: 480px"
      :mask-closable="false"
    >
      <NForm label-placement="left" label-width="100px" :model="editForm">
        <NFormItem label="名稱" required>
          <NInput v-model:value="editForm.name" />
        </NFormItem>
        <NFormItem label="Provider 類型" required>
          <NSelect v-model:value="(editForm as any).provider_type" :options="typeOptions" />
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
  </div>
</template>
