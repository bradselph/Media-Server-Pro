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
        equalizer_preset: '',
        resume_playback: true,
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
        autoplay_similar: true,
        accent_hue: 220,
    }
}

// Private-session flag is module-scoped so it survives Pinia store
// rehydration in dev hot-reload AND so useApi (which is imported before
// the auth store is mounted) can read it via the exported getter without
// triggering a store dependency.
const privateSessionFlag = ref(false)

const LS_PRIVATE_SESSION = 'msp-private-session'

if (typeof window !== 'undefined') {
    // Restore on first import so the next page reload (or refresh) honors
    // the user's prior toggle. Cleared explicitly on logout.
    privateSessionFlag.value = window.localStorage.getItem(LS_PRIVATE_SESSION) === '1'
    watch(privateSessionFlag, v => {
        try {
            if (v) window.localStorage.setItem(LS_PRIVATE_SESSION, '1')
            else window.localStorage.removeItem(LS_PRIVATE_SESSION)
        } catch { /* localStorage may be blocked */
        }
    })
}

/**
 * Exported for useApi.ts to read at request-build time. Returns the
 * current value of the private-session flag without forcing a Pinia
 * store creation (useApi runs at module-load time inside
 * useApiEndpoints.ts and cannot depend on Pinia being mounted).
 */
export function isPrivateSession(): boolean {
    return privateSessionFlag.value
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
    // Private-session toggle (B.2 retention plan). When on, useApi attaches
    // X-MSP-Private: 1 to every request and the backend skips history/
    // analytics writes for the duration of the toggle.
    const privateSession = privateSessionFlag

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
        // Reset the private-session flag on logout so the next user that
        // signs in on this device starts in normal mode.
        privateSessionFlag.value = false
    }

    function togglePrivateSession() {
        privateSessionFlag.value = !privateSessionFlag.value
    }

    return {
        user, allowGuests, isLoading, isLoggedIn, isAdmin, username, thumbnailNonce,
        privateSession, fetchSession, login, logout, togglePrivateSession,
    }
})
