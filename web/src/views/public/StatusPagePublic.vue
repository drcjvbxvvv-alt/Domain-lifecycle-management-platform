<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { NInput, NButton, NSpace } from 'naive-ui'
import { statusPageApi } from '@/api/statuspage'
import type { PublicStatusResponse, OverallStatus, MonitorStatus, IncidentSeverity } from '@/types/statuspage'

const route = useRoute()
const slug  = route.params.slug as string

const status     = ref<PublicStatusResponse | null>(null)
const loading    = ref(true)
const error      = ref<string | null>(null)
const needsAuth  = ref(false)
const password   = ref('')
const authToken  = ref(sessionStorage.getItem(`sp_token_${slug}`) ?? '')
let   refreshTimer: ReturnType<typeof setInterval> | null = null

// ── Auth ───────────────────────────────────────────────────────────────────────

async function doAuth() {
  try {
    const res = await statusPageApi.authPage(slug, password.value) as any
    authToken.value = res?.data?.token ?? password.value
    sessionStorage.setItem(`sp_token_${slug}`, authToken.value)
    needsAuth.value = false
    await fetchStatus()
  } catch {
    error.value = '密碼錯誤'
  }
}

// ── Fetch ──────────────────────────────────────────────────────────────────────

async function fetchStatus() {
  try {
    const res = await statusPageApi.getPublicStatus(slug, authToken.value || undefined) as any
    if (res?.data?.password_required) {
      needsAuth.value = true
      loading.value = false
      return
    }
    status.value = res?.data
    error.value = null
  } catch (e: any) {
    if (e?.response?.status === 401) {
      needsAuth.value = true
    } else if (e?.response?.status === 404) {
      error.value = '找不到此狀態頁'
    } else {
      error.value = '載入失敗'
    }
  } finally {
    loading.value = false
  }
}

// ── Auto-refresh ───────────────────────────────────────────────────────────────

function startRefresh(seconds: number) {
  if (refreshTimer) clearInterval(refreshTimer)
  refreshTimer = setInterval(fetchStatus, seconds * 1000)
}

onMounted(async () => {
  await fetchStatus()
  if (status.value?.page.auto_refresh_seconds) {
    startRefresh(status.value.page.auto_refresh_seconds)
  }
})

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})

// ── Display helpers ────────────────────────────────────────────────────────────

const overallLabel: Record<OverallStatus, string> = {
  operational: '所有系統正常運行',
  degraded:    '部分系統異常',
  outage:      '系統大範圍中斷',
  maintenance: '系統維護中',
}

const overallColor: Record<OverallStatus, string> = {
  operational: '#18a058',
  degraded:    '#f0a020',
  outage:      '#d03050',
  maintenance: '#2080f0',
}

const statusDot: Record<MonitorStatus, string> = {
  up:          '#18a058',
  down:        '#d03050',
  maintenance: '#2080f0',
  unknown:     '#999',
}

const statusLabel: Record<MonitorStatus, string> = {
  up:          '正常',
  down:        '中斷',
  maintenance: '維護中',
  unknown:     '未知',
}

const severityBg: Record<IncidentSeverity, string> = {
  info:    '#e8f4fd',
  warning: '#fff7e6',
  danger:  '#fef0f0',
}
const severityColor: Record<IncidentSeverity, string> = {
  info:    '#1677ff',
  warning: '#fa8c16',
  danger:  '#d03050',
}

const overall = computed(() => status.value?.overall ?? 'operational')
</script>

