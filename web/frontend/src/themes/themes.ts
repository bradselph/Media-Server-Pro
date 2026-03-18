/**
 * Theme definitions for Media Server Pro.
 *
 * Each theme is a flat map of CSS custom-property values.  The theme engine
 * applies them by setting `style.setProperty` on `document.documentElement`.
 *
 * To add a new theme, create a `ThemeDefinition` and add it to `builtInThemes`.
 * Users can also supply their own `ThemeDefinition` via the custom-theme API.
 */

/** Every CSS variable the theme system controls. */
export interface ThemeTokens {
    /* ── Brand / accent ── */
    '--primary-color': string
    '--primary-hover': string
    '--primary-rgb': string          // e.g. "102,126,234" for rgba() usage

    /* ── Gradient (header, auth page, etc.) ── */
    '--gradient-start': string
    '--gradient-end': string

    /* ── Surfaces ── */
    '--background': string
    '--card-bg': string
    '--input-bg': string

    /* ── Text ── */
    '--text-color': string
    '--text-muted': string

    /* ── Borders & hover ── */
    '--border-color': string
    '--hover-bg': string

    /* ── Semantic status ── */
    '--success-color': string
    '--danger-color': string
    '--warning-color': string
    '--info-color': string

    /* ── Media type badges ── */
    '--badge-video': string
    '--badge-audio': string

    /* ── Admin accent (header gradient) ── */
    '--admin-gradient-start': string
    '--admin-gradient-end': string

    /* ── Shadows ── */
    '--shadow-color': string         // base color for box-shadow rgba
}

export interface ThemeDefinition {
    /** Unique ID used for persistence (e.g. "dark", "midnight"). */
    id: string
    /** Human-readable display name. */
    label: string
    /** The base mode for the theme — drives `data-theme` on <html>. */
    base: 'light' | 'dark'
    /** CSS custom-property values. */
    tokens: ThemeTokens
}

/* ─────────────────────────  Built-in themes  ───────────────────────── */

const lightTokens: ThemeTokens = {
    '--primary-color':        '#667eea',
    '--primary-hover':        '#5a6fd6',
    '--primary-rgb':          '102,126,234',
    '--gradient-start':       '#667eea',
    '--gradient-end':         '#764ba2',
    '--background':           '#f5f5f5',
    '--card-bg':              '#ffffff',
    '--input-bg':             '#f9fafb',
    '--text-color':           '#333333',
    '--text-muted':           '#666666',
    '--border-color':         '#e0e0e0',
    '--hover-bg':             'rgba(0, 0, 0, 0.06)',
    '--success-color':        '#10b981',
    '--danger-color':         '#ef4444',
    '--warning-color':        '#f59e0b',
    '--info-color':           '#3b82f6',
    '--badge-video':          '#3b82f6',
    '--badge-audio':          '#8b5cf6',
    '--admin-gradient-start': '#1e3a5f',
    '--admin-gradient-end':   '#2d1b69',
    '--shadow-color':         '0,0,0',
}

const darkTokens: ThemeTokens = {
    '--primary-color':        '#667eea',
    '--primary-hover':        '#5a6fd6',
    '--primary-rgb':          '102,126,234',
    '--gradient-start':       '#667eea',
    '--gradient-end':         '#764ba2',
    '--background':           '#1a1a2e',
    '--card-bg':              '#16213e',
    '--input-bg':             '#1a1a3e',
    '--text-color':           '#e0e0e0',
    '--text-muted':           '#a0a0a0',
    '--border-color':         '#2a2a4a',
    '--hover-bg':             'rgba(255, 255, 255, 0.08)',
    '--success-color':        '#10b981',
    '--danger-color':         '#ef4444',
    '--warning-color':        '#f59e0b',
    '--info-color':           '#3b82f6',
    '--badge-video':          '#3b82f6',
    '--badge-audio':          '#8b5cf6',
    '--admin-gradient-start': '#1e3a5f',
    '--admin-gradient-end':   '#2d1b69',
    '--shadow-color':         '0,0,0',
}

const midnightTokens: ThemeTokens = {
    '--primary-color':        '#818cf8',
    '--primary-hover':        '#6366f1',
    '--primary-rgb':          '129,140,248',
    '--gradient-start':       '#312e81',
    '--gradient-end':         '#1e1b4b',
    '--background':           '#0f0e1a',
    '--card-bg':              '#1a1830',
    '--input-bg':             '#1e1c36',
    '--text-color':           '#e2e0f0',
    '--text-muted':           '#9896b0',
    '--border-color':         '#2d2b4a',
    '--hover-bg':             'rgba(255, 255, 255, 0.06)',
    '--success-color':        '#34d399',
    '--danger-color':         '#f87171',
    '--warning-color':        '#fbbf24',
    '--info-color':           '#60a5fa',
    '--badge-video':          '#60a5fa',
    '--badge-audio':          '#a78bfa',
    '--admin-gradient-start': '#1e1b4b',
    '--admin-gradient-end':   '#312e81',
    '--shadow-color':         '0,0,0',
}

const nordTokens: ThemeTokens = {
    '--primary-color':        '#88c0d0',
    '--primary-hover':        '#7eb8c8',
    '--primary-rgb':          '136,192,208',
    '--gradient-start':       '#5e81ac',
    '--gradient-end':         '#81a1c1',
    '--background':           '#2e3440',
    '--card-bg':              '#3b4252',
    '--input-bg':             '#434c5e',
    '--text-color':           '#eceff4',
    '--text-muted':           '#d8dee9',
    '--border-color':         '#4c566a',
    '--hover-bg':             'rgba(255, 255, 255, 0.08)',
    '--success-color':        '#a3be8c',
    '--danger-color':         '#bf616a',
    '--warning-color':        '#ebcb8b',
    '--info-color':           '#81a1c1',
    '--badge-video':          '#81a1c1',
    '--badge-audio':          '#b48ead',
    '--admin-gradient-start': '#3b4252',
    '--admin-gradient-end':   '#434c5e',
    '--shadow-color':         '0,0,0',
}

