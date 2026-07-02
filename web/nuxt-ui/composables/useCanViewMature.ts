import type {ComputedRef} from 'vue'

/**
 * useCanViewMature — shared computed for whether the current user may view
 * mature content: they must be logged in AND have both the show_mature
 * preference and the can_view_mature permission. Used by the player and the
 * home page so the gate stays consistent.
 */
export function useCanViewMature(): ComputedRef<boolean> {
    const authStore = useAuthStore()
    return computed(() =>
        authStore.isLoggedIn &&
        (authStore.user?.preferences?.show_mature ?? false) &&
        (authStore.user?.permissions?.can_view_mature ?? false),
    )
}
