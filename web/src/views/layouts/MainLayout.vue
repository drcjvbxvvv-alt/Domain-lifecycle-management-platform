<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NLayout, NLayoutSider, NLayoutContent, NAvatar, NDropdown } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route  = useRoute()
const auth   = useAuthStore()

const collapsed = ref(false)

// Nav items grouped for the sidebar
interface NavItem {
  label: string
  key: string
  icon: string
}

interface NavGroup {
  groupLabel: string
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    groupLabel: '總覽',
    items: [
      { label: '到期總覽',   key: '/dashboard/expiry', icon: 'clock'    },
    ],
  },
  {
    groupLabel: '業務管理',
    items: [
      { label: '專案管理',   key: '/projects',              icon: 'folder'   },
      { label: '域名管理',   key: '/domains',               icon: 'globe'    },
      { label: '批次匯入',   key: '/domains/import',        icon: 'upload'   },
      { label: '匯入歷史',   key: '/domains/import/history', icon: 'list'    },
      { label: '發布管理',   key: '/releases',              icon: 'rocket'   },
    ],
  },
  {
    groupLabel: '運維監控',
    items: [
      { label: '告警記錄',        key: '/alerts',       icon: 'bell'     },
      { label: 'Agent 管理',      key: '/agents',       icon: 'server'   },
      { label: 'Host Group 管理', key: '/host-groups',  icon: 'layers'   },
    ],
  },
  {
    groupLabel: '資產管理',
    items: [
      { label: 'Registrar 管理',    key: '/registrars',    icon: 'briefcase' },
      { label: 'DNS Provider 管理', key: '/dns-providers', icon: 'dns'       },
    ],
  },
  {
    groupLabel: '設定',
    items: [
      { label: '使用者管理',   key: '/settings/users',          icon: 'users'    },
      { label: '費率表管理',   key: '/settings/fee-schedules',  icon: 'currency' },
      { label: '標籤管理',     key: '/settings/tags',            icon: 'tag'      },
      { label: 'DNS 記錄範本', key: '/settings/dns-templates',   icon: 'dns'      },
    ],
  },
]

const activeKey = computed(() => {
  const path = route.path
  // Sort longest-first so /domains/import/history matches before /domains
  const allKeys = navGroups
    .flatMap(g => g.items.map(i => i.key))
    .sort((a, b) => b.length - a.length)
  return allKeys.find(k => path.startsWith(k)) ?? path
})

function navigate(key: string) {
  router.push(key)
}

const userDropdownOptions = [
  { label: '登出', key: 'logout' },
]

function onUserAction(key: string) {
  if (key === 'logout') {
    auth.logout()
    router.push('/login')
  }
}

// SVG icon helper — returns inline SVG string for nav icons
function getIconSvg(name: string): string {
  const icons: Record<string, string> = {
    folder: `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
    globe:  `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>`,
    rocket: `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z"/><path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z"/><path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0"/><path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"/></svg>`,
    bell:   `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>`,
    server: `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>`,
    users:  `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>`,
    layers:    `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>`,
    upload:    `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 16 12 12 8 16"/><line x1="12" y1="12" x2="12" y2="21"/><path d="M20.39 18.39A5 5 0 0 0 18 9h-1.26A8 8 0 1 0 3 16.3"/></svg>`,
    list:      `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>`,
  }
  return icons[name] ?? icons['folder']
}
</script>

