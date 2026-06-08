import type {Playlist, Suggestion} from '~/types/api'
import {useFavoritesApi, usePlaylistApi, usePlaybackApi, useSuggestionsApi} from '~/composables/useApiEndpoints'

// Wiring for a personalized RecommendationRow on secondary pages (search,
// categories). Owns the /api/suggestions/personalized fetch plus the
// favorite / playlist / progress state the row's hover actions expect.
// index.vue keeps its own copy of this wiring because the same state also
// drives the home grid, hero, and bulk-selection features there.
export function usePersonalizedRow(limit = 12) {
    const authStore = useAuthStore()
    const router = useRouter()
    const toast = useToast()
    const suggestionsApi = useSuggestionsApi()
    const playbackApi = usePlaybackApi()
    const favoritesApi = useFavoritesApi()
    const playlistApi = usePlaylistApi()

    const items = ref<Suggestion[]>([])
    const loading = ref(false)
    const favoriteIds = ref<Set<string>>(new Set())
    const togglingIds = new Set<string>()
    // reactive() so .add() calls re-trigger the v-if guards hiding broken images
    const failedIds = reactive(new Set<string>())
    const progress = ref<Record<string, number>>({})
    const myPlaylists = ref<Playlist[]>([])

    let alive = true
    onUnmounted(() => {
        alive = false
    })

    async function load() {
        if (!authStore.isLoggedIn) {
            items.value = []
            return
        }
        loading.value = true
        try {
            const [rec, favs, pls] = await Promise.allSettled([
                suggestionsApi.getPersonalized(limit),
                favoritesApi.list(),
                playlistApi.list(),
            ])
            if (!alive) return
            if (rec.status === 'fulfilled') items.value = rec.value ?? []
            if (favs.status === 'fulfilled') favoriteIds.value = new Set((favs.value ?? []).map(r => r.media_id))
            if (pls.status === 'fulfilled') myPlaylists.value = pls.value ?? []
            void loadProgress()
        } finally {
            if (alive) loading.value = false
        }
    }

    // Batch-fetch resume positions so the row surfaces the same progress bar /
    // Watched pill pair as the home page's suggestion rows.
    async function loadProgress() {
        const ids = items.value.map(s => s.media_id).filter(Boolean)
        if (ids.length === 0) return
        try {
            const r = await playbackApi.getBatchPositions(ids)
            if (!alive) return
            const positions = r?.positions ?? {}
            const out: Record<string, number> = {}
            for (const s of items.value) {
                const pos = positions[s.media_id]
                const dur = s.duration ?? 0
                if (pos && dur > 0) out[s.media_id] = pos / dur
            }
            progress.value = out
        } catch { /* non-critical */
        }
    }

    async function toggleFavorite(id: string) {
        if (!authStore.isLoggedIn) {
            router.push('/login')
            return
        }
        if (togglingIds.has(id)) return
        const wasFav = favoriteIds.value.has(id)
        // Optimistic update
        const next = new Set(favoriteIds.value)
        if (wasFav) next.delete(id)
        else next.add(id)
        favoriteIds.value = next
        togglingIds.add(id)
        try {
            if (wasFav) await favoritesApi.remove(id)
            else await favoritesApi.add(id)
        } catch {
            if (!alive) return
            // Revert on error
            const reverted = new Set(favoriteIds.value)
            if (wasFav) reverted.add(id)
            else reverted.delete(id)
            favoriteIds.value = reverted
        } finally {
            togglingIds.delete(id)
        }
    }

    // Per-card quick "add to playlist" (does not navigate, keeps playback running)
    async function quickAddToPlaylist(itemId: string, playlistId: string) {
        try {
            await playlistApi.addItem(playlistId, itemId)
            toast.add({title: 'Added to playlist', color: 'success', icon: 'i-lucide-list-music'})
        } catch {
            toast.add({title: 'Already in playlist or failed to add', color: 'warning', icon: 'i-lucide-list-x'})
        }
    }

    function playlistMenuItemsFor(itemId: string) {
        const plItems = myPlaylists.value.map(pl => ({
            label: pl.name,
            icon: 'i-lucide-list-music',
            click: () => quickAddToPlaylist(itemId, pl.id),
        }))
        const newPl = [{label: 'New Playlist…', icon: 'i-lucide-plus', to: '/playlists'}]
        return plItems.length > 0 ? [plItems, newPl] : [newPl]
    }

    function onThumbnailError(id: string) {
        failedIds.add(id)
    }

    return {items, loading, favoriteIds, failedIds, progress, load, toggleFavorite, playlistMenuItemsFor, onThumbnailError}
}
