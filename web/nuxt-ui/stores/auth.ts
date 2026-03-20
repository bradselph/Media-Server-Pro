/**
 * Auth store — mirrors the React authStore behavior.
 *
 * Manages user session state, authentication status, and permissions.
 * Uses Pinia for state management (replacing Zustand from the React app).
 */

import { defineStore } from 'pinia'
import type { User, UserPermissions, UserPreferences } from '~/types/api'

const defaultPermissions: UserPermissions = {
  can_stream: false,
  can_download: false,
  can_upload: false,
  can_delete: false,
  can_manage: false,
  can_view_mature: false,
  can_create_playlists: false,
}

interface AuthState {
  user: User | null
  isAuthenticated: boolean
  isAdmin: boolean
  allowGuests: boolean
  permissions: UserPermissions
  isLoading: boolean
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    user: null,
    isAuthenticated: false,
    isAdmin: false,
    allowGuests: false,
    permissions: defaultPermissions,
    isLoading: true,
  }),

  actions: {
    async checkSession() {
      const { getSession } = useApiEndpoints()
      try {
        const session = await getSession()
        this.user = session.user ?? null
        this.isAuthenticated = session.authenticated
        this.isAdmin = session.user?.role === 'admin'
        this.allowGuests = session.allow_guests
        this.permissions = session.user?.permissions ?? defaultPermissions
        this.isLoading = false
      } catch (err: any) {
        // Preserve auth state on transient errors; only clear on explicit 401
        const status = err?.status as number | undefined
        if (status === 401) {
          this.user = null
          this.isAuthenticated = false
          this.isAdmin = false
          this.allowGuests = false
          this.permissions = defaultPermissions
        }
        this.isLoading = false
      }
    },

    async login(username: string, password: string): Promise<{ isAdmin: boolean }> {
      const { login } = useApiEndpoints()
      const result = await login(username, password)
      // After login, fetch full user data
      await this.checkSession()
      return { isAdmin: result.is_admin }
    },

    async logout() {
      const { logout } = useApiEndpoints()
      const previousAllowGuests = this.allowGuests
      try {
        await logout()
      } finally {
        // Preserve allowGuests — it's a server config value, unchanged by logout
        this.user = null
        this.isAuthenticated = false
        this.isAdmin = false
        this.permissions = defaultPermissions
        this.allowGuests = previousAllowGuests
      }
    },

    async updatePreferences(prefs: Partial<UserPreferences>) {
      const { updatePreferences } = useApiEndpoints()
      const updated = await updatePreferences(prefs)
      if (this.user) {
        this.user = {
          ...this.user,
          preferences: { ...this.user.preferences, ...updated },
        }
      }
    },

    setUser(user: User | null) {
      this.user = user
      this.isAuthenticated = user !== null
      this.isAdmin = user?.role === 'admin'
      this.permissions = user?.permissions ?? defaultPermissions
    },
  },
})
