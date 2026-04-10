// Protect routes that require a logged-in user.
// Usage: definePageMeta({ middleware: 'auth' })
export default defineNuxtRouteMiddleware(to => {
    const authStore = useAuthStore()
    // Block navigation while session is still resolving — do not allow through
    if (authStore.isLoading) return abortNavigation()
    if (!authStore.isLoggedIn) {
        return navigateTo({path: '/login', query: {redirect: to.fullPath}})
    }
})
