import {create} from 'zustand'
import {persist} from 'zustand/middleware'

// IC-11: 'auto' follows the OS/browser prefers-color-scheme setting
export type Theme = 'light' | 'dark' | 'auto'

interface ThemeState {
    theme: Theme
    setTheme: (theme: Theme) => void
    toggleTheme: () => void
}

function resolveTheme(theme: Theme): 'light' | 'dark' {
    if (theme === 'auto') {
        return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    }
    return theme
}

function applyTheme(theme: Theme) {
    document.documentElement.setAttribute('data-theme', resolveTheme(theme))
}

export const useThemeStore = create<ThemeState>()(
    persist(
        (set, get) => ({
            theme: 'dark' as Theme,

            setTheme: (theme: Theme) => {
                applyTheme(theme)
                set({theme})
            },

            toggleTheme: () => {
                const cycle: Theme[] = ['dark', 'light', 'auto']
                const i = cycle.indexOf(get().theme)
                const next = cycle[(i + 1) % cycle.length]
                applyTheme(next)
                set({theme: next})
            },
        }),
        {
            name: 'media-server-theme',
            onRehydrateStorage: () => (state) => {
                if (state?.theme) applyTheme(state.theme)
            },
        },
    ),
)

// Apply theme from persisted storage on initial load (before async rehydration)
const initialTheme = useThemeStore.getState().theme
applyTheme(initialTheme)
