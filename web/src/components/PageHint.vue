<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NAlert } from 'naive-ui'

const props = defineProps<{
  /** Unique key per page — used to remember dismissal in localStorage */
  storageKey: string
  title?: string
}>()

const visible = ref(true)

onMounted(() => {
  if (localStorage.getItem(`hint_dismissed:${props.storageKey}`) === '1') {
    visible.value = false
  }
})

function dismiss() {
  visible.value = false
  localStorage.setItem(`hint_dismissed:${props.storageKey}`, '1')
}
</script>

<template>
  <NAlert
    v-if="visible"
    type="info"
    :title="title"
    closable
    class="page-hint"
    @close="dismiss"
  >
    <slot />
  </NAlert>
</template>

<style scoped>
.page-hint {
  margin: var(--space-4) 0 0;
  flex-shrink: 0;
}
</style>
