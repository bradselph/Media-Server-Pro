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
})

describe('themeStore', () => {
    it('initial theme is dark', () => {
        expect(useThemeStore.getState().theme).toBe('dark')
    })

    it('setTheme updates theme and applies to DOM', () => {
        useThemeStore.getState().setTheme('light')

        expect(useThemeStore.getState().theme).toBe('light')
        expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    })

    it('toggleTheme cycles dark -> light -> auto', () => {
        // Start at dark
        expect(useThemeStore.getState().theme).toBe('dark')

        useThemeStore.getState().toggleTheme()
        expect(useThemeStore.getState().theme).toBe('light')

        useThemeStore.getState().toggleTheme()
        expect(useThemeStore.getState().theme).toBe('auto')

        useThemeStore.getState().toggleTheme()
        expect(useThemeStore.getState().theme).toBe('dark')
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
})
