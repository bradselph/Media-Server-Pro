import {defineStore} from 'pinia'

export interface QueueItem {
    id: string
    name: string
    type: 'video' | 'audio' | 'unknown'
    duration: number
    thumbnail_url?: string
}

// localStorage key for queue persistence. Surviving a reload matches the
// sidebar's persisted open/tab/pin state so the entire "what's queued"
// thread holds together across sessions (handoff A.6 risk #2).
const LS_QUEUE = 'msp-queue-v1'

function loadInitialItems(): QueueItem[] {
    if (typeof window === 'undefined') return []
    try {
        const raw = window.localStorage.getItem(LS_QUEUE)
        if (!raw) return []
        const parsed = JSON.parse(raw)
        if (!Array.isArray(parsed)) return []
        // Drop entries that fail a basic shape check so a corrupt store
        // never injects undefined into row keys / templates.
        return parsed.filter((x): x is QueueItem =>
            !!x && typeof x.id === 'string' && typeof x.name === 'string'
            && typeof x.duration === 'number',
        )
    } catch {
        return []
    }
}

export const useQueueStore = defineStore('queue', () => {
    const items = ref<QueueItem[]>(loadInitialItems())

    // Mirror to localStorage on every mutation. The list rarely exceeds a
    // dozen items, so the JSON.stringify cost is negligible and avoids the
    // pinia-plugin-persistedstate dependency.
    if (typeof window !== 'undefined') {
        watch(items, (next) => {
            try {
                window.localStorage.setItem(LS_QUEUE, JSON.stringify(next))
            } catch { /* localStorage blocked / full */
            }
        }, {deep: true})
    }

    function addToQueue(item: QueueItem) {
        // Avoid duplicate consecutive entries
        if (items.value.some(q => q.id === item.id)) return
        items.value.push(item)
    }

    function addNext(item: QueueItem) {
        // Insert at front (plays next)
        items.value = items.value.filter(q => q.id !== item.id)
        items.value.unshift(item)
    }

    /** Remove the first item and return it. Returns null if queue is empty. */
    function shift(): QueueItem | null {
        return items.value.shift() ?? null
    }

    function remove(id: string) {
        items.value = items.value.filter(q => q.id !== id)
    }

    function clear() {
        items.value = []
    }

    function moveUp(id: string) {
        const idx = items.value.findIndex(q => q.id === id)
        if (idx <= 0) return
        const copy = [...items.value]
        ;[copy[idx - 1], copy[idx]] = [copy[idx]!, copy[idx - 1]!]
        items.value = copy
    }

    function moveDown(id: string) {
        const idx = items.value.findIndex(q => q.id === id)
        if (idx < 0 || idx >= items.value.length - 1) return
        const copy = [...items.value]
        ;[copy[idx], copy[idx + 1]] = [copy[idx + 1]!, copy[idx]!]
        items.value = copy
    }

    return {items, addToQueue, addNext, shift, remove, clear, moveUp, moveDown}
})
