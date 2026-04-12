import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { http } from '@/utils/http'

interface LoginData {
  token:    string
  user_id:  number
  username: string
  roles:    string[]
}

interface StoredUser {
  user_id:  number
  username: string
  roles:    string[]
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('token'))
  function loadUser(): StoredUser | null {
    try { return JSON.parse(localStorage.getItem('user') ?? 'null') } catch { return null }
  }
  const user = ref<StoredUser | null>(loadUser())

  const isLoggedIn = computed(() => !!token.value)
  const role       = computed(() => user.value?.roles?.[0] ?? '')

  async function login(username: string, password: string) {
    const res = (await http.post('/auth/login', { username, password })) as { data: LoginData }
    const d   = res.data
    token.value = d.token
    user.value  = { user_id: d.user_id, username: d.username, roles: d.roles }
    localStorage.setItem('token', d.token)
    localStorage.setItem('user', JSON.stringify(user.value))
  }

  function logout() {
    token.value = null
    user.value  = null
    localStorage.removeItem('token')
    localStorage.removeItem('user')
  }

  return { token, user, isLoggedIn, role, login, logout }
})
