import { defineStore } from 'pinia'
import type { ServerSettings } from '~/types/api'

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<ServerSettings | null>(null)
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  const features = computed(() => settings.value?.features)

  async function loadSettings() {
    if (settings.value) return
    isLoading.value = true
    error.value = null
    try {
      const { get } = useSettingsApi()
      settings.value = await get()
    } catch (e: unknown) {
      // Non-critical — UI degrades gracefully, but callers can check error state
      error.value = e instanceof Error ? e.message : 'Failed to load server settings'
    } finally {
      isLoading.value = false
    }
  }

  return { settings, isLoading, error, features, loadSettings }
})
