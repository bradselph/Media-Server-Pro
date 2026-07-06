import {describe, it, expect, beforeEach} from 'vitest'
import {setActivePinia, createPinia} from 'pinia'
import {useQueueStore, type QueueItem} from '~/stores/queue'

const item = (id: string, name = id): QueueItem => ({id, name, type: 'video', duration: 10})

describe('queue store', () => {
    beforeEach(() => {
        window.localStorage.clear()
        setActivePinia(createPinia())
    })

    it('adds items and dedupes by id', () => {
        const q = useQueueStore()
        q.addToQueue(item('a'))
        q.addToQueue(item('a', 'A duplicate'))
        q.addToQueue(item('b'))
        expect(q.items.map(i => i.id)).toEqual(['a', 'b'])
    })

    it('addNext moves an existing item to the front (plays next)', () => {
        const q = useQueueStore()
        q.addToQueue(item('a'))
        q.addToQueue(item('b'))
        q.addNext(item('b'))
        expect(q.items.map(i => i.id)).toEqual(['b', 'a'])
        expect(q.items.length).toBe(2) // no duplicate
    })

    it('shift removes and returns the head, and returns null when empty', () => {
        const q = useQueueStore()
        q.addToQueue(item('a'))
        q.addToQueue(item('b'))
        expect(q.shift()?.id).toBe('a')
        expect(q.items.map(i => i.id)).toEqual(['b'])
        expect(q.shift()?.id).toBe('b')
        expect(q.shift()).toBeNull()
    })

    it('remove and clear mutate the queue', () => {
        const q = useQueueStore()
        q.addToQueue(item('a'))
        q.addToQueue(item('b'))
        q.remove('a')
        expect(q.items.map(i => i.id)).toEqual(['b'])
        q.clear()
        expect(q.items).toEqual([])
    })

    it('moveUp / moveDown reorder within bounds and no-op at the edges', () => {
        const q = useQueueStore()
        q.addToQueue(item('a'))
        q.addToQueue(item('b'))
        q.addToQueue(item('c'))
        q.moveUp('a')   // already first -> no-op
        expect(q.items.map(i => i.id)).toEqual(['a', 'b', 'c'])
        q.moveDown('b')
        expect(q.items.map(i => i.id)).toEqual(['a', 'c', 'b'])
        q.moveUp('b')
        expect(q.items.map(i => i.id)).toEqual(['a', 'b', 'c'])
        q.moveDown('c') // already last -> no-op
        expect(q.items.map(i => i.id)).toEqual(['a', 'b', 'c'])
    })

    it('rehydrates a persisted queue and drops corrupt entries on load', () => {
        window.localStorage.setItem('msp-queue-v1', JSON.stringify([
            {id: 'good', name: 'Good', type: 'video', duration: 5},
            {id: 'no-duration', name: 'Bad'},          // missing duration -> dropped
            {name: 'no-id', duration: 3},               // missing id -> dropped
            null,                                        // junk -> dropped
        ]))
        setActivePinia(createPinia())
        const q = useQueueStore()
        expect(q.items.map(i => i.id)).toEqual(['good'])
    })
})
