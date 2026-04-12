<script setup lang="ts">
import { onMounted } from 'vue'
import { PageHeader, StatCard, PageHint } from '@/components'
import { useProjectStore } from '@/stores/project'
import { useAgentStore } from '@/stores/agent'

const projectStore = useProjectStore()
const agentStore   = useAgentStore()

onMounted(async () => {
  await Promise.all([
    projectStore.fetchList(),
    agentStore.fetchList({ limit: 1 }),
  ])
})
</script>

<template>
  <div class="dashboard">
    <PageHeader title="Dashboard" subtitle="平台概覽">
      <template #hint>
        <PageHint storage-key="dashboard" title="平台快速導覽">
          左側選單依模組瀏覽：<strong>專案 → 域名 → 範本 → 發布 → Agent</strong>。<br>
          建議操作流程：建立專案 → 註冊域名並核准至 active → 建立範本版本 → 建立發布。<br>
          此頁數字為即時快照；點擊左側選單進入各功能的完整列表與操作。
        </PageHint>
      </template>
    </PageHeader>

    <div class="dashboard__stats">
      <StatCard label="專案數"  :value="projectStore.projects.length" color="#38bdf8" />
      <StatCard label="Agent 數" :value="agentStore.total"            color="#4ade80" />
    </div>
  </div>
</template>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
}
.dashboard__stats {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-4);
  padding: var(--space-6) var(--content-padding);
}
</style>
