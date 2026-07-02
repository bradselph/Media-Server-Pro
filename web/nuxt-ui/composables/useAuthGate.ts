/**
 * useAuthGate — runs fn once the user session is known and authenticated.
 *
 * Fires fn on mount when the session is already resolved and a user is present,
 * and otherwise on the first authentication change afterward. Guards against a
 * double-run so a page's initial data load happens exactly once. Replaces the
 * hand-rolled `hasFetched` + onMounted + watch trio duplicated across pages.
 */
export function useAuthGate(fn: () => void) {
    const authStore = useAuthStore()
    let fetched = false
    const run = () => {
        fetched = true
        fn()
    }
    onMounted(() => {
        if (!authStore.isLoading && authStore.user) run()
    })
    watch(() => authStore.user, (user) => {
        if (user && !fetched) run()
    })
}
