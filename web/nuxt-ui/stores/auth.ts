import {defineStore} from 'pinia'
import type {User, UserPermissions, UserPreferences} from '~/types/api'
import {normalizeUser} from '~/utils/apiCompat'

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
        skip_interval: 10,
        shuffle_enabled: false,
        show_buffer_bar: true,
        download_prompt: true,
        // FND-0046: Include custom_eq_presets field (even if undefined) for type consistency
        // between default and server-normalized preferences.
        custom_eq_presets: undefined,
    }
}

export const useAuthStore = defineStore('auth', () => {
    const user = ref<User | null>(null)
    const allowGuests = ref(false)
    const isLoading = ref(true)
    // Incremented each time a user logs in so that thumbnail URLs change,
    // forcing the browser to re-request mature-gated images instead of
    // serving the cached censored placeholder from the pre-login session.
    const thumbnailNonce = ref(0)

    const isLoggedIn = computed(() => !!user.value)
    const isAdmin = computed(() => user.value?.role === 'admin')
    const username = computed(() => user.value?.username ?? '')

    async function fetchSession() {
        isLoading.value = true
        try {
            const {getSession} = useApiEndpoints()
            const res = await getSession()
            allowGuests.value = res.allow_guests
            user.value = res.authenticated ? (normalizeUser(res.user) ?? null) : null
        } catch (e) {
            // FND-0045: Log error to distinguish network failures from logged-out state.
            // Only clear user if server explicitly says "not authenticated"; preserve user
            // on transient errors so login flow doesn't get disrupted (FND-0044).
            console.warn('[auth] fetchSession failed:', e)
            // Preserve existing user.value on transient errors; don't null it out.
            // Only null if server explicitly says not-authenticated (handled above in try block).
        } finally {
            isLoading.value = false
        }
    }

    let loginInProgress = false

    async function login(uname: string, password: string) {
        if (loginInProgress) throw new Error('Login already in progress')
        loginInProgress = true
        try {
            const {login: apiLogin} = useApiEndpoints()
            const res = await apiLogin(uname, password)
            // Set a minimal user immediately so isLoggedIn becomes true right away,
            // then fetch the full session (id, real permissions, real preferences).
            user.value = {
                id: '',
                username: res.username,
                role: res.role,
                type: 'standard',
                enabled: true,
                created_at: '',
                storage_used: 0,
                active_streams: 0,
                permissions: defaultPermissions(),
                preferences: defaultPreferences(),
            }
            // Overwrite with real server data (permissions, preferences, id).
            // Errors are intentionally swallowed — the minimal user above is sufficient fallback.
            await fetchSession().catch(() => {
            })
            // Bump nonce so thumbnail URLs change and the browser re-requests
            // mature-gated images rather than serving the cached censored version.
            thumbnailNonce.value++
            return res
        } finally {
            loginInProgress = false
        }
    }

    async function logout() {
        const {logout: apiLogout} = useApiEndpoints()
        try {
            await apiLogout()
        } catch (e) {
            // FND-0043: Log error so failures are observable; we still clear local user state
            // for UX reasons (best-effort logout) but error is logged for monitoring.
            console.error('[auth] server-side logout failed (session may still be active):', e)
        }
        // Clear local user state even if server logout failed, for best-effort UX.
        user.value = null
    }

    return {user, allowGuests, isLoading, isLoggedIn, isAdmin, username, thumbnailNonce, fetchSession, login, logout}
})
