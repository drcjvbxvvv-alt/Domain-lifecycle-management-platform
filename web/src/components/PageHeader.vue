<script setup lang="ts">
defineProps<{
  title:     string
  subtitle?: string
}>()
</script>

<!--
  Usage:
    <PageHeader title="域名列表" subtitle="共 1,200 筆">
      <template #actions>
        <NButton type="primary">新增域名</NButton>
      </template>
    </PageHeader>
-->
<template>
  <div class="page-header">
    <div class="page-header__left">
      <h1 class="page-header__title">{{ title }}</h1>
      <p v-if="subtitle" class="page-header__subtitle">{{ subtitle }}</p>
    </div>
    <div class="page-header__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--page-header-height);
  padding: 0 var(--content-padding);
  border-bottom: 1px solid var(--border);
  background-color: var(--bg-page);
  flex-shrink: 0;
}

.page-header__left {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.page-header__title {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.3;
}

.page-header__subtitle {
  font-size: 13px;
  color: var(--text-muted);
  line-height: 1;
}

.page-header__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
</style>
