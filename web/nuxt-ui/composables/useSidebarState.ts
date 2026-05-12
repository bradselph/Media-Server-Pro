/**
 * useSidebarState — single source of truth for the NowPlayingSidebar's
 * open/closed/rail state, active tab, and which playlist is pinned.
 *
 * Built as a global singleton via `createSharedComposable`-style pattern
 * (module-scoped refs) so that default.vue's body data-attribute and the
 * sidebar component itself stay in sync without prop drilling.
 *
 * Persists to localStorage under the `msp-sidebar-*` namespace so the
 * user's choice survives reloads. Keys are read inside an onMounted on
 * first call so SSR never touches `window`.
 */

type Tab = 'queue' | 'playlist'

const LS_OPEN = 'msp-sidebar-open'
const LS_TAB = 'msp-sidebar-tab'
const LS_PIN = 'msp-sidebar-pinned-playlist'

// Module-scoped singletons. Initialized to the default (open) and synced
// to localStorage in initFromStorage().
const open = ref(true)
const tab = ref<Tab>('queue')
const pinnedPlaylistId = ref<string | null>(null)
let initialized = false

function initFromStorage() {
    if (initialized || typeof window === 'undefined') return
    initialized = true

    const savedOpen = localStorage.getItem(LS_OPEN)
    if (savedOpen !== null) open.value = savedOpen !== 'false'

    const savedTab = localStorage.getItem(LS_TAB)
    if (savedTab === 'queue' || savedTab === 'playlist') tab.value = savedTab

    pinnedPlaylistId.value = localStorage.getItem(LS_PIN)

    // Persist on change. These watchers live for the lifetime of the SPA
    // (module scope) which is fine — there's only ever one sidebar.
    watch(open, (v) => { localStorage.setItem(LS_OPEN, String(v)) })
    watch(tab, (v) => { localStorage.setItem(LS_TAB, v) })
    watch(pinnedPlaylistId, (v) => {
        if (v) localStorage.setItem(LS_PIN, v)
        else localStorage.removeItem(LS_PIN)
    })
}

export function useSidebarState() {
    if (import.meta.client) initFromStorage()
    return {
        open,
        tab,
        pinnedPlaylistId,
        toggle: () => { open.value = !open.value },
        expand: () => { open.value = true },
        collapse: () => { open.value = false },
        setTab: (next: Tab) => { tab.value = next },
        pinPlaylist: (id: string | null) => { pinnedPlaylistId.value = id },
    }
}
