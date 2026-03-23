// Protect routes that require a logged-in user.
// Usage: definePageMeta({ middleware: 'auth' })
export default defineNuxtRouteMiddleware(() => {
  const authStore = useAuthStore()
  if (!authStore.isLoading && !authStore.isLoggedIn) {
    return navigateTo('/login')
  }
})
