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

// Apply theme from persisted storage on initial load so the first paint matches
// the user's last choice. Zustand persist rehydrates async, so we read localStorage
// synchronously to avoid a flash of default (dark) before rehydration applies stored theme.
const PERSIST_KEY = 'media-server-theme'
function getInitialTheme(): Theme {
    try {
        const raw = localStorage.getItem(PERSIST_KEY)
        if (!raw) return 'dark'
        const parsed = JSON.parse(raw) as { state?: { theme?: string } }
        const t = parsed?.state?.theme
        if (t === 'light' || t === 'dark' || t === 'auto') return t as Theme
    } catch {
        /* ignore */
    }
    return 'dark'
}
applyTheme(getInitialTheme())
