import { defineStore } from 'pinia'
import type { User, UserPermissions, UserPreferences } from '~/types/api'

function defaultPermissions(): UserPermissions {
  return {
    can_stream: true,
    can_download: false,
    can_upload: false,
    can_delete: false,
    can_manage: false,
    can_view_mature: false,
    can_create_playlists: true,
  }
}

function defaultPreferences(): UserPreferences {
  return {
    theme: 'dark',
    view_mode: 'grid',
    default_quality: 'auto',
    auto_play: false,
    playback_speed: 1,
    volume: 1,
    show_mature: false,
    mature_preference_set: false,
    language: 'en',
    equalizer_preset: '',
    resume_playback: true,
    show_analytics: true,
    items_per_page: 20,
    sort_by: 'date_added',
    sort_order: 'desc',
    filter_category: '',
    filter_media_type: '',
    show_continue_watching: true,
    show_recommended: true,
    show_trending: true,
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
