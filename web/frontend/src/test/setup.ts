import '@testing-library/jest-dom'

// Provide a localStorage polyfill for zustand persist middleware in jsdom.
// Some jsdom/vitest configurations don't provide a fully functional localStorage.
if (typeof globalThis.localStorage === 'undefined' || typeof globalThis.localStorage?.setItem !== 'function') {
    const store: Record<string, string> = {}
    globalThis.localStorage = {
        getItem: (key: string) => store[key] ?? null,
        setItem: (key: string, value: string) => { store[key] = String(value) },
        removeItem: (key: string) => { delete store[key] },
        clear: () => { Object.keys(store).forEach(k => delete store[k]) },
        get length() { return Object.keys(store).length },
        key: (index: number) => Object.keys(store)[index] ?? null,
    }
}

// Provide a default matchMedia mock for tests that need it (e.g., themeStore).
if (typeof window.matchMedia !== 'function') {
    Object.defineProperty(window, 'matchMedia', {
        writable: true,
        value: (query: string) => ({
            matches: false,
            media: query,
            onchange: null,
            addListener: () => {},
            removeListener: () => {},
            addEventListener: () => {},
            removeEventListener: () => {},
            dispatchEvent: () => false,
        }),
    })
}
