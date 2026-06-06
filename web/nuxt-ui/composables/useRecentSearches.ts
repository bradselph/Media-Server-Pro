// Per-device persistent buffer of the user's last N search queries. Backs
// the recent-searches strip on /search and the nav autocomplete dropdown so
// both stay in sync as the user runs new queries.
const RECENT_KEY = 'msp-recent-searches'
const RECENT_MAX = 8

function read(): string[] {
    if (typeof window === 'undefined') return []
    try {
        const raw = window.localStorage.getItem(RECENT_KEY)
        if (!raw) return []
        const parsed = JSON.parse(raw)
        return Array.isArray(parsed)
            ? parsed.filter((v): v is string => typeof v === 'string').slice(0, RECENT_MAX)
            : []
    } catch {
        return []
    }
}

function write(list: string[]) {
    if (typeof window === 'undefined') return
    try {
        window.localStorage.setItem(RECENT_KEY, JSON.stringify(list.slice(0, RECENT_MAX)))
    } catch { /* quota or storage disabled */
    }
}

export function useRecentSearches() {
    const recent = useState<string[]>('msp-recent-searches', () => read())

    function push(q: string) {
        const trimmed = q.trim()
        if (!trimmed) return
        const next = [trimmed, ...recent.value.filter(r => r.toLowerCase() !== trimmed.toLowerCase())]
        recent.value = next.slice(0, RECENT_MAX)
        write(recent.value)
    }

    function remove(q: string) {
        recent.value = recent.value.filter(r => r !== q)
        write(recent.value)
    }

    function clear() {
        recent.value = []
        write([])
    }

    return {recent, push, remove, clear}
}
