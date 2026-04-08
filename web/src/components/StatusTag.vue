<script setup lang="ts">
import { computed } from 'vue'
import type { DomainStatus } from '@/types/common'
import { colors } from '@/styles/tokens'

const props = defineProps<{ status: DomainStatus }>()

const labelMap: Record<DomainStatus, string> = {
  inactive:  '未部署',
  deploying: '部署中',
  active:    '正常',
  degraded:  '降級',
  switching: '切換中',
  suspended: '已暫停',
  failed:    '失敗',
  blocked:   '已封鎖',
  retired:   '已退役',
}

const style = computed(() => {
  const token = colors.status[props.status]
  return {
    color:           token.color,
    backgroundColor: token.bg,
    borderColor:     token.color + '40',
  }
})
</script>

<template>
  <span class="status-tag" :style="style">
    <span class="dot" :style="{ backgroundColor: style.color }" />
    {{ labelMap[status] }}
  </span>
</template>

<style scoped>
.status-tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: var(--space-12, 9999px);
  border: 1px solid;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  line-height: 1.6;
}
.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
</style>
