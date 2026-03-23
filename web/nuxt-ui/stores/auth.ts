import { defineStore } from 'pinia'
import type { User } from '~/types/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const isLoading = ref(true)

  const isLoggedIn = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.role === 'admin')
  const username = computed(() => user.value?.username ?? '')

  async function fetchSession() {
    isLoading.value = true
    try {
      const { getSession } = useApiEndpoints()
      const res = await getSession()
      user.value = res.authenticated ? (res.user ?? null) : null
    } catch {
      user.value = null
    } finally {
      isLoading.value = false
    }
  }

  async function login(username: string, password: string) {
    const { login: apiLogin } = useApiEndpoints()
    const res = await apiLogin(username, password)
    user.value = res.user
    return res
  }

  async function logout() {
    const { logout: apiLogout } = useApiEndpoints()
    try { await apiLogout() } catch {}
    user.value = null
  }

  function clear() {
    user.value = null
  }

  function setUser(u: User) {
    user.value = u
  }

  return { user, isLoading, isLoggedIn, isAdmin, username, fetchSession, login, logout, clear, setUser }
})
