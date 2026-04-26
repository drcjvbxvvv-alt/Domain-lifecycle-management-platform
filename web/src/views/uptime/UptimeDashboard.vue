<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard, NSpace, NSelect, NButton, NTag, NSpin, NGrid, NGi,
  useMessage,
} from 'naive-ui'
import type { SelectOption } from 'naive-ui'
import { PageHeader } from '@/components'
import { uptimeApi } from '@/api/uptime'
import type { DomainUptimeRow, CalendarDay } from '@/api/uptime'

const message = useMessage()

// ── State ──────────────────────────────────────────────────────────────────────

const loadingWorst   = ref(false)
const loadingCalendar = ref(false)
const worstList      = ref<DomainUptimeRow[]>([])
const selectedDomain = ref<number | null>(null)
const calendarData   = ref<CalendarDay[]>([])
const calendarYear   = ref(new Date().getFullYear())
const calendarMonth  = ref(new Date().getMonth() + 1)
const days           = ref(30)

const daysOptions: SelectOption[] = [
  { label: '過去 7 天',  value: 7  },
  { label: '過去 30 天', value: 30 },
  { label: '過去 90 天', value: 90 },
]

const monthOptions: SelectOption[] = Array.from({ length: 12 }, (_, i) => ({
  label: `${i + 1} 月`,
  value: i + 1,
}))

// ── Data fetch ─────────────────────────────────────────────────────────────────

async function fetchWorst() {
  loadingWorst.value = true
  try {
    const res = await uptimeApi.getWorstPerformers({ days: days.value, limit: 15 }) as any
    worstList.value = res?.data?.items ?? []
  } catch {
    message.error('載入最差執行清單失敗')
  } finally {
    loadingWorst.value = false
  }
}

async function fetchCalendar() {
  if (!selectedDomain.value) return
  loadingCalendar.value = true
  try {
    const res = await uptimeApi.getUptimeCalendar(selectedDomain.value, calendarYear.value, calendarMonth.value) as any
    calendarData.value = res?.data?.days ?? []
  } catch {
    message.error('載入日曆資料失敗')
  } finally {
    loadingCalendar.value = false
  }
}

function selectDomain(domainId: number) {
  selectedDomain.value = domainId
  fetchCalendar()
}

// ── Calendar helpers ──────────────────────────────────────────────────────────

function calendarColor(pct: number): string {
  if (pct < 0)    return '#e0e0e0'   // no data
  if (pct >= 99)  return '#18a058'   // green
  if (pct >= 95)  return '#f0a020'   // yellow
  if (pct >= 80)  return '#fa6400'   // orange
  return '#d03050'                   // red
}

function calendarTitle(day: CalendarDay): string {
  if (day.uptime_pct < 0) return `${day.date}: 無資料`
  return `${day.date}: ${day.uptime_pct.toFixed(2)}%`
}

// ── Uptime badge ──────────────────────────────────────────────────────────────

function uptimeType(pct: number): 'success' | 'warning' | 'error' | 'default' {
  if (pct >= 99)  return 'success'
  if (pct >= 95)  return 'warning'
  if (pct >= 0)   return 'error'
  return 'default'
}

// ── Lifecycle ──────────────────────────────────────────────────────────────────

onMounted(fetchWorst)
</script>

<template>
  <div>
    <PageHeader title="可用性儀表板" />

    <!-- Controls -->
    <NSpace class="mb-3" align="center">
      <NSelect v-model:value="days" :options="daysOptions" style="width:140px" @update:value="fetchWorst" />
      <NButton type="primary" @click="fetchWorst">刷新</NButton>
    </NSpace>

    <NGrid :cols="2" :x-gap="16" :y-gap="16">
      <!-- Worst Performers Table -->
      <NGi>
        <NCard title="可用率最低的域名 (Top 15)">
          <NSpin :show="loadingWorst">
            <div v-if="worstList.length === 0" class="empty-hint">
              暫無資料（等待 probe 累積數據）
            </div>
            <table v-else class="uptime-table">
              <thead>
                <tr>
                  <th>域名</th>
                  <th>可用率</th>
                  <th>DOWN 次數</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in worstList"
                  :key="row.domain_id"
                  :class="{ 'row-selected': selectedDomain === row.domain_id }"
                  @click="selectDomain(row.domain_id)"
                >
                  <td class="fqdn">{{ row.fqdn }}</td>
                  <td>
                    <NTag :type="uptimeType(row.uptime_pct)" size="small">
                      {{ row.uptime_pct.toFixed(2) }}%
                    </NTag>
                  </td>
                  <td class="count">{{ row.down_count }}</td>
                  <td>
                    <NButton size="tiny" @click.stop="selectDomain(row.domain_id)">日曆</NButton>
                  </td>
                </tr>
              </tbody>
            </table>
          </NSpin>
        </NCard>
      </NGi>

      <!-- Calendar Heatmap -->
      <NGi>
        <NCard title="可用率日曆">
          <template #header-extra>
            <NSpace size="small" align="center">
              <NSelect
                v-model:value="calendarMonth"
                :options="monthOptions"
                style="width:90px"
                @update:value="fetchCalendar"
              />
              <NButton size="small" @click="fetchCalendar" :disabled="!selectedDomain">載入</NButton>
            </NSpace>
          </template>
          <NSpin :show="loadingCalendar">
            <div v-if="!selectedDomain" class="empty-hint">
              ← 點擊左側域名查看日曆
            </div>
            <div v-else-if="calendarData.length === 0" class="empty-hint">
              暫無資料
            </div>
            <div v-else class="calendar-grid">
              <div
                v-for="day in calendarData"
                :key="day.date"
                class="calendar-day"
                :style="{ background: calendarColor(day.uptime_pct) }"
                :title="calendarTitle(day)"
              >
                <span class="day-label">{{ new Date(day.date).getDate() }}</span>
              </div>
            </div>
          </NSpin>
        </NCard>
      </NGi>
    </NGrid>

    <!-- Legend -->
    <NSpace class="mt-3" align="center">
      <span class="legend-label">圖例：</span>
      <span class="legend-item" style="background:#18a058">≥ 99%</span>
      <span class="legend-item" style="background:#f0a020">95–99%</span>
      <span class="legend-item" style="background:#fa6400">80–95%</span>
      <span class="legend-item" style="background:#d03050">< 80%</span>
      <span class="legend-item" style="background:#e0e0e0; color:#666">無資料</span>
    </NSpace>
  </div>
</template>

<style scoped>
.mb-3 { margin-bottom: 12px; }
.mt-3 { margin-top: 12px; }
.empty-hint { color: #999; text-align: center; padding: 32px 0; font-size: 14px; }

.uptime-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.uptime-table th {
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid #eee;
  color: #666;
}
.uptime-table td {
  padding: 6px 8px;
  border-bottom: 1px solid #f5f5f5;
  cursor: pointer;
}
.uptime-table tr:hover td { background: #f9f9f9; }
.row-selected td { background: #e8f4ff !important; }
.fqdn { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.count { color: #d03050; }

.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 4px;
}
.calendar-day {
  aspect-ratio: 1;
  border-radius: 3px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: default;
}
.day-label { font-size: 11px; color: rgba(255,255,255,0.9); font-weight: 600; }

.legend-label { font-size: 13px; color: #666; }
.legend-item {
  padding: 3px 10px;
  border-radius: 3px;
  font-size: 12px;
  color: #fff;
}
</style>
