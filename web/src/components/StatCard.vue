<script setup lang="ts">
withDefaults(defineProps<{
  label:   string
  value:   string | number
  // Optional accent color (use colors.xxx from tokens)
  color?:  string
  // Optional trend: positive = green, negative = red
  trend?:  number
  suffix?: string
}>(), {
  color: '#38bdf8',
})
</script>

<!--
  Usage:
    <StatCard label="正常域名" :value="1024" color="#4ade80" :trend="12" />
    <StatCard label="已封鎖"   :value="3"    color="#ef4444" suffix="個" />
-->
<template>
  <div class="stat-card">
    <div class="stat-card__accent" :style="{ backgroundColor: color }" />
    <div class="stat-card__body">
      <p class="stat-card__label">{{ label }}</p>
      <div class="stat-card__value-row">
        <span class="stat-card__value" :style="{ color }">
          {{ value }}
        </span>
        <span v-if="suffix" class="stat-card__suffix">{{ suffix }}</span>
        <span
          v-if="trend !== undefined"
          class="stat-card__trend"
          :class="trend >= 0 ? 'trend-up' : 'trend-down'"
        >
          {{ trend >= 0 ? '▲' : '▼' }} {{ Math.abs(trend) }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.stat-card {
  display: flex;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-5);
  background-color: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  position: relative;
}

.stat-card__accent {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  border-radius: 8px 0 0 8px;
}

.stat-card__body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding-left: var(--space-2);
}

.stat-card__label {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.stat-card__value-row {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
}

.stat-card__value {
  font-size: 28px;
  font-weight: 700;
  line-height: 1;
}

.stat-card__suffix {
  font-size: 13px;
  color: var(--text-secondary);
}

.stat-card__trend {
  font-size: 12px;
  font-weight: 500;
}
.trend-up   { color: #4ade80; }
.trend-down { color: #f87171; }
</style>
