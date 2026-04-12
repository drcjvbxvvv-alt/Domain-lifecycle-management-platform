<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NLayout, NLayoutSider, NLayoutContent, NMenu, NAvatar, NDropdown, NText } from 'naive-ui'
import type { MenuOption } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route  = useRoute()
const auth   = useAuthStore()

const collapsed = ref(false)

const menuOptions: MenuOption[] = [
  {
    label: '專案管理',
    key: '/projects',
    icon: () => '📁',
  },
  {
    label: '域名管理',
    key: '/domains',
    icon: () => '🌐',
  },
  {
    label: '發布管理',
    key: '/releases',
    icon: () => '🚀',
  },
  {
    label: '告警記錄',
    key: '/alerts',
    icon: () => '🔔',
  },
  {
    label: 'Agent 管理',
    key: '/agents',
    icon: () => '🖥️',
  },
  {
    label: '使用者管理',
    key: '/settings/users',
    icon: () => '👥',
  },
]

const activeKey = computed(() => {
  const path = route.path
  // Match longest prefix
  const keys = menuOptions.map(o => o.key as string)
  return keys.find(k => path.startsWith(k)) ?? path
})

function onMenuSelect(key: string) {
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
</script>

<template>
  <NLayout style="height: 100vh;" has-sider>
    <!-- Sidebar -->
    <NLayoutSider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="220"
      :collapsed="collapsed"
      show-trigger
      @collapse="collapsed = true"
      @expand="collapsed = false"
      style="background: var(--bg-sidebar);"
    >
      <!-- Logo -->
      <div class="sidebar-logo">
        <span v-if="!collapsed" class="logo-text">域名平台</span>
        <span v-else class="logo-icon">🌐</span>
      </div>

      <NMenu
        :options="menuOptions"
        :value="activeKey"
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="20"
        @update:value="onMenuSelect"
      />
    </NLayoutSider>

    <!-- Main content area -->
    <NLayout>
      <!-- Top header -->
      <div class="main-header">
        <div class="header-title">
          <NText>{{ route.meta.title ?? '域名生命週期管理平台' }}</NText>
        </div>
        <div class="header-right">
          <NDropdown :options="userDropdownOptions" @select="onUserAction">
            <div class="user-menu">
              <NAvatar round size="small">
                {{ auth.user?.username?.[0]?.toUpperCase() ?? 'U' }}
              </NAvatar>
              <span v-if="auth.user" class="user-name">{{ auth.user.username }}</span>
            </div>
          </NDropdown>
        </div>
      </div>

      <!-- Page content -->
      <NLayoutContent class="page-content">
        <RouterView />
      </NLayoutContent>
    </NLayout>
  </NLayout>
</template>

<style scoped>
.sidebar-logo {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  border-bottom: 1px solid var(--border);
  font-weight: 700;
  font-size: 16px;
  color: var(--primary);
  white-space: nowrap;
  overflow: hidden;
}

.logo-text { letter-spacing: 1px; }
.logo-icon { font-size: 20px; }

.main-header {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
}

.header-title { font-weight: 600; font-size: 15px; color: var(--text-primary); }

.header-right { display: flex; align-items: center; gap: 12px; }

.user-menu {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: background 0.15s;
}
.user-menu:hover { background: var(--bg-hover); }
.user-name { font-size: 13px; color: var(--text-secondary); }

.page-content {
  padding: 24px;
  overflow-y: auto;
  height: calc(100vh - 56px);
}
</style>
