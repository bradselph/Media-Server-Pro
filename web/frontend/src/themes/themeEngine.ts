/**
 * Theme engine — applies a ThemeDefinition to the document by setting CSS
 * custom properties on <html> and updating the `data-theme` attribute.
 */

import type {ThemeDefinition, ThemeTokens} from './themes'
import {themeMap} from './themes'

/**
 * Apply a theme definition to the document.
 * Sets every token as a CSS custom property and updates `data-theme`
 * so that any remaining light/dark CSS selectors still work.
 */
export function applyThemeDefinition(theme: ThemeDefinition): void {
    const root = document.documentElement
    root.setAttribute('data-theme', theme.base)
    root.setAttribute('data-theme-id', theme.id)

    const entries = Object.entries(theme.tokens) as [keyof ThemeTokens, string][]
    for (const [prop, value] of entries) {
        root.style.setProperty(prop, value)
    }
}

/**
 * Resolve a theme ID (which might be "auto") to a concrete ThemeDefinition.
 *
 * - "auto" picks "light" or "dark" based on `prefers-color-scheme`.
 * - Any other string is looked up in the built-in theme map.
 * - Falls back to "dark" if the ID is unrecognised.
 */
export function resolveThemeId(id: string): ThemeDefinition {
    if (id === 'auto') {
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
        return themeMap.get(prefersDark ? 'dark' : 'light')!
    }
    return themeMap.get(id) ?? themeMap.get('dark')!
}

/**
 * Convenience: resolve + apply in one step.
 */
export function applyThemeById(id: string): void {
    applyThemeDefinition(resolveThemeId(id))
}

/**
 * Build a ThemeDefinition from user-supplied partial overrides on top of
 * an existing theme's tokens.  Useful for custom user themes.
 */
export function createCustomTheme(
    id: string,
    label: string,
    baseThemeId: string,
    overrides: Partial<ThemeTokens>,
): ThemeDefinition {
    const base = themeMap.get(baseThemeId) ?? themeMap.get('dark')!
    return {
        id,
        label,
        base: base.base,
        tokens: {...base.tokens, ...overrides},
    }
}
