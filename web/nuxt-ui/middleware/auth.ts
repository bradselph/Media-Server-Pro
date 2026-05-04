// Protect routes that require a logged-in user.
// Usage: definePageMeta({ middleware: 'auth' })
export default defineNuxtRouteMiddleware(async to => {
    const authStore = useAuthStore()
    // Wait for any in-flight session fetch to finish before deciding. Otherwise
    // a race between isLoading=true and isLoggedIn settling could redirect a
    // user who is in fact authenticated.
    if (authStore.isLoading) {
        await authStore.fetchSession()
    }
    if (!authStore.isLoggedIn) {
        return navigateTo({path: '/login', query: {redirect: to.fullPath}})
    }
})
