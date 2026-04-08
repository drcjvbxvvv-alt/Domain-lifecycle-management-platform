<script setup lang="ts">
import { computed } from 'vue'
import type { AnyStatus, StatusSemantic } from '@/types/common'
import { colors } from '@/styles/tokens'

const props = defineProps<{ status: AnyStatus | string }>()

// Status → Semantic mapping. Each status value belongs to one semantic
// bucket which determines its color (FRONTEND_GUIDE.md §"顏色使用規範").
// New status values must be added to BOTH this map AND the labelMap below.
const semanticMap: Record<string, StatusSemantic> = {
  // Domain Lifecycle
  requested:    'warning',
  approved:     'warning',
  provisioned:  'progress',
  active:       'success',
  disabled:     'danger',
  retired:      'neutral',

  // Release
  pending:      'progress',
  planning:     'progress',
  ready:        'progress',
  executing:    'progress',
  paused:       'warning',
  succeeded:    'success',
  failed:       'danger',
  rolling_back: 'danger',
  rolled_back:  'danger',
  cancelled:    'neutral',

  // Agent
  registered:   'neutral',
  online:       'success',
  busy:         'progress',
  idle:         'success',
  offline:      'danger',
  draining:     'warning',
  // disabled — see Domain Lifecycle entry; same color
  upgrading:    'upgrading',
  error:        'danger',
}

// Display label per status (Chinese)
const labelMap: Record<string, string> = {
  // Domain Lifecycle
  requested:    '申請中',
  approved:     '已核准',
  provisioned:  '已建置',
  active:       '正常',
  disabled:     '已停用',
  retired:      '已退役',

  // Release
  pending:      '等待中',
  planning:     '規劃中',
  ready:        '待執行',
  executing:    '執行中',
  paused:       '已暫停',
  succeeded:    '成功',
  failed:       '失敗',
  rolling_back: '回滾中',
  rolled_back:  '已回滾',
  cancelled:    '已取消',

  // Agent
  registered:   '已註冊',
  online:       '在線',
  busy:         '忙碌',
  idle:         '閒置',
  offline:      '離線',
  draining:     '排空中',
  upgrading:    '升級中',
  error:        '錯誤',
}

const semantic = computed<StatusSemantic>(() => semanticMap[props.status] ?? 'neutral')
const label    = computed<string>(() => labelMap[props.status] ?? props.status)

const style = computed(() => {
  const token = colors.statusSemantic[semantic.value]
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
    {{ label }}
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
