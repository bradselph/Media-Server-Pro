// Initialize auth state on app startup by checking the session
export default defineNuxtPlugin(async () => {
  const authStore = useAuthStore()
  await authStore.fetchSession()
})
