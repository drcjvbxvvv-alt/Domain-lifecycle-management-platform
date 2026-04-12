<script setup lang="ts">
import { onMounted } from 'vue'
import { NGrid, NGridItem, NStatistic, NCard } from 'naive-ui'
import PageHeader from '@/components/PageHeader.vue'
import { useProjectStore } from '@/stores/project'
import { useDomainStore } from '@/stores/domain'
import { useAgentStore } from '@/stores/agent'
import { useReleaseStore } from '@/stores/release'

const projectStore = useProjectStore()
const domainStore  = useDomainStore()
const agentStore   = useAgentStore()
const releaseStore = useReleaseStore()

onMounted(async () => {
  await Promise.all([
    projectStore.fetchList(),
    domainStore.fetchList(),
    agentStore.fetchList({ limit: 1 }),
  ])
})
</script>

<template>
  <div>
    <PageHeader title="Dashboard" subtitle="平台概覽" />

    <NGrid :cols="4" :x-gap="16" :y-gap="16" style="margin-top: 24px;">
      <NGridItem>
        <NCard>
          <NStatistic label="專案數" :value="projectStore.projects.length" />
        </NCard>
      </NGridItem>
      <NGridItem>
        <NCard>
          <NStatistic label="域名數" :value="domainStore.total" />
        </NCard>
      </NGridItem>
      <NGridItem>
        <NCard>
          <NStatistic label="Agent 數" :value="agentStore.total" />
        </NCard>
      </NGridItem>
      <NGridItem>
        <NCard>
          <NStatistic label="發布數" :value="releaseStore.total" />
        </NCard>
      </NGridItem>
    </NGrid>
  </div>
</template>
