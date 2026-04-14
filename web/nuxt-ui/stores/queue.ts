import { defineStore } from 'pinia'

export interface QueueItem {
    id: string
    name: string
    type: 'video' | 'audio' | 'unknown'
    duration: number
    thumbnail_url?: string
}

export const useQueueStore = defineStore('queue', () => {
    const items = ref<QueueItem[]>([])

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

    return { items, addToQueue, addNext, shift, remove, clear, moveUp, moveDown }
})
