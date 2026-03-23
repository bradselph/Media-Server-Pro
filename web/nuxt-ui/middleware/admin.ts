// Protect routes that require admin role.
// Usage: definePageMeta({ middleware: 'admin' })
export default defineNuxtRouteMiddleware(() => {
  const authStore = useAuthStore()
  if (!authStore.isLoading && !authStore.isAdmin) {
    return navigateTo('/admin-login')
  }
})
