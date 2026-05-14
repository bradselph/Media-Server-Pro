import type { RouterConfig } from '@nuxt/schema'

// Preserve scroll position when the user returns to a list page from a
// detail page (e.g. /player → / via the browser back button). The default
// Nuxt 3 behaviour is correct for back/forward but races against
// asynchronous data fetches on the list page — when the user lands the
// page has 0 items, scrollTo(0,0) is honored, then items load and the
// page is at the top. We persist scroll-top per pathname into
// sessionStorage on leave and replay it after the next mount + a short
// rAF tick so the layout has measured the grid.
//
// Affects: /, /browse, /search, /categories, /favorites, /history.
// /player and admin pages always scroll to top.

const SCROLL_KEY = 'msp-scroll-positions'
const RESTORE_PATHS = new Set(['/', '/browse', '/search', '/categories', '/favorites', '/history', '/playlists'])

function readPositions(): Record<string, number> {
  if (typeof window === 'undefined') return {}
  try {
    const raw = sessionStorage.getItem(SCROLL_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as unknown
    if (parsed && typeof parsed === 'object') return parsed as Record<string, number>
  }
  catch { /* ignore */ }
  return {}
}

function writePositions(map: Record<string, number>) {
  if (typeof window === 'undefined') return
  try {
    sessionStorage.setItem(SCROLL_KEY, JSON.stringify(map))
  }
  catch { /* sessionStorage may be unavailable / quota */ }
}

// Stash the scroll position whenever the user navigates away from a
// restore-eligible path. The pagehide event also catches hard reloads.
if (typeof window !== 'undefined') {
  const stash = () => {
    const path = window.location.pathname
    if (!RESTORE_PATHS.has(path)) return
    const map = readPositions()
    map[path] = window.scrollY
    writePositions(map)
  }
  window.addEventListener('pagehide', stash)
}

export default <RouterConfig>{
  scrollBehavior(to, from, savedPosition) {
    // Same route, same path (e.g. query-string-only change) — don't scroll.
    if (to.path === from.path) return false

    // Stash the leaving page's scroll if it's restore-eligible so the user
    // lands back at the same spot when they come back from /player.
    if (typeof window !== 'undefined' && from.path && RESTORE_PATHS.has(from.path)) {
      const map = readPositions()
      map[from.path] = window.scrollY
      writePositions(map)
    }

    // Wait for the first paints after the new page mounts before we apply
    // any restoration — pages with async data render at height 0 initially,
    // and scrolling to a position past the current bottom is a no-op.
    const restoreEligible = RESTORE_PATHS.has(to.path)
    let target: { top: number; left: number } | null = null
    if (savedPosition) {
      target = savedPosition
    }
    else if (restoreEligible) {
      const map = readPositions()
      const y = map[to.path]
      if (typeof y === 'number' && y > 0) target = { top: y, left: 0 }
    }
    if (!target) return { top: 0, left: 0 }

    return new Promise((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve(target)))
    })
  },
}
