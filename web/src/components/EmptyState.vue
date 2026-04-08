<script setup lang="ts">
withDefaults(defineProps<{
  title?:       string
  description?: string
}>(), {
  title:       '暫無資料',
  description: '',
})
</script>

<!--
  Usage (basic):
    <EmptyState />

  Usage (custom):
    <EmptyState title="尚無域名" description="點擊右上角「新增域名」開始">
      <template #action>
        <NButton type="primary">新增域名</NButton>
      </template>
    </EmptyState>
-->
<template>
  <div class="empty-state">
    <div class="empty-state__icon">
      <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
        <rect width="48" height="48" rx="12" fill="rgba(56,189,248,0.08)"/>
        <path d="M14 34 L34 34 M14 24 L34 24 M14 14 L26 14"
              stroke="#38bdf8" stroke-width="2" stroke-linecap="round" opacity="0.5"/>
        <circle cx="34" cy="14" r="6" stroke="#38bdf8" stroke-width="2" opacity="0.5"/>
      </svg>
    </div>
    <p class="empty-state__title">{{ title }}</p>
    <p v-if="description" class="empty-state__description">{{ description }}</p>
    <div v-if="$slots.action" class="empty-state__action">
      <slot name="action" />
    </div>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  padding: var(--space-16) var(--content-padding);
  min-height: 240px;
}

.empty-state__icon { margin-bottom: var(--space-2); }

.empty-state__title {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-secondary);
}

.empty-state__description {
  font-size: 13px;
  color: var(--text-muted);
  text-align: center;
  max-width: 320px;
  line-height: 1.6;
}

.empty-state__action { margin-top: var(--space-2); }
</style>
