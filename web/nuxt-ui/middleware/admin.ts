// Protect routes that require admin role.
// Usage: definePageMeta({ middleware: 'admin' })
//
// NOTE: plugins/auth.ts is an async plugin that awaits fetchSession() before
// any navigation occurs. By the time this middleware runs, isLoading is always
// false. The isLoading guard below is a defensive belt-and-suspenders check that
// prevents a redirect-flash if that invariant ever changes (e.g. lazy plugin).
export default defineNuxtRouteMiddleware(to => {
    const authStore = useAuthStore()
    // Block navigation while session is still resolving — do not allow through
    if (authStore.isLoading) return abortNavigation()
    if (!authStore.isAdmin) {
        return navigateTo({path: '/admin-login', query: {redirect: to.fullPath}})
    }
})