const draculaTokens: ThemeTokens = {
    '--primary-color':        '#bd93f9',
    '--primary-hover':        '#a77de8',
    '--primary-rgb':          '189,147,249',
    '--gradient-start':       '#bd93f9',
    '--gradient-end':         '#ff79c6',
    '--background':           '#282a36',
    '--card-bg':              '#343746',
    '--input-bg':             '#3c3f58',
    '--text-color':           '#f8f8f2',
    '--text-muted':           '#bfbfbf',
    '--border-color':         '#44475a',
    '--hover-bg':             'rgba(255, 255, 255, 0.08)',
    '--success-color':        '#50fa7b',
    '--danger-color':         '#ff5555',
    '--warning-color':        '#f1fa8c',
    '--info-color':           '#8be9fd',
    '--badge-video':          '#8be9fd',
    '--badge-audio':          '#bd93f9',
    '--admin-gradient-start': '#44475a',
    '--admin-gradient-end':   '#6272a4',
    '--shadow-color':         '0,0,0',
}

const solarizedLightTokens: ThemeTokens = {
    '--primary-color':        '#268bd2',
    '--primary-hover':        '#1a7abd',
    '--primary-rgb':          '38,139,210',
    '--gradient-start':       '#268bd2',
    '--gradient-end':         '#2aa198',
    '--background':           '#fdf6e3',
    '--card-bg':              '#eee8d5',
    '--input-bg':             '#fdf6e3',
    '--text-color':           '#657b83',
    '--text-muted':           '#93a1a1',
    '--border-color':         '#d3cbb7',
    '--hover-bg':             'rgba(0, 0, 0, 0.06)',
    '--success-color':        '#859900',
    '--danger-color':         '#dc322f',
    '--warning-color':        '#b58900',
    '--info-color':           '#268bd2',
    '--badge-video':          '#268bd2',
    '--badge-audio':          '#6c71c4',
    '--admin-gradient-start': '#073642',
    '--admin-gradient-end':   '#002b36',
    '--shadow-color':         '0,0,0',
}

const forestTokens: ThemeTokens = {
    '--primary-color':        '#4ade80',
    '--primary-hover':        '#22c55e',
    '--primary-rgb':          '74,222,128',
    '--gradient-start':       '#166534',
    '--gradient-end':         '#14532d',
    '--background':           '#0c1a0e',
    '--card-bg':              '#132416',
    '--input-bg':             '#172e1a',
    '--text-color':           '#d4eed8',
    '--text-muted':           '#88b890',
    '--border-color':         '#244a2a',
    '--hover-bg':             'rgba(74, 222, 128, 0.08)',
    '--success-color':        '#4ade80',
    '--danger-color':         '#f87171',
    '--warning-color':        '#fbbf24',
    '--info-color':           '#60a5fa',
    '--badge-video':          '#60a5fa',
    '--badge-audio':          '#c084fc',
    '--admin-gradient-start': '#14532d',
    '--admin-gradient-end':   '#052e16',
    '--shadow-color':         '0,0,0',
}

const sunsetTokens: ThemeTokens = {
    '--primary-color':        '#f97316',
    '--primary-hover':        '#ea580c',
    '--primary-rgb':          '249,115,22',
    '--gradient-start':       '#ea580c',
    '--gradient-end':         '#dc2626',
    '--background':           '#1c1210',
    '--card-bg':              '#271a16',
    '--input-bg':             '#2e201a',
    '--text-color':           '#f0ddd4',
    '--text-muted':           '#b89888',
    '--border-color':         '#4a3028',
    '--hover-bg':             'rgba(249, 115, 22, 0.08)',
    '--success-color':        '#22c55e',
    '--danger-color':         '#ef4444',
    '--warning-color':        '#f59e0b',
    '--info-color':           '#38bdf8',
    '--badge-video':          '#38bdf8',
    '--badge-audio':          '#e879f9',
    '--admin-gradient-start': '#7c2d12',
    '--admin-gradient-end':   '#991b1b',
    '--shadow-color':         '0,0,0',
}

/* ─────────────────────────  Theme registry  ───────────────────────── */

export const builtInThemes: ThemeDefinition[] = [
    { id: 'light',           label: 'Light',           base: 'light', tokens: lightTokens },
    { id: 'dark',            label: 'Dark',            base: 'dark',  tokens: darkTokens },
    { id: 'midnight',        label: 'Midnight',        base: 'dark',  tokens: midnightTokens },
    { id: 'nord',            label: 'Nord',            base: 'dark',  tokens: nordTokens },
    { id: 'dracula',         label: 'Dracula',         base: 'dark',  tokens: draculaTokens },
    { id: 'solarized-light', label: 'Solarized Light', base: 'light', tokens: solarizedLightTokens },
    { id: 'forest',          label: 'Forest',          base: 'dark',  tokens: forestTokens },
    { id: 'sunset',          label: 'Sunset',          base: 'dark',  tokens: sunsetTokens },
]

/** Lookup map for quick access by id. */
export const themeMap = new Map<string, ThemeDefinition>(
    builtInThemes.map(t => [t.id, t]),
)
