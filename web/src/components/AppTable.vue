<script setup lang="ts" generic="T extends Record<string, unknown>">
import { computed } from 'vue'
import { NDataTable, NPagination } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import EmptyState from './EmptyState.vue'

const props = withDefaults(defineProps<{
  columns:    DataTableColumns<T>
  data:       T[]
  loading?:   boolean
  rowKey?:    (row: T) => string | number
  // Pagination — leave undefined to hide
  page?:      number
  pageSize?:  number
  itemCount?: number
}>(), {
  loading:  false,
  pageSize: 50,
})

const emit = defineEmits<{
  'update:page': [page: number]
}>()

const isEmpty = computed(() => !props.loading && props.data.length === 0)
</script>

<!--
  Usage:
    <AppTable
      :columns="columns"
      :data="domains"
      :loading="loading"
      :row-key="(row) => row.uuid"
      :page="page"
      :page-size="50"
      :item-count="total"
      @update:page="page = $event"
    />
-->
<template>
  <div class="app-table">
    <NDataTable
      v-if="!isEmpty"
      :columns="columns"
      :data="data"
      :loading="loading"
      :row-key="rowKey"
      :single-line="false"
      :bordered="false"
      size="small"
      class="app-table__inner"
    />

    <EmptyState v-else-if="isEmpty" />

    <div v-if="itemCount !== undefined && itemCount > 0" class="app-table__pagination">
      <NPagination
        :page="page"
        :page-size="pageSize"
        :item-count="itemCount"
        :page-slot="5"
        show-quick-jumper
        @update:page="emit('update:page', $event)"
      />
    </div>
  </div>
</template>

<style scoped>
.app-table {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}

.app-table__inner {
  flex: 1;
}

/* Standardise row height */
.app-table__inner :deep(.n-data-table-tr) {
  height: var(--table-row-height);
}

/* Header background */
.app-table__inner :deep(.n-data-table-th) {
  background-color: var(--bg-page) !important;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
}

/* Row hover */
.app-table__inner :deep(.n-data-table-tr:hover .n-data-table-td) {
  background-color: var(--bg-hover) !important;
}

.app-table__pagination {
  display: flex;
  justify-content: flex-end;
  padding: var(--space-4) var(--content-padding);
  border-top: 1px solid var(--border);
}
</style>
