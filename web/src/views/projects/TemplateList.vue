<script setup lang="ts">
import { onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DataTableColumns } from 'naive-ui'
import { NButton } from 'naive-ui'
import { AppTable, PageHeader, PageHint } from '@/components'
import { useTemplateStore } from '@/stores/template'
import type { TemplateResponse } from '@/types/template'

const route  = useRoute()
const router = useRouter()
const store  = useTemplateStore()
const pid    = route.params.id as string

const columns: DataTableColumns<TemplateResponse> = [
  { title: '名稱',    key: 'name',        ellipsis: { tooltip: true } },
  { title: '說明',    key: 'description', ellipsis: { tooltip: true } },
  { title: '建立時間', key: 'created_at',  width: 180,
    render: (row) => new Date(row.created_at).toLocaleString('zh-TW') },
  { title: '操作', key: 'actions', width: 80, fixed: 'right',
    render: (row) => h(NButton, {
      size: 'small', quaternary: true, type: 'primary',
      onClick: () => router.push(`/projects/${pid}/templates/${row.id}`),
    }, { default: () => '查看' }) },
]

onMounted(() => store.fetchByProject(pid))
</script>

<template>
  <div class="list-page">
    <PageHeader title="範本管理">
      <template #hint>
        <PageHint storage-key="template-list" title="範本管理說明">
          範本支援 <strong>HTML 頁面</strong>與 <strong>Nginx conf</strong> 兩種類型。<br>
          點擊範本「查看」可查看所有版本及其 Version ID；建立發布時需填入具體的 Version ID（數字）。<br>
          版本一旦發布即不可修改，需異動內容時請建立新版本。
        </PageHint>
      </template>
    </PageHeader>
    <AppTable
      :columns="columns"
      :data="store.templates"
      :loading="store.loading"
      :row-key="(r) => String(r.id)"
    />
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
