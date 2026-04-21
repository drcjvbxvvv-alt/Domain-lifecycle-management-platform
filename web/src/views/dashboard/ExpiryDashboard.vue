<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { NCard, NSpace, NStatistic, NSpin } from 'naive-ui'
import { PageHeader, PageHint } from '@/components'
import { expiryApi } from '@/api/expiry'
import type { ExpiryDashboardData } from '@/types/expiry'
import { BAND_CONFIG } from '@/types/expiry'
import type { ApiResponse } from '@/types/common'

const router  = useRouter()
const loading = ref(true)
const data    = ref<ExpiryDashboardData | null>(null)

async function fetchData() {
  loading.value = true
  try {
    const res = await expiryApi.dashboard() as unknown as ApiResponse<ExpiryDashboardData>
    data.value = res.data
  } finally {
    loading.value = false
  }
}

const bands = computed(() => {
  if (!data.value) return []
  return data.value.domain_bands.map(b => ({
    ...b,
    ...BAND_CONFIG[b.status],
  }))
})

const calendarDots = computed(() => {
  if (!data.value) return []
  return data.value.calendar.sort((a, b) => a.date.localeCompare(b.date))
})

function navigateToList(_status: string) {
  router.push('/domains')
}

onMounted(fetchData)
</script>

<template>
  <div class="expiry-dashboard">
    <PageHeader title="到期總覽" subtitle="域名與 SSL 憑證到期狀態">
      <template #hint>
        <PageHint storage-key="expiry-dashboard" title="到期總覽說明">
          系統每日自動檢查域名到期日，依剩餘天數分為五個等級。<br>
          點擊各卡片可快速跳轉至對應域名列表。
        </PageHint>
      </template>
    </PageHeader>

    <NSpin :show="loading" style="min-height:200px">
      <!-- Band cards -->
      <div v-if="data" class="band-grid">
        <NCard
          v-for="band in bands"
          :key="band.status"
          class="band-card"
          :style="{ borderTop: `3px solid ${band.color}` }"
          hoverable
          @click="navigateToList(band.status)"
        >
          <div class="band-card__header">
            <span class="band-card__emoji">{{ band.emoji }}</span>
            <span class="band-card__label">{{ band.label }}</span>
          </div>
          <NStatistic :value="band.count" class="band-card__stat" />
        </NCard>
      </div>

      <!-- Total -->
      <div v-if="data" class="total-bar">
        <NSpace align="center">
          <span class="total-label">需關注域名總計</span>
          <strong class="total-value">{{ data.total_expiring }}</strong>
        </NSpace>
      </div>

      <!-- Calendar (simplified: date list) -->
      <div v-if="data && calendarDots.length > 0" class="calendar-section">
        <h3 class="section-title">未來 90 天到期日曆</h3>
        <div class="calendar-grid">
          <div
            v-for="entry in calendarDots"
            :key="entry.date"
            class="calendar-entry"
          >
            <span class="calendar-date">{{ entry.date }}</span>
            <span class="calendar-count">{{ entry.count }} 筆</span>
          </div>
        </div>
      </div>
    </NSpin>
  </div>
</template>

<style scoped>
.expiry-dashboard {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
}

.band-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 12px;
  padding: 16px var(--content-padding);
}

.band-card {
  cursor: pointer;
  transition: transform 0.15s;
}
.band-card:hover {
  transform: translateY(-2px);
}
.band-card__header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}
.band-card__emoji {
  font-size: 18px;
}
.band-card__label {
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 500;
}
.band-card__stat {
  font-size: 28px;
}

.total-bar {
  padding: 12px var(--content-padding);
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}
.total-label {
  font-size: 14px;
  color: var(--text-muted);
}
.total-value {
  font-size: 20px;
  color: var(--text);
}

.calendar-section {
  padding: 16px var(--content-padding);
}
.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
  margin-bottom: 12px;
}
.calendar-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.calendar-entry {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  background: var(--bg-hover);
  border-radius: 4px;
  font-size: 13px;
}
.calendar-date {
  font-family: var(--font-mono);
  color: var(--text);
}
.calendar-count {
  color: var(--text-muted);
  font-weight: 500;
}
</style>
