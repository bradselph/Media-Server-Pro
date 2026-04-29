import { THEMES, type ThemeValue } from '~/stores/theme'

// Initialize auth state on app startup by checking the session
export default defineNuxtPlugin(async () => {
    const authStore = useAuthStore()
    await authStore.fetchSession()
    // Apply the server-saved theme after session load so it takes effect on
    // new devices/browsers where localStorage doesn't already have a value.
    if (import.meta.client && authStore.user?.preferences?.theme) {
        const themeStore = useThemeStore()
        const serverTheme = authStore.user.preferences.theme
        if (THEMES.some(t => t.value === serverTheme)) {
            themeStore.setTheme(serverTheme as ThemeValue)
        }
    }
})
