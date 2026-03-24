import { defineStore } from 'pinia'
import type { User, UserPermissions, UserPreferences } from '~/types/api'

function defaultPermissions(): UserPermissions {
  return {
    can_upload: false,
    can_download: false,
    can_delete: false,
    can_manage_playlists: false,
    can_view_mature: false,
    bypass_age_gate: false,
    max_storage_mb: 0,
  }
}

function defaultPreferences(): UserPreferences {
  return {
    theme: '',
    playback_speed: 1,
    volume: 1,
    auto_play: false,
    resume_playback: false,
    items_per_page: 24,
    view_mode: 'grid',
    default_quality: '',
    language: '',
    equalizer_preset: '',
    sort_by: '',
    sort_order: '',
    filter_category: '',
    filter_media_type: '',
    show_mature: false,
    show_analytics: true,
    show_home_recently_added: true,
    show_home_continue_watching: true,
    show_home_suggestions: true,
  }
}

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

  async function login(uname: string, password: string) {
    const { login: apiLogin } = useApiEndpoints()
    const res = await apiLogin(uname, password)
    // Login returns flat fields, not a nested user object
    user.value = {
      id: '',
      username: res.username,
      role: res.role,
      enabled: true,
      created_at: '',
      permissions: defaultPermissions(),
      preferences: defaultPreferences(),
    }
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
