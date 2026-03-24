import { defineStore } from 'pinia'
import type { Playlist } from '~/types/api'

export const usePlaylistStore = defineStore('playlist', () => {
  const playlists = ref<Playlist[]>([])
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  async function fetchPlaylists() {
    isLoading.value = true
    error.value = null
    try {
      playlists.value = await usePlaylistApi().list()
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to load playlists'
    } finally {
      isLoading.value = false
    }
  }

  async function createPlaylist(name: string, description?: string, isPublic = false) {
    const pl = await usePlaylistApi().create({ name, description, is_public: isPublic })
    playlists.value.unshift(pl)
    return pl
  }

  async function updatePlaylist(id: string, data: Partial<Playlist>) {
    const updated = await usePlaylistApi().update(id, data)
    const idx = playlists.value.findIndex(p => p.id === id)
    if (idx !== -1) playlists.value[idx] = updated
    return updated
  }

  async function deletePlaylist(id: string) {
    await usePlaylistApi().delete(id)
    playlists.value = playlists.value.filter(p => p.id !== id)
  }

  async function addMediaToPlaylist(playlistId: string, mediaId: string) {
    await usePlaylistApi().addItem(playlistId, mediaId)
    const updated = await usePlaylistApi().get(playlistId)
    const idx = playlists.value.findIndex(p => p.id === playlistId)
    if (idx !== -1) playlists.value[idx] = updated
    return updated
  }

  async function removeMediaFromPlaylist(playlistId: string, itemId: string) {
    await usePlaylistApi().removePlaylistItemById(playlistId, itemId)
    const pl = playlists.value.find(p => p.id === playlistId)
    if (pl && pl.items) pl.items = pl.items.filter(i => i.id !== itemId)
  }

  return {
    playlists, isLoading, error,
    fetchPlaylists, createPlaylist, updatePlaylist, deletePlaylist,
    addMediaToPlaylist, removeMediaFromPlaylist,
  }
})
