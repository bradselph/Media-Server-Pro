// Protect routes that require admin role.
// Usage: definePageMeta({ middleware: 'admin' })
export default defineNuxtRouteMiddleware(to => {
  const authStore = useAuthStore()
  if (!authStore.isLoading && !authStore.isAdmin) {
    return navigateTo({ path: '/admin-login', query: { redirect: to.fullPath } })
  }
})
