import {create} from 'zustand'
import {persist} from 'zustand/middleware'
import {builtInThemes} from '@/themes/themes'
import {applyThemeById} from '@/themes/themeEngine'

/**
 * Theme ID — can be any built-in theme id, "auto", or a custom theme id.
 * "auto" follows the OS/browser prefers-color-scheme setting (resolves to
 * "light" or "dark").
 */
export type ThemeId = string

interface ThemeState {
    theme: ThemeId
    setTheme: (theme: ThemeId) => void
    /** Cycle through built-in themes (including "auto" at the end). */
    toggleTheme: () => void
}

/** The ordered cycle used by toggleTheme(). */
const cycle: string[] = [...builtInThemes.map(t => t.id), 'auto']

export const useThemeStore = create<ThemeState>()(
    persist(
        (set, get) => ({
            theme: 'dark' as ThemeId,

            setTheme: (theme: ThemeId) => {
                applyThemeById(theme)
                set({theme})
            },

            toggleTheme: () => {
                const i = cycle.indexOf(get().theme)
                const next = cycle[(i + 1) % cycle.length]
                applyThemeById(next)
                set({theme: next})
            },
        }),
        {
            name: 'media-server-theme',
            onRehydrateStorage: () => (state) => {
                if (state?.theme) applyThemeById(state.theme)
            },
        },
    ),
)

// Apply theme from persisted storage on initial load so the first paint matches
// the user's last choice. Zustand persist rehydrates async, so we read localStorage
// synchronously to avoid a flash of default (dark) before rehydration applies stored theme.
const PERSIST_KEY = 'media-server-theme'
function getInitialTheme(): ThemeId {
    try {
        const raw = localStorage.getItem(PERSIST_KEY)
        if (!raw) return 'dark'
        const parsed = JSON.parse(raw) as { state?: { theme?: string } }
        const t = parsed?.state?.theme
        if (typeof t === 'string' && t.length > 0) return t
    } catch {
        /* ignore */
    }
    return 'dark'
}
applyThemeById(getInitialTheme())
