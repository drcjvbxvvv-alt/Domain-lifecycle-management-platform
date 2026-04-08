<script setup lang="ts">
import { computed } from 'vue'
import { NModal, NCard, NButton, NSpace } from 'naive-ui'

const props = withDefaults(defineProps<{
  show:     boolean
  title:    string
  content:  string
  type?:    'danger' | 'warning' | 'info'
  loading?: boolean
  confirmText?: string
  cancelText?:  string
}>(), {
  type:        'danger',
  loading:     false,
  confirmText: '確認',
  cancelText:  '取消',
})

const emit = defineEmits<{
  'update:show': [value: boolean]
  confirm: []
  cancel:  []
}>()

const confirmType = computed(() => {
  if (props.type === 'danger')  return 'error'
  if (props.type === 'warning') return 'warning'
  return 'primary'
})

const iconColor = computed(() => {
  if (props.type === 'danger')  return '#ef4444'
  if (props.type === 'warning') return '#eab308'
  return '#38bdf8'
})

function onCancel() {
  emit('update:show', false)
  emit('cancel')
}
</script>

<!--
  Usage:
    <ConfirmModal
      v-model:show="showDelete"
      title="刪除域名"
      content="此操作無法還原，確定要刪除 example.com 嗎？"
      type="danger"
      :loading="deleting"
      @confirm="handleDelete"
    />
-->
<template>
  <NModal
    :show="show"
    :mask-closable="!loading"
    @update:show="emit('update:show', $event)"
  >
    <NCard class="confirm-modal" :bordered="false" role="dialog">
      <div class="confirm-modal__body">
        <!-- Icon -->
        <div class="confirm-modal__icon" :style="{ color: iconColor }">
          <svg v-if="type === 'danger'" width="28" height="28" viewBox="0 0 24 24" fill="none">
            <path d="M12 9v4M12 17h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
          <svg v-else-if="type === 'warning'" width="28" height="28" viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2"/>
            <path d="M12 8v4M12 16h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
          </svg>
          <svg v-else width="28" height="28" viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2"/>
            <path d="M12 8v4M12 16h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
          </svg>
        </div>

        <!-- Text -->
        <div>
          <p class="confirm-modal__title">{{ title }}</p>
          <p class="confirm-modal__content">{{ content }}</p>
        </div>
      </div>

      <!-- Actions -->
      <div class="confirm-modal__actions">
        <NSpace justify="end">
          <NButton :disabled="loading" @click="onCancel">{{ cancelText }}</NButton>
          <NButton
            :type="confirmType"
            :loading="loading"
            @click="emit('confirm')"
          >
            {{ confirmText }}
          </NButton>
        </NSpace>
      </div>
    </NCard>
  </NModal>
</template>

<style scoped>
.confirm-modal {
  width: 420px;
  max-width: calc(100vw - 48px);
}

.confirm-modal__body {
  display: flex;
  gap: var(--space-4);
  align-items: flex-start;
  padding-bottom: var(--space-5);
}

.confirm-modal__icon {
  flex-shrink: 0;
  margin-top: 2px;
}

.confirm-modal__title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: var(--space-2);
}

.confirm-modal__content {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.confirm-modal__actions {
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}
</style>
