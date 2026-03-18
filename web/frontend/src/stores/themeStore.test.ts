import {describe, expect, it, vi, beforeEach} from 'vitest'
import {useThemeStore} from './themeStore'

// Mock matchMedia for the 'auto' theme test
function mockMatchMedia(prefersDark: boolean) {
    Object.defineProperty(window, 'matchMedia', {
        writable: true,
        value: vi.fn().mockImplementation((query: string) => ({
            matches: query === '(prefers-color-scheme: dark)' ? prefersDark : false,
            media: query,
            onchange: null,
            addListener: vi.fn(),
            removeListener: vi.fn(),
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
            dispatchEvent: vi.fn(),
        })),
    })
}

beforeEach(() => {
    // Reset store state to default
    useThemeStore.setState({theme: 'dark'})
    // Set a default matchMedia mock
    mockMatchMedia(true)
    // Clear data-theme attribute
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.removeAttribute('data-theme-id')
    document.documentElement.style.cssText = ''
})

describe('themeStore', () => {
    it('initial theme is dark', () => {
        expect(useThemeStore.getState().theme).toBe('dark')
    })

    it('setTheme updates theme and applies to DOM', () => {
        useThemeStore.getState().setTheme('light')

        expect(useThemeStore.getState().theme).toBe('light')
        expect(document.documentElement.getAttribute('data-theme')).toBe('light')
        expect(document.documentElement.getAttribute('data-theme-id')).toBe('light')
    })

    it('toggleTheme cycles through all built-in themes and auto', () => {
        // Start at dark
        expect(useThemeStore.getState().theme).toBe('dark')

        // Toggle through all themes — the exact cycle is defined in themeStore
        // based on builtInThemes. Just verify it cycles back to start.
        const seen: string[] = ['dark']
        for (let i = 0; i < 20; i++) {
            useThemeStore.getState().toggleTheme()
            const t = useThemeStore.getState().theme
            if (t === 'dark') break // cycled back to start
            seen.push(t)
        }
        // Should have cycled back
        expect(useThemeStore.getState().theme).toBe('dark')
        // Should include auto and at least light
        expect(seen).toContain('light')
        expect(seen).toContain('auto')
    })

    it('auto theme resolves based on matchMedia preferring dark', () => {
        mockMatchMedia(true)
        useThemeStore.getState().setTheme('auto')

        expect(useThemeStore.getState().theme).toBe('auto')
        expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    })

    it('auto theme resolves based on matchMedia preferring light', () => {
        mockMatchMedia(false)
        useThemeStore.getState().setTheme('auto')

        expect(useThemeStore.getState().theme).toBe('auto')
        expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    })

    it('setTheme applies CSS custom properties to documentElement', () => {
        useThemeStore.getState().setTheme('dark')

        // Theme engine should have set CSS variables
        expect(document.documentElement.style.getPropertyValue('--primary-color')).toBe('#667eea')
        expect(document.documentElement.style.getPropertyValue('--background')).toBe('#1a1a2e')
    })

    it('named themes apply correct tokens', () => {
        useThemeStore.getState().setTheme('nord')

        expect(useThemeStore.getState().theme).toBe('nord')
        expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
        expect(document.documentElement.getAttribute('data-theme-id')).toBe('nord')
        expect(document.documentElement.style.getPropertyValue('--primary-color')).toBe('#88c0d0')
    })

    it('unknown theme falls back to dark', () => {
        useThemeStore.getState().setTheme('nonexistent')

        expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
        expect(document.documentElement.style.getPropertyValue('--primary-color')).toBe('#667eea')
    })
})
