import { THEMES, type ThemeValue } from '~/stores/theme'

// Initialize auth state on app startup by checking the session
export default defineNuxtPlugin(async () => {
    const authStore = useAuthStore()
    await authStore.fetchSession()
    // Apply the server-saved theme after session load. This overrides any locally
    // stored theme preference, ensuring server preferences take precedence when user logs in.
    if (import.meta.client && authStore.user?.preferences?.theme) {
        const themeStore = useThemeStore()
        const serverTheme = authStore.user.preferences.theme
        if (THEMES.some(t => t.value === serverTheme)) {
            themeStore.setTheme(serverTheme as ThemeValue)
        }
    }
})
