// Protect routes that require a logged-in user.
// Usage: definePageMeta({ middleware: 'auth' })
export default defineNuxtRouteMiddleware(to => {
    const authStore = useAuthStore()
    // While session is still loading, redirect to login with the intended path as
    // the redirect target. abortNavigation() would leave users on a blank page with
    // no recovery path; redirecting to login provides a usable fallback.
    if (authStore.isLoading || !authStore.isLoggedIn) {
        return navigateTo({path: '/login', query: {redirect: to.fullPath}})
    }
})
