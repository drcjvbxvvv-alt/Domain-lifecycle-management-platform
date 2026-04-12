<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton, NModal, NCard, NForm, NFormItem, NInput, useMessage } from 'naive-ui'
import { AppTable, PageHeader, PageHint } from '@/components'
import { useProjectStore } from '@/stores/project'
import type { ProjectResponse } from '@/types/project'

const router  = useRouter()
const store   = useProjectStore()
const message = useMessage()

const showCreate = ref(false)
const creating   = ref(false)
const form = ref({ name: '', slug: '', description: '' })

const columns: DataTableColumns<ProjectResponse> = [
  { title: '名稱',    key: 'name',       ellipsis: { tooltip: true } },
  { title: 'Slug',   key: 'slug',       width: 160 },
  { title: '說明',    key: 'description', ellipsis: { tooltip: true } },
  { title: '建立時間', key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  {
    title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${row.uuid}`),
    }, { default: () => '查看' }),
  },
]

function openCreate() {
  form.value = { name: '', slug: '', description: '' }
  showCreate.value = true
}

async function handleCreate() {
  if (!form.value.name || !form.value.slug) {
    message.warning('請填寫名稱和 Slug')
    return
  }
  creating.value = true
  try {
    await store.create({
      name: form.value.name,
      slug: form.value.slug,
      description: form.value.description || undefined,
    })
    message.success('專案建立成功')
    showCreate.value = false
  } catch (e: any) {
    message.error(e?.response?.data?.message || '建立失敗')
  } finally {
    creating.value = false
  }
}

onMounted(() => store.fetchList())
</script>

<template>
  <div class="list-page">
    <PageHeader title="專案管理" subtitle="管理所有專案">
      <template #actions>
        <NButton type="primary" @click="openCreate">新增專案</NButton>
      </template>
      <template #hint>
        <PageHint storage-key="project-list" title="專案管理說明">
          專案是所有資源的容器，域名、範本和發布均歸屬於某個專案。<br>
          <strong>Slug</strong> 為 URL 識別符（小寫英文 / 數字 / 連字號），建立後不可修改。<br>
          點擊「查看」進入專案詳情，可管理該專案下的域名、範本和發布。
        </PageHint>
      </template>
    </PageHeader>
    <AppTable
      :columns="columns"
      :data="store.projects"
      :loading="store.loading"
      :row-key="(r) => r.uuid"
    />

    <NModal v-model:show="showCreate" :mask-closable="!creating">
      <NCard title="新增專案" :bordered="false" style="width: 480px">
        <NForm label-placement="left" label-width="80">
          <NFormItem label="名稱" required>
            <NInput v-model:value="form.name" placeholder="My Project" />
          </NFormItem>
          <NFormItem label="Slug" required>
            <NInput v-model:value="form.slug" placeholder="my-project" />
          </NFormItem>
          <NFormItem label="說明">
            <NInput v-model:value="form.description" type="textarea" placeholder="專案描述（選填）" :rows="3" />
          </NFormItem>
        </NForm>
        <template #action>
          <div style="display: flex; justify-content: flex-end; gap: 8px">
            <NButton @click="showCreate = false" :disabled="creating">取消</NButton>
            <NButton type="primary" :loading="creating" @click="handleCreate">建立</NButton>
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
