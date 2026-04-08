<script setup lang="ts">
import { computed } from 'vue'
import type { AlertSeverity } from '@/types/common'
import { colors } from '@/styles/tokens'

const props = defineProps<{ severity: AlertSeverity }>()

const style = computed(() => {
  const token = colors.severity[props.severity]
  return {
    color:           token.color,
    backgroundColor: token.bg,
    borderColor:     token.color + '40',
  }
})
</script>

<template>
  <span class="severity-tag" :style="style">{{ severity }}</span>
</template>

<style scoped>
.severity-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  border: 1px solid;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
  white-space: nowrap;
  line-height: 1.6;
  font-family: monospace;
}
</style>
