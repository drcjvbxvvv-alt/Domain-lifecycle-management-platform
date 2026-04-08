<script setup lang="ts">
// SearchBar is a layout wrapper only.
// Put filter controls (NSelect, NInput, etc.) in the default slot.
// Put the primary action button in the #action slot.
</script>

<!--
  Usage:
    <SearchBar>
      <NSelect v-model:value="projectId" :options="projectOptions" placeholder="所有專案" />
      <NSelect v-model:value="status"    :options="statusOptions"  placeholder="所有狀態" />
      <NInput  v-model:value="keyword"   placeholder="搜尋域名..." />
      <template #action>
        <NButton type="primary" @click="handleCreate">新增</NButton>
      </template>
    </SearchBar>
-->
<template>
  <div class="search-bar">
    <div class="search-bar__filters">
      <slot />
    </div>
    <div class="search-bar__action">
      <slot name="action" />
    </div>
  </div>
</template>

<style scoped>
.search-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--search-bar-height);
  padding: 0 var(--content-padding);
  border-bottom: 1px solid var(--border);
  background-color: var(--bg-card);
  gap: var(--space-3);
  flex-shrink: 0;
}

.search-bar__filters {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  /* Naive UI select/input width fix */
  min-width: 0;
}

/* Constrain child filter widths */
.search-bar__filters :deep(.n-select) { width: 160px; }
.search-bar__filters :deep(.n-input)  { max-width: 280px; }

.search-bar__action {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}
</style>
