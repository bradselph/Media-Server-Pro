import {create} from 'zustand'
import {authApi, preferencesApi} from '@/api/endpoints'
import type {User, UserPermissions, UserPreferences} from '@/api/types'

interface AuthState {
    user: User | null
    isAuthenticated: boolean
    isAdmin: boolean
    allowGuests: boolean
    permissions: UserPermissions
    isLoading: boolean

    // Actions
    checkSession: () => Promise<void>
    login: (username: string, password: string) => Promise<{ isAdmin: boolean }>
    logout: () => Promise<void>
    updatePreferences: (prefs: Partial<UserPreferences>) => Promise<void>
    setUser: (user: User | null) => void
}

const defaultPermissions: UserPermissions = {
    can_stream: false,
    can_download: false,
    can_upload: false,
    can_delete: false,
    can_manage: false,
    can_view_mature: false,
    can_create_playlists: false,
}

export const useAuthStore = create<AuthState>((set, get) => ({
    user: null,
    isAuthenticated: false,
    isAdmin: false,
    allowGuests: false,
    permissions: defaultPermissions,
    isLoading: true,

    checkSession: async () => {
        try {
            // getSession() returns allow_guests + user without 401-ing for guests
            const session = await authApi.getSession()
            set({
                user: session.user ?? null,
                isAuthenticated: session.authenticated,
                isAdmin: session.user?.role === 'admin',
                allowGuests: session.allow_guests,
                permissions: session.user?.permissions ?? defaultPermissions,
                isLoading: false,
            })
        } catch (err) {
            // IC-08: preserve auth state on transient errors; only clear on explicit 401
            const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : undefined
            if (status === 401) {
                set({
                    user: null,
                    isAuthenticated: false,
                    isAdmin: false,
                    allowGuests: false,
                    permissions: defaultPermissions,
                    isLoading: false,
                })
            } else {
                // Transient network/server error — don't log user out
                set({isLoading: false})
            }
        }
    },

    login: async (username: string, password: string) => {
        const result = await authApi.login(username, password)
        // After login, fetch full user data
        await get().checkSession()
        return {isAdmin: result.is_admin}
    },

    logout: async () => {
        try {
            await authApi.logout()
        } finally {
            // Preserve allowGuests — it's a server config value, unchanged by logout
            set({
                user: null,
                isAuthenticated: false,
                isAdmin: false,
                permissions: defaultPermissions,
            })
        }
    },

    updatePreferences: async (prefs: Partial<UserPreferences>) => {
        const updated = await preferencesApi.update(prefs)
        const user = get().user
        if (user) {
            set({user: {...user, preferences: {...user.preferences, ...updated}}})
        }
    },

    setUser: (user: User | null) =>
        set({
            user,
            isAuthenticated: user !== null,
            isAdmin: user?.role === 'admin',
            permissions: user?.permissions ?? defaultPermissions,
        }),
}))
