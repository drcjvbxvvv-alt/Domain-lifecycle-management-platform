import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/LoginView.vue'),
      meta: { requiresAuth: false },
    },

    // ── Main layout ──────────────────────────────────────────
    {
      path: '/',
      component: () => import('@/views/layouts/MainLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        {
          path: '',
          redirect: '/projects',
        },

        // Dashboard
        {
          path: 'dashboard',
          name: 'Dashboard',
          component: () => import('@/views/DashboardView.vue'),
          meta: { title: 'Dashboard', minRole: 'viewer' },
        },

        // ── Projects ──────────────────────────────────────────
        {
          path: 'projects',
          name: 'ProjectList',
          component: () => import('@/views/projects/ProjectList.vue'),
          meta: { title: '專案管理', minRole: 'viewer' },
        },
        {
          path: 'projects/:id',
          name: 'ProjectDetail',
          component: () => import('@/views/projects/ProjectDetail.vue'),
          meta: { title: '專案詳情', minRole: 'viewer' },
        },
        {
          path: 'projects/:id/domains',
          name: 'ProjectDomainList',
          component: () => import('@/views/domains/DomainList.vue'),
          meta: { title: '域名列表', minRole: 'viewer' },
        },
        {
          path: 'projects/:id/templates',
          name: 'TemplateList',
          component: () => import('@/views/projects/TemplateList.vue'),
          meta: { title: '範本列表', minRole: 'viewer' },
        },
        {
          path: 'projects/:id/templates/:tid',
          name: 'TemplateDetail',
          component: () => import('@/views/projects/TemplateDetail.vue'),
          meta: { title: '範本詳情', minRole: 'viewer' },
        },
        {
          path: 'projects/:id/releases',
          name: 'ProjectReleaseList',
          component: () => import('@/views/releases/ReleaseList.vue'),
          meta: { title: '發布列表', minRole: 'viewer' },
        },
        {
          path: 'projects/:id/releases/:rid',
          name: 'ReleaseDetail',
          component: () => import('@/views/releases/ReleaseDetail.vue'),
          meta: { title: '發布詳情', minRole: 'viewer' },
        },

        // ── Domains (global) ───────────────────────────────────
        {
          path: 'domains',
          name: 'DomainList',
          component: () => import('@/views/domains/DomainList.vue'),
          meta: { title: '域名列表', minRole: 'viewer' },
        },
        {
          path: 'domains/:id',
          name: 'DomainDetail',
          component: () => import('@/views/domains/DomainDetail.vue'),
          meta: { title: '域名詳情', minRole: 'viewer' },
        },

        // ── Releases (global) ──────────────────────────────────
        {
          path: 'releases',
          name: 'ReleaseList',
          component: () => import('@/views/releases/ReleaseList.vue'),
          meta: { title: '發布管理', minRole: 'viewer' },
        },

        // ── Alerts ────────────────────────────────────────────
        {
          path: 'alerts',
          name: 'AlertList',
          component: () => import('@/views/AlertList.vue'),
          meta: { title: '告警記錄', minRole: 'viewer' },
        },

        // ── Agents ────────────────────────────────────────────
        {
          path: 'agents',
          name: 'AgentList',
          component: () => import('@/views/agents/AgentList.vue'),
          meta: { title: 'Agent 管理', minRole: 'viewer' },
        },
        {
          path: 'agents/:id',
          name: 'AgentDetail',
          component: () => import('@/views/agents/AgentDetail.vue'),
          meta: { title: 'Agent 詳情', minRole: 'viewer' },
        },

        // ── Settings ──────────────────────────────────────────
        {
          path: 'settings/users',
          name: 'UserList',
          component: () => import('@/views/settings/UserList.vue'),
          meta: { title: '使用者管理', minRole: 'admin' },
        },
      ],
    },

    // 404
    {
      path: '/:pathMatch(.*)*',
      name: 'NotFound',
      component: () => import('@/views/NotFound.vue'),
    },
  ],
})

// Navigation guard
router.beforeEach((to) => {
  const auth = useAuthStore()

  if (to.meta.requiresAuth !== false && !auth.isLoggedIn) {
    return { name: 'Login', query: { redirect: to.fullPath } }
  }

  if (to.name === 'Login' && auth.isLoggedIn) {
    return { name: 'ProjectList' }
  }
})

export default router