<template>
  <NLayout style="height: 100vh;" has-sider>
    <!-- ─── Sidebar ─────────────────────────────────────────────────────── -->
    <NLayoutSider
      collapse-mode="width"
      :collapsed-width="56"
      :width="220"
      :collapsed="collapsed"
      @collapse="collapsed = true"
      @expand="collapsed = false"
      style="background: var(--bg-sidebar); border-right: 1px solid var(--sidebar-border);"
    >
      <!-- Logo / Brand -->
      <div class="sidebar-logo">
        <img src="/logo.svg" class="sidebar-logo__img" alt="域名平台" />
      </div>

      <!-- Collapse toggle -->
      <button class="sidebar-collapse-btn" @click="collapsed = !collapsed" :title="collapsed ? '展開' : '收合'">
        <svg v-if="!collapsed" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="15 18 9 12 15 6"/>
        </svg>
        <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="9 18 15 12 9 6"/>
        </svg>
      </button>

      <!-- Nav groups -->
      <nav class="sidebar-nav">
        <div v-for="group in navGroups" :key="group.groupLabel" class="sidebar-nav__group">
          <!-- Group label (hidden when collapsed) -->
          <span v-if="!collapsed" class="sidebar-nav__group-label">{{ group.groupLabel }}</span>

          <!-- Nav items -->
          <button
            v-for="item in group.items"
            :key="item.key"
            class="sidebar-nav__item"
            :class="{ 'sidebar-nav__item--active': activeKey === item.key }"
            :title="collapsed ? item.label : undefined"
            @click="navigate(item.key)"
          >
            <!-- Active indicator bar -->
            <span v-if="activeKey === item.key" class="sidebar-nav__active-bar" />
            <!-- Icon -->
            <span class="sidebar-nav__icon" v-html="getIconSvg(item.icon)" />
            <!-- Label -->
            <span v-if="!collapsed" class="sidebar-nav__label">{{ item.label }}</span>
          </button>
        </div>
      </nav>

    </NLayoutSider>

    <!-- ─── Main content area ───────────────────────────────────────────── -->
    <NLayout>
      <!-- Top header: chrome only — notifications + user. Page title lives in PageHeader. -->
      <div class="main-header">
        <div class="header-right">
          <!-- Notification bell (placeholder) -->
          <button class="header-icon-btn" title="通知">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/>
              <path d="M13.73 21a2 2 0 0 1-3.46 0"/>
            </svg>
          </button>

          <!-- User avatar dropdown -->
          <NDropdown :options="userDropdownOptions" @select="onUserAction">
            <div class="header-user">
              <NAvatar round size="small" style="background: #1e40af; color: #fff; font-size: 11px;">
                {{ auth.user?.username?.[0]?.toUpperCase() ?? 'U' }}
              </NAvatar>
              <span v-if="auth.user" class="header-user__name">{{ auth.user.username }}</span>
            </div>
          </NDropdown>
        </div>
      </div>

      <!-- Page content -->
      <NLayoutContent class="layout-content">
        <RouterView />
      </NLayoutContent>
    </NLayout>
  </NLayout>
</template>

<style scoped>
/* ── Sidebar Logo ─────────────────────────────────────────────────────── */
.sidebar-logo {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-bottom: 1px solid var(--sidebar-border);
}
.sidebar-logo__img {
  width: 32px;
  height: 32px;
  object-fit: contain;
  display: block;
}

/* ── Sidebar Collapse Button ──────────────────────────────────────────── */
.sidebar-collapse-btn {
  width: 100%;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 0 14px;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--sidebar-text);
  opacity: 0.5;
  transition: opacity 0.15s;
  margin-top: 4px;
}
.sidebar-collapse-btn:hover {
  opacity: 1;
}

/* ── Sidebar Nav ──────────────────────────────────────────────────────── */
.sidebar-nav {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px;
}
.sidebar-nav__group {
  margin-bottom: 4px;
}
.sidebar-nav__group-label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.8px;
  color: var(--sidebar-text);
  opacity: 0.5;
  padding: 12px 8px 4px;
  white-space: nowrap;
}
.sidebar-nav__item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: 36px;
  padding: 0 10px;
  border-radius: 8px;
  border: none;
  background: none;
  cursor: pointer;
  color: var(--sidebar-text);
  font-size: 13px;
  font-weight: 500;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  transition: background 0.12s, color 0.12s;
  margin-bottom: 1px;
}
.sidebar-nav__item:hover {
  background: var(--bg-sidebar-hover);
  color: #cbd5e1;
}
.sidebar-nav__item--active {
  background: var(--bg-sidebar-active);
  color: var(--sidebar-text-active);
}
.sidebar-nav__active-bar {
  position: absolute;
  left: 0;
  top: 6px;
  bottom: 6px;
  width: 3px;
  border-radius: 0 2px 2px 0;
  background: var(--primary);
}
.sidebar-nav__icon {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  width: 16px;
  height: 16px;
}
.sidebar-nav__label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}


/* ── Main Header ──────────────────────────────────────────────────────── */
.main-header {
  height: var(--header-height);
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 0 24px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  box-shadow: var(--shadow-sm);
  flex-shrink: 0;
}
.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.header-icon-btn {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  border: none;
  background: none;
  cursor: pointer;
  color: var(--text-muted);
  transition: background 0.12s, color 0.12s;
}
.header-icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.header-user {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 8px;
  transition: background 0.12s;
}
.header-user:hover {
  background: var(--bg-hover);
}
.header-user__name {
  font-size: 13px;
  color: var(--text-secondary);
}

/* ── Layout Content ───────────────────────────────────────────────────── */
.layout-content {
  padding: 24px;
  overflow-y: auto;
  height: calc(100vh - var(--header-height));
}
</style>
