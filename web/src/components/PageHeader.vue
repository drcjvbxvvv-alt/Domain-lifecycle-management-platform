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

  Note: PageHeader does NOT own its own margin-bottom.
  The parent page component controls vertical spacing between sections.
-->
<template>
  <div class="page-header">
    <div class="page-header__row">
      <div class="page-header__left">
        <h1 class="page-header__title">{{ title }}</h1>
        <p v-if="subtitle" class="page-header__subtitle">{{ subtitle }}</p>
      </div>
      <div class="page-header__actions">
        <slot name="actions" />
      </div>
    </div>
    <slot name="hint" />
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  flex-direction: column;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.page-header__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.page-header__left {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.page-header__title {
  font-size: 20px;
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1.3;
  letter-spacing: -0.3px;
}

.page-header__subtitle {
  font-size: 13px;
  color: var(--text-muted);
  margin-top: 2px;
  line-height: 1;
}

.page-header__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