<template>
  <div class="sp-page">
    <!-- Loading -->
    <div v-if="loading" class="sp-center">載入中…</div>

    <!-- Error -->
    <div v-else-if="error && !needsAuth" class="sp-center sp-error">{{ error }}</div>

    <!-- Password gate -->
    <div v-else-if="needsAuth" class="sp-auth">
      <h2>此狀態頁需要密碼</h2>
      <NSpace vertical>
        <NInput
          v-model:value="password"
          type="password"
          placeholder="輸入密碼"
          show-password-on="click"
          @keyup.enter="doAuth"
        />
        <NButton type="primary" block @click="doAuth">確認</NButton>
        <p v-if="error" class="sp-error">{{ error }}</p>
      </NSpace>
    </div>

    <!-- Status page content -->
    <template v-else-if="status">
      <!-- Header -->
      <header class="sp-header">
        <img v-if="status.page.logo_url" :src="status.page.logo_url" class="sp-logo" alt="logo" />
        <h1>{{ status.page.title }}</h1>
        <p v-if="status.page.description" class="sp-desc">{{ status.page.description }}</p>
      </header>

      <!-- Overall status banner -->
      <div class="sp-overall" :style="{ background: overallColor[overall] }">
        <span>{{ overallLabel[overall] }}</span>
      </div>

      <!-- Active incidents -->
      <div v-if="status.incidents.length > 0" class="sp-section">
        <h2>事件公告</h2>
        <div
          v-for="inc in status.incidents"
          :key="inc.id"
          class="sp-incident"
          :style="{ background: severityBg[inc.severity], borderLeft: `4px solid ${severityColor[inc.severity]}` }"
        >
          <div class="sp-incident-title">
            <span v-if="inc.pinned" class="sp-pin">📌 </span>
            <strong>{{ inc.title }}</strong>
          </div>
          <p v-if="inc.content">{{ inc.content }}</p>
          <small class="sp-time">{{ new Date(inc.created_at).toLocaleString('zh-TW') }}</small>
        </div>
      </div>

      <!-- Groups -->
      <div class="sp-section">
        <div v-for="group in status.groups" :key="group.group_id" class="sp-group">
          <div class="sp-group-header">
            <h3>{{ group.group_name }}</h3>
            <div class="sp-dot" :style="{ background: statusDot[group.status as MonitorStatus] }" />
          </div>

          <div v-for="mon in group.monitors" :key="mon.monitor_id" class="sp-monitor">
            <div class="sp-monitor-row">
              <div class="sp-dot" :style="{ background: statusDot[mon.status] }" />
              <span class="sp-monitor-name">{{ mon.display_name }}</span>
              <span class="sp-monitor-status">{{ statusLabel[mon.status] }}</span>
              <span v-if="mon.response_ms" class="sp-latency">{{ mon.response_ms }}ms</span>
              <span class="sp-uptime">{{ mon.uptime_pct.toFixed(2) }}%</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Footer -->
      <footer v-if="status.page.footer_text" class="sp-footer">
        {{ status.page.footer_text }}
      </footer>
    </template>

    <!-- Custom CSS injection -->
    <component :is="'style'" v-if="status?.page.custom_css">{{ status.page.custom_css }}</component>
  </div>
</template>

<style scoped>
.sp-page {
  max-width: 800px;
  margin: 0 auto;
  padding: 24px 16px;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
}
.sp-center { text-align: center; padding: 60px 0; color: #666; }
.sp-error  { color: #d03050; }
.sp-auth   { max-width: 320px; margin: 80px auto; text-align: center; }
.sp-header { text-align: center; margin-bottom: 24px; }
.sp-header h1 { font-size: 28px; margin: 0 0 8px; }
.sp-logo   { height: 48px; margin-bottom: 12px; }
.sp-desc   { color: #666; }
.sp-overall {
  color: #fff;
  text-align: center;
  padding: 14px;
  border-radius: 6px;
  font-size: 16px;
  font-weight: 600;
  margin-bottom: 24px;
}
.sp-section { margin-bottom: 24px; }
.sp-section h2 { font-size: 18px; margin-bottom: 12px; border-bottom: 1px solid #eee; padding-bottom: 6px; }
.sp-incident {
  padding: 12px 16px;
  border-radius: 4px;
  margin-bottom: 10px;
}
.sp-incident-title { font-weight: 600; margin-bottom: 4px; }
.sp-time { color: #999; font-size: 12px; }
.sp-group { background: #fafafa; border: 1px solid #eee; border-radius: 6px; margin-bottom: 12px; overflow: hidden; }
.sp-group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  background: #f5f5f5;
  border-bottom: 1px solid #eee;
}
.sp-group-header h3 { margin: 0; font-size: 15px; }
.sp-monitor { padding: 8px 16px; border-bottom: 1px solid #f0f0f0; }
.sp-monitor:last-child { border-bottom: none; }
.sp-monitor-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.sp-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}
.sp-monitor-name  { flex: 1; font-size: 14px; }
.sp-monitor-status { font-size: 13px; color: #666; }
.sp-latency        { font-size: 12px; color: #999; }
.sp-uptime         { font-size: 13px; color: #18a058; margin-left: auto; }
.sp-footer { text-align: center; color: #999; font-size: 13px; margin-top: 32px; }
</style>
